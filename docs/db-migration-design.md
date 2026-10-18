# DDL 迁移工具接入实施计划（issue #62）

> 状态：**已实施**（2026-10-15，§8 六项决策均按建议），待生产基线比对与 force 后上线。分支 `feature/db-migrate-20261015`。
> 使用规范以 [migrations/README.md](../migrations/README.md) 为准；本文保留设计过程与取舍。
> 选型沿用 issue：golang-migrate（forward-only）+ Squawk。本文只写「怎么落地」与 issue 未覆盖的风险点。

## 0. 现状摸底中发现、issue 未提到的问题

| # | 问题 | 影响 | 处理 |
|---|---|---|---|
| A | **golang-migrate 只记录"当前版本号"**，版本号小于当前版本的新文件会被 `up` **静默跳过** | 两个分支并行写迁移，后合并的那个时间戳更早 → 生产永远不执行，且不报错 | CI 硬检查：PR 新增迁移的版本号必须 **大于** base 分支已有的最大版本号（否则要求重命名） |
| B | `bootstrap.sql` 是 `root:root 0600`（含全部角色密码），CI 以 `deploy` 用户 SSH，读不到 | issue 设计 5「迁移后重跑 bootstrap.sql」无法直接实现 | 拆成 `roles.sql`（密码，0600）+ `grants.sql`（授权 + `dbz_pub`，无密钥，`root:deploy 0640`）；迁移后只重跑 `grants.sql` |
| C | infra（Ansible）与 build 并行、都早于 deploy；Ansible 跑 bootstrap 时**新表还没建**，publication 块会跳过它 | 新 CDC 表本次发布进不了 `dbz_pub` | 顺序改为 `infra → migrate`，migrate 之后重跑 `grants.sql`（此时 `cdc_tables` 已是新值、表也已存在） |
| D | 只改 `deploy/**`（如 `cdc_tables`）不改应用时，`deploy` job 不会跑 → `register-connectors.sh` 不执行 | 新 CDC 表登记了 publication，但 Debezium `table.include.list` 未更新 | `deploy` job 在 migrate/infra 成功时也运行，但"换镜像"步骤只在有新镜像/回滚时执行（见 §3） |
| E | PG18 的 `pg_dump` 输出含 `\restrict` / `\unrestrict` psql 元命令、`SET` / `set_config('search_path','',false)` | golang-migrate 不是 psql，元命令直接报语法错；`search_path=''` 会残留在会话里 | 基线导出后清洗（脚本化，见 §1） |
| F | 文档 `docs/pgsql-ddl/` 记录 **16** 张表，issue 说生产 **17** 张 | 基线以哪个为准 | 以**生产导出为准**；多出的那张在基线 review 时确认是遗留表还是漏写文档 |
| G | 已有 `deploy/scripts/migrate-pg.sh`（一次性**数据**搬迁脚本） | 名字撞车易混淆 | 新脚本命名 `db-migrate.sh`，README 里区分 |
| H | 首次上线时若先跑了 pipeline 再 `force` 基线，`up` 会对已存在的表执行 `CREATE TABLE` → 失败 + dirty | 首发事故 | `db-migrate.sh` 护栏：`public.schema_migrations` 不存在且 `domains` 非空 → 拒绝执行，提示先 `force`；`force` 在 develop→main 之前手工完成（§6） |

## 1. 仓库结构

```
migrations/
  README.md                                   # 编写规范、expand/contract、CONCURRENTLY、dirty 修复
  20261015000000_baseline.up.sql              # 生产 schema 基线（不 lint、不做 CDC 检查）
  2026xxxxxxxxxx_<desc>.up.sql                # 之后的增量（forward-only，不建 .down.sql）
.squawk.toml                                  # Squawk 规则配置
Makefile                                      # 仓库目前没有 Makefile，新建，只放 migrate-* 目标
scripts/check-migrations.sh                   # CI 校验：命名 / 版本单调 / 无 down / CDC 登记
deploy/scripts/clean-baseline.sh              # 清洗 pg_dump 输出为基线迁移（一次性）
deploy/scripts/db-migrate.sh                  # 服务器侧执行迁移（CI 经 SSH 调用）
.github/ci/pg-init.sql                        # CI 临时库：建角色 + domains schema（无密码）
```

