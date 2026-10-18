# Qubar 生产部署（阿里云 ECS + Cloudflare Tunnel）

单机 ECS（ap-southeast-8，4C / 30G，Alibaba Cloud Linux 4）跑全部中间件 + API，业务与管理界面**无公网入站端口**，
流量经 Cloudflare Tunnel 进入。公网只开放 22（服务器拥有者 root 密钥登录，白名单由拥有者在安全组管理）。
机器上另有 MCSManager（23333 / 24444），本套 IaC 不触碰。

```
用户 ─HTTPS─► Cloudflare ─tunnel(出站)─► cloudflared(host 网络) ─► 127.0.0.1:8888 qubar
                                                      │                    │ docker 网络 qubar-net
CI / 运维 ─► Cloudflare Access(ssh.qubar.site) ───────┘ sshd:22            ▼
拥有者 ─► 公网 22（安全组 IP 白名单，root + 密钥）──────► sshd:22
                                          postgres:18.4 · redis:8.2.7 · elasticsearch+IK:8.15.3 · kibana
                                          redpanda:v26.1.10 · console · kafka-connect(Debezium PG→ES)
```

| 层 | 工具 | 目录 | 管什么 |
|---|---|---|---|
| 云资源 | Terraform | `terraform/bootstrap`、`terraform/prod` | bootstrap：R2 桶（state + PG 备份，含过期规则）；prod：CF tunnel / DNS / Zero Trust Access。**阿里云侧不归 Terraform 管**（无 RAM 权限），见「阿里云控制台手工项」 |
| 主机 + 中间件 | Ansible | `ansible/` | 内核参数 / swap / sshd / docker / deploy 用户；中间件 compose、PG 角色与 publication、topic、备份 timer、迁移运行目录 |
| 表结构 | golang-migrate | `../migrations/` | `domains` schema 版本化迁移（权威来源），发布时由 `db-migrate.sh` 执行，规范见 [migrations/README.md](../migrations/README.md) |
| 运行时 | Docker Compose | `compose/` | `middleware/`（中间件）、`app/`（qubar） |
| 自建镜像 | Dockerfile | `images/` | ES+IK、Kafka Connect+Debezium（服务器本地构建，tag 含 Dockerfile 哈希） |
| 流水线 | GitHub Actions | `.github/workflows/` | `ci.yml`（PR）、`release.yml`（develop → main 合并即发布） |

## 发布流程（日常）

develop → main 合并（PR merge）→ `release.yml`：

1. `deploy/**` 有变更 → **infra**：`terraform apply` → `ansible-playbook site.yml`（幂等）
2. 应用代码有变更 → **test → build**：`ghcr.io/l0sgai/qubar:<sha>`（linux/amd64）
3. `migrations/**` 有变更或有新镜像 → **migrate**：同步 `migrations/` 到 `/opt/qubar/migrate/migrations/` →
   `db-migrate.sh`（以 `qubar_owner` 跑 `migrate up`，再重跑 `grants.sql` 补授权、把新 CDC 表加进 `dbz_pub`）。
   **失败则不发布应用**，线上旧版本不受影响
4. **deploy**：经 CF Access SSH → `deploy-app.sh <image>`（`compose up --wait` 等健康检查，失败自动回滚上一版）
   → `register-connectors.sh`（幂等）→ 外网冒烟 `https://api-next.qubar.site/healthz`。
   只有 infra / 迁移变更时也会运行（不换镜像），以便重新注册 connector

回滚：Actions → release → Run workflow → 勾 `rollback`；或服务器上 `/opt/qubar/bin/deploy-app.sh --rollback`。
回滚只换镜像、**不回退 schema**（迁移 forward-only，且必须兼容上一版本应用）。

## 密钥清单（GitHub → Settings → Environments → `production`，仅允许 main）

| Secret | 来源 |
|---|---|
| `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` | R2 API token（Terraform state backend + 服务器 PG 备份上传共用） |
| `CLOUDFLARE_API_TOKEN` | CF API Token（见下方权限） |
| `DEPLOY_SSH_PRIVATE_KEY` | CI 登录 deploy 用户的私钥（公钥在 `ansible/group_vars/all.yml`） |
| `PG_SUPERUSER_PASSWORD` / `PG_OWNER_PASSWORD` / `PG_APP_PASSWORD` / `PG_DEBEZIUM_PASSWORD` / `REDIS_PASSWORD` | 随机生成，本机副本见 `~/deployements/aliyun-deployment/secrets.local.yml` |
| `QUBAR_CONFIG_YAML` | 生产 `config.yaml` 全文（原 Nacos `qubar-prod-conf`）。连接类配置（pgsql/redis/es/redpanda 地址与密码）由 `compose/app/compose.yml` 的 `QUBAR_*` 环境变量覆盖，YAML 里的值无所谓 |

