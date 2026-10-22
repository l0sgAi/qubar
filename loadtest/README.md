# loadtest — k6 压测（#63）

源站层压测：k6 容器在服务器上加入 `qubar-net` 直打 `qubar:8888`，不经 Cloudflare。结果与报告见 `docs/loadtest-report-*.md`。

| 文件 | 作用 |
|---|---|
| `seed-users.sh` | 注册 N 个 `lt_<RUN_ID>_*` 账号（Redis 预置"验证码已校验"标记，跳过邮件）、建压测圈、全员加圈 → `data/users.json` |
| `seed-content.js` | 压测账号在压测圈发帖，每帖 3 条根评论 + 2 条回复（帖子经 CDC 进 ES） |
| `fixture.sh` | 从 PG 取本批次帖子 / 评论 ID → `data/content.json` |
| `scenarios.js` | 场景 `SCENARIO=read\|write\|mixed\|sse`，节奏 `PROFILE=smoke\|stress\|soak\|spike` |
| `run.sh` | 服务器上跑一轮：k6 容器（`--cpus=1`）+ `monitor.sh` 采样 → `results/<场景>-<profile>-<时间>/` |
| `monitor.sh` | 临时监控：主机 / 容器 CPU 内存、PG 连接与复制槽积压、Redis、Redpanda 消费组积压 |
| `analyze.py` | 按时间窗汇总 RPS / p50 / p95 / p99 / 错误率并对齐监控采样，输出 Markdown 表 |
| `cleanup.sh` | 逻辑删除本批次数据 + 清理 Redis key；默认只预览，`APPLY=1` 执行 |

`data/`（含 token）与 `results/` 已 git-ignore。

## 约束

- **登录不进循环**：Argon2id 每次 64MB + 4 线程；token 在注册时拿到，压测全程复用（同账号同设备再登录会顶掉旧 token）。
- 压测账号只读写本批次的帖子 / 评论（`content.json`），不触碰真实用户数据；`cleanup.sh` 预览里的"越界"计数应为 0。
- 脚本不触达验证码 / 找回密码（Mailtrap 额度）与上传（S3 计费）接口。
- 压测前停用 AI 机器人（`ai_agent.status=0`，原值生成恢复 SQL 存 `data/`，结束后执行恢复），避免评论关键词触发 LLM 计费。

## 生产执行步骤

```bash
# 0. 本机同步脚本到服务器
rsync -a --exclude data --exclude results loadtest/ root@<host>:/root/qubar-loadtest/

# 服务器上，/root/qubar-loadtest
export REDIS_CLI='docker exec -i qubar-redis redis-cli' PSQL='docker exec -i qubar-postgres psql -U postgres -d qubar'
systemctl start qubar-pg-backup && journalctl -u qubar-pg-backup -n 5     # 1. 备份
mkdir -p data && $PSQL -Atc "select format('update domains.ai_agent set status = %s where id = %L;', status, id)
  from domains.ai_agent where status <> 0" > data/ai_agent_restore.sql    # 2. 记下原值（生成恢复 SQL）
$PSQL -c "update domains.ai_agent set status = 0 where status <> 0"       #    停 AI 机器人
N=200 ./seed-users.sh                                                     # 3. 账号 + 压测圈
docker run --rm --network qubar-net -u "$(id -u)" -v "$PWD:/lt" -w /lt grafana/k6 run \
  -e BASE_URL=http://qubar:8888 -e POSTS=3000 -e VUS=20 seed-content.js   # 4. 灌内容
./fixture.sh                                                              # 5. 等 CDC 同步后取 ID

SCENARIO=read PROFILE=smoke ./run.sh                                      # 6. 冒烟
SCENARIO=read PROFILE=stress RATE=600 STEPS=6 ./run.sh                    # 7. 阶梯加压找拐点
SCENARIO=write PROFILE=stress RATE=300 STEPS=6 ./run.sh
SCENARIO=mixed PROFILE=soak RATE=<拐点70%> DURATION=1h ./run.sh           # 8. 浸泡
SCENARIO=mixed PROFILE=spike RATE=<拐点> ./run.sh                         # 9. 突刺
SCENARIO=sse SSE_VUS=1000 ./run.sh                                        # 10. 长连（每用户上限 5，>5N 会 429）

python3 analyze.py results/<目录>                                         # 汇总（本机或服务器）

APPLY=1 ./cleanup.sh                                                      # 11. 清理
$PSQL < data/ai_agent_restore.sql                                         # 12. 恢复 AI 机器人
```

开发栈（`make dev-up && make dev-run`）可直接用默认的 `REDIS_CLI` / `PSQL` 调通脚本；但开发栈没有 CDC，
ES 里没有 post / users 索引，热门 / 最新流和搜索会 500，只能验证写链路与 PG / Redis 读链路。