**基线生成**（需 DB-owner 在生产执行一次，我写清洗脚本）：

```bash
ssh ssh.qubar.site 'docker exec qubar-postgres pg_dump -U postgres -d qubar \
  --schema-only -n domains --no-owner --no-privileges' > /tmp/prod-schema.sql
deploy/scripts/clean-baseline.sh /tmp/prod-schema.sql > migrations/20261015000000_baseline.up.sql
```

清洗规则：删 `\restrict`/`\unrestrict`、`SET ...`、`set_config(...)`、`CREATE SCHEMA domains`、`ALTER ... OWNER TO`、`COMMENT ON SCHEMA`、`CREATE PUBLICATION`；保留表 / 索引 / `COMMENT ON TABLE|COLUMN`。
然后对本机 dev 库同样导出比对，确认 dev 与生产一致（不一致先修 dev）。

**每个迁移文件约定**（`make migrate-new` 模板自动带）：

```sql
-- cdc: none            -- 新建 domains 表且不进 CDC 时必须写；进 CDC 则改 deploy/ansible/group_vars/all.yml 的 cdc_tables
SET lock_timeout = '5s';   -- 拿不到锁就失败，避免 ALTER 排队把线上读写全部堵住
...
```

- 文件内多语句由 PG 作为**隐式事务**整体执行（golang-migrate 默认非 multi-statement 模式），失败整体回滚，但版本会被置 dirty。
- `CREATE INDEX CONCURRENTLY` 必须**单独一个文件、只有这一条语句**（不能在事务里）。

## 2. 服务器侧（Ansible，`roles/stack`）

| 改动 | 说明 |
|---|---|
| `bootstrap.sql.j2` → `roles.sql.j2` + `grants.sql.j2` | 前者 `root:root 0600`；后者 `root:deploy 0640`。"Apply PG bootstrap SQL" 依次执行两者，行为与现在一致 |
| 目录 `{{ qubar_root }}/migrate/`、`migrate/migrations/` | `deploy:deploy 0750`，CI rsync 目标 |
| `{{ qubar_root }}/migrate/.env` | `deploy:deploy 0600`，内容 `PGPASSWORD=<pg_owner_password>`（不进命令行、不进 `docker inspect` 的 Cmd）；`no_log` |
| `scripts/db-migrate.sh` 加入 "Scripts" 下发清单 | 见下 |
| `group_vars/all.yml` 加 `migrate_image: migrate/migrate:v4.x.y` | 版本实施时取最新稳定版并锁定 |

`db-migrate.sh`（deploy 用户执行；deploy 在 docker 组）：

1. 护栏 H：查 `public.schema_migrations` 是否存在 / `domains` 表数；
2. `docker run --rm --network qubar-net --env-file /opt/qubar/migrate/.env -v /opt/qubar/migrate/migrations:/migrations:ro $MIGRATE_IMAGE -path /migrations -database 'postgres://qubar_owner@postgres:5432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1' up`
   - **实施时验证**：golang-migrate 的 postgres 驱动（lib/pq）是否读取 `PGPASSWORD`；不读则改为容器内 `sh -c` 拼 URL（密码只出现在容器进程内）；
3. `docker exec -i qubar-postgres psql -U postgres -d qubar -v ON_ERROR_STOP=1 < /opt/qubar/middleware/pg/grants.sql`（补授权 + 刷新 `dbz_pub`）；
4. 打印 `migrate version`，追加一行到 `/opt/qubar/migrate/migrations.log`（时间、git sha、版本）——补上 issue 里"无执行记录"。

任一步失败 → 非 0 退出 → 流水线中止，不换镜像。

## 3. `release.yml`

```
changes ─┬─ infra ───────────────┐
         └─ test → build ────────┴─→ migrate ─→ deploy
```

- `changes`：新增过滤器 `db: migrations/**`；新增输出 `db`。
- **`migrate` job**（新增，`environment: production`）
  - `needs: [changes, infra, build]`，条件：`infra`/`build` 未失败，且 `(db == true || app == true)` 且非回滚。应用发版时也跑 —— `up` 无待执行迁移时是 no-op，保证"应用永远不早于 schema"。
  - 步骤：`prod-access` → `rsync -a --delete migrations/ $PROD_SSH:/opt/qubar/migrate/migrations/` → `ssh $PROD_SSH /opt/qubar/bin/db-migrate.sh ${{ github.sha }}`。