`QUBAR_CONFIG_YAML` 注意：`oauth.proxy_url` 留空（服务器在海外，直连 Google）；OAuth `redirect_url` / CORS 用 `https://api.qubar.site` / `https://qubar.site`；
`security.data_key` 必须与旧环境一致，否则已加密入库的 AI 机器人 api_key 无法解密。

**Cloudflare API Token 权限**：Account → Cloudflare Tunnel: Edit、Access: Apps and Policies: Edit、Access: Service Tokens: Edit、Workers R2 Storage: Edit；Zone(qubar.site) → DNS: Edit。
**R2 API token**（bootstrap 建桶之后再建）：R2 → Manage API Tokens → Create → Object Read & Write，限定桶 `qubar-tfstate`、`qubar-backup`；拿 S3 兼容的 Access Key ID / Secret。
Cloudflare 控制台需先开通 Zero Trust（免费版），登录方式保留 One-time PIN。

## 首次引导（只做一次，按顺序）

```bash
# 0. 凭证（本机 shell）
export CLOUDFLARE_API_TOKEN=...

# 1. R2 桶（state + 备份）；完成后在 CF 控制台建 R2 API token（限这两个桶）
terraform -chdir=deploy/terraform/bootstrap init && terraform -chdir=deploy/terraform/bootstrap apply
export AWS_ACCESS_KEY_ID=<R2 Access Key ID> AWS_SECRET_ACCESS_KEY=<R2 Secret>

# 2. Cloudflare tunnel / Access / DNS
terraform -chdir=deploy/terraform/prod init && terraform -chdir=deploy/terraform/prod apply

# 3. 主机 + 中间件（tunnel 未通前走公网 IP；带上 tunnel token 让 cloudflared 起来）
cd deploy/ansible
ansible-playbook site.yml -e ansible_host=43.107.44.74 -e ansible_user=deploy \
  --private-key ~/deployements/aliyun-deployment/ALIYUN.pem \
  -e @~/deployements/aliyun-deployment/secrets.local.yml \
  -e tunnel_token="$(terraform -chdir=../terraform/prod output -raw tunnel_token)" \
  -e qubar_config_yaml="$(cat ~/deployements/aliyun-deployment/config.prod.yaml)" \
  -e r2_access_key_id="$AWS_ACCESS_KEY_ID" -e r2_secret_access_key="$AWS_SECRET_ACCESS_KEY"

# 4. 数据迁移（本机 PG → 生产；必须在注册 connectors 之前）
SSH_TARGET=deploy@43.107.44.74 SSH_OPTS="-i ~/deployements/aliyun-deployment/ALIYUN.pem" deploy/scripts/migrate-pg.sh

# 4.1 标记迁移基线（存量库不回放基线；不做这步 db-migrate.sh 会拒绝执行）。全新空库则跳过，由 migrate 从零建表
#     生产已于 2026-10-18 完成：force 前核对 domains 17 张表、全部归 qubar_owner，pg_dump 与基线文件一致；
#     首次带迁移的发布 release 39125657356：current=20261015000000 pending=0 → no change。
#     本机 ~/.ssh/config 没有 ssh.qubar.site 条目时，隧道需显式带 ProxyCommand 与密钥：
#     ssh -f -N -L 15432:127.0.0.1:5432 -o ProxyCommand='cloudflared access ssh --hostname %h' -i <密钥> deploy@ssh.qubar.site
ssh -L 15432:127.0.0.1:5432 ssh.qubar.site   # 另开终端保持
migrate -path migrations -database 'postgres://qubar_owner@127.0.0.1:15432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1' \
  force 20261015000000

# 5. 首次发布：合并 develop → main，或 Actions 手动 release(force_deploy)
```

### 切流 api.qubar.site（2026-10-12 已完成）

做法：把原 `api` CNAME（指向本机 `dev-debug` tunnel）`terraform import` 进 `cloudflare_dns_record.tunnel["api"]`，
`api_cutover = true` 后 apply 原地改 content → 零中断。本机开发 tunnel 已改用 `api-dev.qubar.site`。
回切（应急）：`api_cutover = false` 会**删除** api 记录——应急时直接在 CF 控制台把 content 改回 `b13234a3-….cfargotunnel.com`。

### SSH 访问约定（2026-10-13 起）

两条通道并存，**都必须保留**：

