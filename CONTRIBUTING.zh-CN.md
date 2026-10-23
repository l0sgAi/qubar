# 参与贡献 Qubar

[English](CONTRIBUTING.md) | **简体中文**

感谢你对 Qubar 感兴趣！欢迎提交 bug 报告、想法、文档修正和代码。
本文说明从克隆仓库到 PR 合并需要知道的事。

## 开始之前

- **小改动**（错别字、文档、明显的 bug）：直接提 PR。
- **较大的改动**（新接口、表结构变更、新领域）：先开 issue 对齐方案再写代码。想认领已有 issue，在下面评论即可。
- Issue 模板在 [.github/ISSUE_TEMPLATE](.github/ISSUE_TEMPLATE)。新 issue 默认带 `agent:triage`，等维护者审核。
- 发现安全问题？请不要公开提 issue，先私下联系维护者。

## 本地环境

只需要 **Go 1.26+** 和 **Docker**。

```bash
git clone https://github.com/<your-fork>/qubar.git
cd qubar
make dev-up     # 起 PG 18 / Redis / Elasticsearch(IK) / Redpanda 并执行全部迁移
make dev-run    # 以 configs/config.dev.yaml 启动 http://localhost:8888，自检 /healthz、/readyz
```

开发栈端口：PG `25432`、Redis `26379`、ES `29200`、Redpanda `29092`。`make dev-psql q="select ..."` 查开发库。

开发栈的已知限制：

- 没有 CDC，ES 里的业务索引**不会**随 PG 同步。
- S3 / OAuth / 邮件是占位配置，依赖它们的接口在本地不可用。改动涉及这些路径时，在 PR 里写明未验证的部分。

## 项目结构与约定

Qubar 是按领域拆分的 Go 模块化单体（`pkg/domains/<domain>/{domain,application,infrastructure,interfaces/http}`）。
总览见 [README](README.zh-CN.md)，完整规则见 [.agents/skills/qubar-skill/SKILL.md](.agents/skills/qubar-skill/SKILL.md)。最常踩的几条：

- `domain/` 是纯 Go：不 import GORM / Redis / Elasticsearch / Hertz，不 import 兄弟领域。
- 跨领域调用走 Facade/Port 接口 + `pkg/composition/` 里的桥接器，禁止直接 import。
- handler 用 `appctx.AppContext` 和 `pkg/shared/httputil` 的响应助手，不用 `c.JSON(...)`。
- 主键是 UUIDv7 且没有 `BeforeCreate` hook：**每次**插入都要显式调用 `sharedomain.NewID()`。
- 不用 GORM `AutoMigrate`，表结构只来自 [migrations/](migrations/) 的 forward-only 迁移
  （`make migrate-new name=...`，规则见 [migrations/README.md](migrations/README.md)）。
- 新增配置项同时改 `configs/config.example.yaml` 和 `configs/config.dev.yaml`。
- 绝不提交真实密钥。`configs/config.yaml` 已 git-ignore；`config.dev.yaml` 只放开发栈凭据。
- 代码注释和文档用中文，与现有代码保持一致。

## 测试

```bash
make verify            # 格式 + vet + lint + 单测 + 迁移检查与空库回放，与 CI 一致
make test-integration  # 连开发栈的集成测试（需先 make dev-up）
```

- 开 PR 前 `make verify` 必须通过。格式问题用 `gofmt -w cmd pkg` 修；lint 必须零告警。
- 改了 `infrastructure/`、SQL / 迁移、Redis Lua、Redpanda 生产者 / 消费者时，`make test-integration` 也必须通过。
  输出里出现 `SKIP` 说明栈没起来，不算通过。
- 新增基础设施代码要补集成测试：同目录 `*_integration_test.go`，首行 `//go:build integration`，用
  [pkg/shared/testkit](pkg/shared/testkit) 的夹具；数据用 `sharedomain.NewID()` 随机 ID 并注册清理，测试要能并行、能反复跑。

## 分支、提交与 PR

- 从 **`develop`** 切分支，PR 目标分支也是 **`develop`**。`main` 是生产发布分支，只由维护者合并。
- 分支命名：`feature/<slug>-<yyyymmdd>`，修 issue 用 `fix/<issue号>-<slug>`。
- commit 用英文 [Conventional Commits](https://www.conventionalcommits.org/)，如 `feat(post): add draft status`、
  `fix(auth): handle expired state (#48)`。
- 一个 PR 只解决一个 issue（描述里写 `Closes #N`）；业务代码改动尽量 ≤ 500 行（测试不计），大需求拆成多个 issue。
- 按 [PR 模板](.github/pull_request_template.md) 填写：做了什么、怎么验证的（`make verify` 结果、调用过的接口）、哪些没验证。

### 审查与合并

- 每个 PR 都会跑 CI（`go` / `db` / `image` / `iac` / `gitleaks` / `integration`），除维护者审查外还有 Claude 自动审查。
  需要仓库密钥的检查在 fork 来的 PR 上可能不会运行，由维护者处理。
- 触及高风险路径的 PR 会被自动打上 `risk:high`，必须经维护者明确批准：`migrations/**`、`deploy/**`、`.github/**`、
  `Dockerfile`、`pkg/domains/auth/**`、`pkg/server/auth/**`、`pkg/util/netguard/**`、`pkg/util/crypto/**`、`pkg/conf/**`。
  请在 PR 描述里显式说明这类改动。
- `develop` 有新提交时，把 `origin/develop` 合并进你的分支（或在 GitHub 上点 "Update branch"）。

## 关于 AI agent

Qubar 的大部分开发由 AI 编码 agent 完成。[CLAUDE.md](CLAUDE.md)、[AGENTS.md](AGENTS.md) 是写给 agent 的说明，
`agent:ready` / `agent:local` 等标签用于把 issue 分给 agent。作为人类贡献者不需要关心这些，按本文操作即可；
如果你自己也用 agent，让它读 `CLAUDE.md` 效果会很好。

## 许可证

提交贡献即表示你同意以 [MIT License](LICENSE) 授权你的贡献。
