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
make dev-psql q="select ..."   # 查开发库
```

- `make verify` 失败就修到通过，不要跳过、不要改 lint / CI 配置去绕过。格式问题：`gofmt -w cmd pkg`。
- **改了 `infrastructure/`、SQL / `migrations/`、Redis Lua、Redpanda 生产者 / 消费者时，必须 `make dev-up && make test-integration` 通过**（`make verify` 不含它，因为它需要起开发栈）。
  输出里出现 `SKIP` 说明栈没起来，不算通过。新增基础设施代码要补集成测试：同目录 `*_integration_test.go`，首行 `//go:build integration`，
  包里 `func TestMain(m *testing.M) { testkit.Main(m) }`，用 `pkg/shared/testkit` 的 `PG / Redis / Redpanda / CleanupRows / CleanupKeys / Eventually`；
  数据一律用 `sharedomain.NewID()` 随机 ID 并注册清理，断言只针对自己插入的数据，测试要能 `t.Parallel()`、对同一个栈反复跑。
- lint 对全仓库生效，必须零告警（`make lint`）。
- 开发栈端口：PG 25432 / Redis 26379 / ES 29200 / Redpanda 29092。**不要连 15432**（那是 ssh 隧道到生产库的本地端口）。
- **开发栈 `qubar-dev` 由所有 worktree / session 共享**（同一个 compose 项目）：并行开发时**禁止 `make dev-reset` / `make dev-down`**，
  会清掉或停掉别的 session 正在用的栈。测试数据靠随机 ID 隔离，不需要重置。
- 开发栈没有 CDC：ES 里的业务索引不会随 PG 自动同步；S3 / OAuth / 邮件是占位配置。依赖这些的改动在 PR 里写明未验证的部分。

## 分支与 PR

- 从 `develop` 切分支：`feature/<slug>-<yyyymmdd>`，修 issue 用 `fix/<issue号>-<slug>`。
- PR 目标分支一律 `develop`。**`develop → main` 会触发生产发布，只由人操作。**
- 一个 PR 只解决一个 issue，描述里写 `Closes #N`；业务代码改动尽量 ≤ 500 行（测试不计），大需求拆 issue。
- commit 用 Conventional Commits，英文：`feat(post): ...`、`fix(auth): ... (#48)`。
- PR 描述按 `.github/pull_request_template.md` 填写：做了什么、怎么验证的（`make verify` 结果、手动调用的接口）、哪些没验证。
- `develop` 有分支规则（`.github/rulesets/`）：必须通过 `go` / `db` / `image` / `iac` / `gitleaks` / `integration` / `risk-gate` / `claude-review`，
  且分支要与 `develop` 保持最新。落后时 `gh pr update-branch <PR>`（或合并 `origin/develop`）后等 CI 重跑。不要尝试绕过规则。
- GitHub Actions 里的 agent 用 `agent/<issue号>-<slug>` 分支；本机 session 用上面的 `feature/` / `fix/` 命名。

## Issue 与标签流转

新 issue 都是 `agent:triage`（spec 待人审核），由人改成下面两种模式之一（按标签分流，工作流本身不变）：

| 模式 | 标签 | 谁实现 | 分支 | 状态流转 |
|---|---|---|---|---|
| Actions | `agent:ready` | `agent-dispatch`（Actions 里的 claude[bot]） | `agent/<N>-<slug>` | `agent:ready` → `agent:wip` → `agent:review` |
| 本地 | `agent:local` | 本地 Claude Code session（app 里独立 worktree） | `feature/<slug>-<yyyymmdd>` 或 `fix/<N>-<slug>` | `agent:local` → `agent:review`（不打 `agent:wip`，不占 Actions 的 3 个并发名额） |

选择标准：

- `agent:ready`：改动小、需求清楚、低风险、不需要讨论或决策。
- `agent:local`：触及高风险路径（见下文）、跨领域、需要讨论或决策、需要本地记忆 / codegraph，或者在 Actions 里失败过。

规则：

- 只做分给自己模式的 issue：Actions agent 只认 `agent:ready`；本地 session 只做明确交给它的 `agent:local` issue。
  `agent:triage` / `agent:found` 需要人审核，不要自己改标签。
- 已被 Actions 认领（`agent:wip`）的 issue 不要改成 `agent:local`：先等它退回 `agent:triage`，或关掉它开的 PR，再换模式。
- 本地模式：PR 描述写 `Closes #N`，开 PR 后把 issue 从 `agent:local` 改成 `agent:review`。CI / 审查出问题时由 app 的 PR Auto-fix
  唤醒该 session 修复，不走 `agent-fix`（它只处理 `agent/` 分支）。
- 两种模式的 PR 走同样的闸口：CI 6 项、`claude-review`、`risk-gate`、低风险自动合并、`pr-sync`。
- 做的过程中发现范围外的问题：用 issue 模板新开 issue 并打 `agent:found`，不要顺手在当前 PR 里改。
- `risk:high` 由 `pr-gate` 按 `.github/labeler.yml` 的路径自动添加：不要移除它。带它的 PR 不会自动合并，必需检查 `risk-gate`
  要等维护者评论 `/approve <head-sha>`。**agent 绝不能评论 `/approve`、不能给 PR 开 auto-merge、不能合并 PR。**

## 自动化流程（`.github/workflows/`）

- `agent-dispatch`：`agent:ready` 的 issue 由 Actions 里的 Claude 实现并开 PR（同时最多 3 个 `agent:wip`）；做不了就退回 `agent:triage`。
  `agent:local` 的 issue 不会被认领（由本地 session 实现）。
- `claude-review`：每个 PR 自动审查，结论写成 `claude-review` 状态；`agent/` 分支有阻塞问题时交给 `agent-fix`。
- `agent-fix`：`agent/` 分支 CI 失败 / 审查有阻塞问题 / 更新冲突时自动修复，每个 PR 最多 3 次，超过打 `hold`。
  本地模式的 PR 不归它管，由 app 的 PR Auto-fix 处理。
- `pr-gate`：打 `risk:high`、写 `risk-gate` 状态、给同仓库非草稿且无 `risk:high` / `hold` 的 PR 开 auto-merge。
- `pr-sync`：开了 auto-merge 但落后于 develop 的 PR 自动更新并补跑检查。
- Dependabot（`.github/dependabot.yml`）：gomod / github-actions 每周向 `develop` 提分组 PR。它拿不到密钥，`claude-review` 对只改依赖版本的
  Dependabot PR 直接记 success（CI 是闸口）；只改 `go.mod` / `go.sum` 的自动合并，Actions 版本 PR 是 `risk:high`。
  安全更新 PR 固定提到 `main`：不要合并，关闭或改 base 到 `develop`（见 `.github/rulesets/README.md`）。不要往 Dependabot 分支推提交。
- `claude`：有写权限的人在 issue / PR 里 `@claude` 让它答疑或改代码。

## 需要人工审核的改动（高风险）

以下路径的改动必须在 PR 里显式说明，等人审核，不要顺手改（与 `.github/labeler.yml` 保持一致，改动时两处同步）：

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