| 通道 | 谁用 | 身份 | 访问控制 |
|---|---|---|---|
| 公网 22 | 服务器拥有者 | `root` + 阿里云密钥对 `skp-v1sdthv1b4fzapd60uvl` | 阿里云安全组 IP 白名单，**由拥有者自行管理**（IaC 不管安全组） |
| Cloudflare Access（`ssh.qubar.site`） | CI、运维 | `deploy` + 服务令牌 / 邮箱 OTP | Zero Trust Access 策略（Terraform 管） |

对应 `ansible/group_vars/all.yml`：`ssh_permit_root_login: prohibit-password`（root 仅密钥，密码登录全局关闭）、`ssh_listen_local_only: false`（sshd 监听 0.0.0.0 / [::]）。
**不要**改成 `"no"` / `true`：2026-10-12 的 #60 曾这样做，把拥有者锁在了外面。

**救援（两条通道都不可用时）**：阿里云控制台 → 实例 → 远程连接 → **Workbench「免密登录 / 会话管理」**（走云助手 aliyun.service，不需要 22 端口和密码）。
VNC 也可用，但需要实例登录密码（密钥实例默认没有，重置密码要重启）。

### 阿里云控制台手工项（Terraform 不管）

- **安全组**（拥有者管理）：入方向保留 22（**只放行白名单 IP**，不要 0.0.0.0/0）与 MCSManager 用的端口；80/443 不需要。
- **自动快照**（可选，整机兜底）：云服务器 ECS → 存储与快照 → 自动快照策略 → 创建（每日 04:00、保留 14 天）→ 关联系统盘。按快照容量计费。

## 运维速查

本机 `~/.ssh/config`（需本机已 `cloudflared`；首次连接浏览器邮箱 OTP 登录）：

```
Host ssh.qubar.site
  User deploy
  IdentityFile ~/deployements/aliyun-deployment/ALIYUN.pem
  ProxyCommand cloudflared access ssh --hostname %h
```

| 场景 | 命令 |
|---|---|
| 连生产 PG（DDL 用 `qubar_owner`） | `ssh -L 15432:127.0.0.1:5432 ssh.qubar.site` → `psql -h 127.0.0.1 -p 15432 -U qubar_owner qubar` |
| Kibana / Redpanda Console | https://kibana.qubar.site 、 https://redpanda.qubar.site （CF Access 邮箱 OTP） |
| 应用日志 | `docker logs -f --tail 200 qubar` |
| 中间件状态 | `cd /opt/qubar/middleware && sudo docker compose ps` |
| CDC 状态 | `curl -s localhost:8083/connectors?expand=status \| jq` |
| 手动备份 | `sudo systemctl start qubar-pg-backup && journalctl -u qubar-pg-backup -n 20` |
| 发布历史 | `cat /opt/qubar/app/releases.log` |
| 迁移版本 / 历史 | `docker exec qubar-postgres psql -U postgres -d qubar -c 'table public.schema_migrations'`；`cat /opt/qubar/migrate/migrations.log` |
| 手工执行迁移 | `/opt/qubar/bin/db-migrate.sh`（执行 `/opt/qubar/migrate/migrations/` 下已同步的文件） |

表结构变更：在仓库 `migrations/` 新增迁移文件（`make migrate-new name=xxx`），随 develop → main 发布自动执行，规范与 dirty 修复见
[migrations/README.md](../migrations/README.md)。执行身份 `qubar_owner`，default privileges 自动给 `qubar_web_app` / `debezium` 授权。
新表要进 CDC：同一个 PR 里在 `ansible/group_vars/all.yml` 的 `cdc_tables`（需要 ES 的再加 `es_sinks`）登记，发布时 Ansible 更新 connector 配置，
迁移后 `grants.sql` 把新表加进 `dbz_pub`，deploy 重新注册 connector；不进 CDC 的表在迁移文件里写 `-- cdc: none`（CI 检查）。
注意不要与 `deploy/scripts/migrate-pg.sh`（首次引导时一次性搬运**数据**）混淆。

## 备份与恢复

- PG：每日 03:30 `pg_dump -Fc` → R2 `qubar-backup/pg/YYYY/MM/`（30 天过期），本地留 3 天 `/var/backups/qubar`。
- 整机：阿里云自动快照（控制台手工配置，见上）。
- ES：不备份，可重建 —— 删 `pg.domains.*` 索引 + 删 `pg-source` connector 与 slot 后重跑 `register-connectors.sh`（Debezium 重新全量快照）。
- 恢复 PG：`docker exec -i qubar-postgres pg_restore -U postgres -d qubar --role=qubar_owner --no-owner --no-privileges --clean --if-exists < dump`，再重跑 `/opt/qubar/middleware/pg/grants.sql`（`docker exec -i qubar-postgres psql -U postgres -d qubar < …/grants.sql`）。整库 dump 含 `public.schema_migrations`，迁移版本随之恢复。