- **`deploy` job**
  - `needs` 加 `migrate`，条件加 `needs.migrate.result != 'failure'`；
  - 运行条件放宽为：build 成功 / 回滚 / migrate 成功 / infra 成功（问题 D）；
  - "GHCR login / Deploy / GHCR logout" 步骤加 `if: build 成功或回滚`；"CDC connectors" 与 "Smoke" 始终执行。
- **回滚**（`rollback: true`）不跑迁移。expand/contract 约束保证旧镜像能在新 schema 上运行。
- `workflow_dispatch` 加 `force_migrate`（只跑迁移），便于 dirty 修复后重试。

## 4. `ci.yml`（PR 阶段，不碰生产）

新增 `db` job：

1. **`scripts/check-migrations.sh`**（对 PR 相对 base 新增/修改的迁移文件）：
   - 文件名 `^[0-9]{14}_[a-z0-9_]+\.up\.sql$`；禁止 `.down.sql`；
   - **已合并的迁移文件不允许修改**（只允许新增）；
   - 新增文件版本号 > base 分支最大版本号（问题 A）；
   - `CREATE TABLE domains.<t>`：`<t>` 不在 `cdc_tables` 且文件无 `-- cdc: none` → 失败（硬失败，按 issue 建议）；
   - 含 `CONCURRENTLY` 的文件只能有这一条语句。
2. **Squawk**：lint 变更的迁移文件（排除 baseline）。`.squawk.toml` 关掉与团队规范冲突的规则：`prefer-text-field`（团队用 varchar 定长）、`prefer-bigint-over-int` / `prefer-identity`（UUIDv7 主键，不适用）；保留 `require-concurrent-index-creation`、`adding-not-nullable-field`、`changing-column-type`、`ban-drop-column`、`renaming-column`、`require-timeout-settings` 等。版本锁定。
3. **全量回放**：`services: postgres:18.4` → `psql -f .github/ci/pg-init.sql`（建 `qubar_owner/qubar_web_app/debezium`、`domains` schema、default privileges）→ 以 `qubar_owner` `migrate up` 全部 → 再跑一次 `up` 确认 no-op → `pg_dump --schema-only` 作为 artifact 上传，便于 review 时看最终 schema。
4. `shellcheck` 范围扩到 `scripts/*.sh`。

## 5. 本机（Makefile）

```make
MIGRATE_DB ?= postgres://qubar_owner@localhost:5432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1
migrate-new:     # make migrate-new name=add_xxx  → 生成 <ts>_add_xxx.up.sql（带模板头），不生成 down
migrate-up:      # migrate -path migrations -database "$(MIGRATE_DB)" up
migrate-version:
migrate-force:   # make migrate-force v=<version>，修 dirty 用
migrate-check:   # 本地跑 scripts/check-migrations.sh + squawk
```

依赖 `brew install golang-migrate squawk`（README 写明）。本机 dev 库首次也要 `make migrate-force v=20261015000000`。

## 6. 上线顺序（关键，防问题 H）

1. 本分支 PR → develop：CI 绿；review 基线 SQL。
2. **develop → main 之前**，DB-owner 手工：
   `ssh -L 15432:127.0.0.1:5432 ssh.qubar.site` → `migrate -path migrations -database 'postgres://qubar_owner@127.0.0.1:15432/qubar?...' force 20261015000000`；本机 dev 同样 force。
3. develop → main：`infra`（拆分 bootstrap、建目录与 env）→ `migrate`（no-op，版本 = 基线）→ `deploy`（若有应用变更）。
4. 演练：一个真实的加列（或 `COMMENT ON`）迁移完整走 develop → main，核对 `schema_migrations`、`migrations.log`、应用正常。
5. 失败演练（可选，在 CI 临时库）：故意写错一条迁移，确认 CI 阻断；dirty 修复流程按 README 走一遍。

## 7. 文档 / 约定同步

