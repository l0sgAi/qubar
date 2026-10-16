# qubar — agent 工作说明

Go 模块化单体（module `interestBar`）：Hertz + GORM/PostgreSQL 18 + Redis + Elasticsearch(IK) + Redpanda。
**写任何代码前先读 `.agents/skills/qubar-skill/SKILL.md`**（分层、命名、跨域 Facade、加配置 / 加领域的步骤）。
仓库有 `.codegraph/` 索引，定位代码优先用 codegraph。

## 命令

```bash
make dev-up     # 起开发依赖栈（deploy/compose/dev）+ 执行迁移，只需要 Docker
make dev-run    # 启动应用 :8888（configs/config.dev.yaml），/healthz、/readyz 自检
make verify     # 提交 / 开 PR 前必须全绿（格式 + vet + lint + 单测 + 迁移检查与空库回放，与 CI 一致）
make test-integration  # 集成测试：连开发栈真实 PG / Redis / Redpanda（需先 make dev-up），CI integration job 同一命令
make dev-psql q="select ..."   # 查开发库；make dev-reset 清空数据卷重来
```

- `make verify` 失败就修到通过，不要跳过、不要改 lint / CI 配置去绕过。格式问题：`gofmt -w cmd pkg`。
- **改了 `infrastructure/`、SQL / `migrations/`、Redis Lua、Redpanda 生产者 / 消费者时，必须 `make dev-up && make test-integration` 通过**（`make verify` 不含它，因为它需要起开发栈）。
  输出里出现 `SKIP` 说明栈没起来，不算通过。新增基础设施代码要补集成测试：同目录 `*_integration_test.go`，首行 `//go:build integration`，
  包里 `func TestMain(m *testing.M) { testkit.Main(m) }`，用 `pkg/shared/testkit` 的 `PG / Redis / Redpanda / CleanupRows / CleanupKeys / Eventually`；
  数据一律用 `sharedomain.NewID()` 随机 ID 并注册清理，断言只针对自己插入的数据，测试要能 `t.Parallel()`、对同一个栈反复跑。
- lint 目前只检查相对 `origin/develop` 的改动（存量告警待清理，见 `.golangci.yml`），新代码必须零告警。
- 开发栈端口：PG 25432 / Redis 26379 / ES 29200 / Redpanda 29092。**不要连 15432**（那是 ssh 隧道到生产库的本地端口）。
- 开发栈没有 CDC：ES 里的业务索引不会随 PG 自动同步；S3 / OAuth / 邮件是占位配置。依赖这些的改动在 PR 里写明未验证的部分。

## 分支与 PR

- 从 `develop` 切分支：`feature/<slug>-<yyyymmdd>`，修 issue 用 `fix/<issue号>-<slug>`。
- PR 目标分支一律 `develop`。**`develop → main` 会触发生产发布，只由人操作。**
- 一个 PR 只解决一个 issue，描述里写 `Closes #N`；业务代码改动尽量 ≤ 500 行（测试不计），大需求拆 issue。
- commit 用 Conventional Commits，英文：`feat(post): ...`、`fix(auth): ... (#48)`。
- PR 描述写清：做了什么、怎么验证的（`make verify` 结果、手动调用的接口）、哪些没验证。

## 需要人工审核的改动（高风险）

以下路径的改动必须在 PR 里显式说明，等人审核，不要顺手改：

- `migrations/**`：只新增 forward-only 迁移（`make migrate-new name=...`），规则见 `migrations/README.md`；先迁移后发版，必须兼容旧版本应用；新建 `domains` 表要登记 CDC 或写 `-- cdc: none`。
- `deploy/**`、`.github/**`、`Dockerfile`：生产基础设施与发布流水线。
- `pkg/domains/auth/**`、`pkg/server/auth/**`、`pkg/util/netguard/**`、`pkg/util/crypto/**`：鉴权、SSRF 防护、加密。
- `pkg/conf/**` 与配置项：新增配置同时改 `configs/config.example.yaml` 和 `configs/config.dev.yaml`。

## 不可违反

- 绝不提交真实密钥。`configs/config.yaml`、`configs/bootstrap.yaml` 已 git-ignore；`configs/config.dev.yaml` 只放开发栈凭据（gitleaks 会拦截）。
- 主键是 UUIDv7 且没有 BeforeCreate hook：每次插入都要显式 `sharedomain.NewID()`（`pkg/shared/domain`），否则主键冲突。
- 不要用 GORM AutoMigrate，表结构只来自 `migrations/`。
- 不要对生产环境做任何操作（SSH、数据库、发布）。
- 代码注释、文档用中文，与现有代码保持一致。