- `migrations/README.md`：新建（规范、expand/contract 示例、CONCURRENTLY、dirty 修复：`migrate force <上一个成功版本>` → 修 SQL → 重跑）。
- `deploy/README.md`：「DDL 变更」一节改为迁移流程；排障表加 `db-migrate.sh` / `migrations.log`；区分 `migrate-pg.sh`。
- `docs/pgsql-ddl/README.md`：顶部声明**权威来源已迁到 `migrations/`**，本目录降为字段语义 / 设计说明；删掉"建表后手工 GRANT"附录（改由 default privileges + `grants.sql` 自动完成）。
- `pkg/server/storage/db/pgsql/connect.go:55` 注释、`.claude/skills/qubar-skill`（SKILL.md、add-domain.md、tech-stack.md 中"改表先改 docs/pgsql-ddl"）改为"新增 migrations/ 迁移文件，同步更新 docs/pgsql-ddl 语义说明"。
- 用户级 `~/.claude/skills/pgsql-ddl` 本身是通用的（会先探测仓库迁移工具），**不需要改**，不在本 PR 范围。

## 8. 待你决策

1. **权威来源迁到 `migrations/`**，`docs/pgsql-ddl/` 降为说明 —— 建议：是。
2. **forward-only，不维护 `.down.sql`** —— 建议：是。
3. **CDC 登记检查为 CI 硬失败** —— 建议：是。
4. **（新增）应用发版时是否总是跑 `migrate up`**（即使本次没有迁移变更）—— 建议：是（no-op 很便宜，防止漏跑）。
5. **（新增）每个迁移强制 `SET lock_timeout`**（Squawk `require-timeout-settings`）—— 建议：是，5s。
6. **（新增）提交拆分**：单个 PR 内按"仓库侧（migrations/CI/Makefile/文档）→ 服务器侧（Ansible/脚本）→ 流水线（release.yml）"分 3 个 commit；还是拆成 2 个 PR（先合仓库侧+CI，再合部署侧）—— 建议：单 PR、分 commit（两侧要一起上线才闭环）。

## 9. 需要你配合的事

- 生产 schema 导出（§1，需生产访问，我不直接连生产）。
- develop → main 之前的生产 / dev `force` 基线（§6 第 2 步）。

## 10. 实施记录（与上文计划的差异）

- **基线来源**：先用本机 dev 库 `pg_dump` 生成（17 张表，与 issue 中生产表数一致）；上线前须按 §1 导出生产 schema，
  用 `clean-baseline.sh` 清洗后与仓库基线 `diff`，一致才 force。多出的第 17 张表是 `log_payment_webhook_event`：
  `docs/pgsql-ddl` 未记录、Go 代码也未引用，暂按现状收进基线，是否删除另议（删除需单独迁移）。
- **同步方式**：服务器没装 rsync，改为 `tar | ssh` 解到 `migrations.new` 后整目录替换。
- **lock_timeout 双保险**：除迁移文件 `SET lock_timeout = '5s'` 外，`roles.sql` 给 `qubar_owner` 设角色默认 `lock_timeout = 5s`。
  实测 `CREATE INDEX CONCURRENTLY` 文件里连 `SET` 都不能有（多语句会被包成事务），只能靠角色默认值。
- **跳过检测做了两层**：CI（PR 版本号 > base 最大版本）+ 服务器 `applied.txt` 清单（发现版本号 <= 当前版本但从未执行的文件即拒绝）。
- **已合并迁移的修改例外**：生产迁移失败需修正原文件时，PR 打 `migration-fix` 标签放行（见 migrations/README.md）。
- **Squawk 规则**：排除 `prefer-text-field`、`prefer-bigint-over-smallint`、`prefer-bigint-over-int`、`require-statement-timeout`；
  额外启用 `require-table-schema`；`assume_in_transaction = true`。
- **本机验证**（PG 18.4 容器 + migrate v4.20.1）：`PGPASSWORD` 经 env-file 生效；失败整体回滚并置 dirty；
  `db-migrate.sh` 的基线护栏 / dirty 拒绝 / 旧版本号拒绝 / 新 CDC 表进 `dbz_pub` 与默认授权 / dirty 修复流程均跑通；
  `check-migrations.sh` 各拒绝场景在临时仓库验证。GitHub Actions 两个 workflow 尚未在 CI 实跑。
