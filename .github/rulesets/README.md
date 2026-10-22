# 仓库闸口配置（l0sgAi/qubar#67 P2）

GitHub 上的分支规则、仓库设置和标签不在代码里，这里记录它们的权威定义和应用命令（需要仓库 admin）。
修改这些设置时先改本目录的文件、走 PR，合并后再执行命令。

## develop 分支规则（`develop.json`）

- 只能通过 PR 合入；禁止 force push、禁止删除分支。
- 必须通过的检查：`go` / `db` / `image` / `iac` / `gitleaks` / `integration`，以及 P3 的 `risk-gate`（`pr-gate.yml`）和 `claude-review`（`claude-review.yml`），且来源限定为 GitHub Actions（`integration_id: 15368`），防止伪造的同名 status 放行。新增必须的 CI job 时同步加到这里。
- 要求分支与 develop 保持最新（strict）。没有 merge queue（个人账号仓库不支持），由它防止两个各自绿的 PR 合并后互相冲突；落后时 `gh pr update-branch <PR>` 或在 PR 页面点 Update branch。
- 不要求 approve：仓库只有一个人，GitHub 不允许作者 approve 自己的 PR。人工审核由必需检查 `risk-gate` 强制：带 `risk:high` 的 PR 要维护者评论 `/approve <head-sha>` 才能合并（新的 push 会让之前的批准失效）。
- `claude-review` 是必需检查：没配 `CLAUDE_CODE_OAUTH_TOKEN` 或额度用完时所有 PR 都会卡住，需要 admin 在 PR 上越过规则合并。
- admin 可以在 PR 上越过规则合并（`bypass_mode: pull_request`），但不能直接 push 到 develop。
- **生效顺序**：`pr-gate`（`pull_request_target`）和所有 claude-code-action 工作流都只按默认分支（main）上的版本运行。
  先把工作流发版到 main、装好 Claude GitHub App、配好 `CLAUDE_CODE_OAUTH_TOKEN`，确认 PR 上出现了 `risk-gate` / `claude-review` 状态，
  再更新规则把它们设为必需检查，否则所有 PR 都会卡住。

```bash
# 首次创建
gh api -X POST repos/l0sgAi/qubar/rulesets --input .github/rulesets/develop.json
# 修改：先查 id，再整体更新
id=$(gh api repos/l0sgAi/qubar/rulesets --jq '.[] | select(.name=="develop") | .id')
gh api -X PUT repos/l0sgAi/qubar/rulesets/$id --input .github/rulesets/develop.json
```

## Dependabot（`.github/dependabot.yml`）

- **版本更新**：`gomod`（根目录）和 `github-actions`（工作流 + `.github/actions/*`）每周一检查，PR 提到 `develop`，
  minor / patch 合成一个分组 PR，major 单独开（gomod 忽略 major：Go 的 major 是换模块路径，要人工改 import）。
- **claude-review**：Dependabot 触发的工作流拿不到 Actions 密钥（`GITHUB_TOKEN` 仍按 `permissions` 授权），Claude 审查跑不了。
  `claude-review.yml` 的 prepare 对满足以下全部条件的 PR 直接记 `success`（`dependabot: dependency-only PR, CI is the gate`）：
  PR 作者是 `dependabot[bot]`；非合并提交都由 `dependabot[bot]` 提交且签名已验证；diff 只碰 `go.mod` / `go.sum` /
  工作流 / composite action，且 `go.mod` 只改依赖、`go`、`toolchain` 版本行，工作流只改 `uses: x@版本` 行。
  不满足时记 `failure` 并写明原因，需要人看过后 admin 越过规则合并（或在 PR 上手动触发一次 claude-review：人触发时有密钥，会正常审查）。
  没有选择「把 `CLAUDE_CODE_OAUTH_TOKEN` 也配成 Dependabot secret」：那样任何能改 Dependabot 分支内容的依赖更新都会在带 token 的 job 里运行，
  而依赖升级的风险本来就主要靠 CI 发现，审查收益小。
- **auto-merge**：pr-gate 对 Dependabot PR 不做特殊处理。只改 `go.mod` / `go.sum` 的 PR 没有 `risk:high`，CI（含 `integration`）全绿后自动合并到 develop；
  Actions 版本 PR 改的是 `.github/**`，会被打 `risk:high`，要维护者 `/approve <head-sha>` 后手动合并。不想自动合并某个依赖 PR 时打 `hold`。
- **安全更新提到 main**：Dependabot 的安全更新（Settings → Code security → Dependabot security updates）固定提到默认分支 `main`，
  `target-branch` 改不了它。这类 PR **不要合并**（会绕过 develop 的全部闸口，且 develop → main 只由人发版）。二选一：
  - 关闭（默认）：每周的分组版本更新会把同一依赖升到最新，等它合入 develop 再发版，alert 随之关闭。
  - 急需时改目标分支，然后关闭再打开一次（改 base 不会触发 `pull_request` / `pull_request_target`，reopen 会）：

    ```bash
    gh pr edit <PR> --base develop
    gh pr close <PR> && gh pr reopen <PR>
    ```

    分支是从 main 切的，落后 develop，靠 `gh pr update-branch <PR>`（或开了 auto-merge 后由 pr-sync）更新；不要再用 `@dependabot rebase / recreate`，它可能按 main 重建分支。
    如果 main 上有 develop 没有的非合并提交，claude-review 的提交检查不通过，需要人处理。
  - 也可以关掉安全更新 PR、只保留 alert，全部依赖更新都走 develop：更干净，但修复要等每周的版本更新（或手动触发 Dependabot）。

## 仓库设置

```bash
gh api -X PATCH repos/l0sgAi/qubar -F allow_auto_merge=true -F delete_branch_on_merge=true
```

## 标签

| 标签 | 含义 |
|---|---|
| `agent:triage` | 新 issue，spec 待人审核（issue 模板默认带上） |
| `agent:ready` | spec 已审核，由 Actions 的 `agent-dispatch` 认领 |
| `agent:local` | spec 已审核，由本地 Claude Code session 实现；不被 `agent-dispatch` 认领，不打 `agent:wip` |
| `agent:wip` | Actions agent 开发中（只用于 `agent:ready` 模式） |
| `agent:review` | PR 已开，等 CI / review |
| `agent:found` | AI 巡检发现的问题，待人筛选 |
| `risk:high` | 触及高风险路径：不开 auto-merge，`risk-gate` 等 `/approve <head-sha>`（`pr-gate` 按 `.github/labeler.yml` 自动打） |
| `hold` | 暂停：关闭 auto-merge（人手动打，或 agent-fix 达到 3 次上限时自动打） |

```bash
gh label create agent:triage --color FBCA04 --description "新 issue，spec 待人审核" --force
gh label create agent:ready  --color 0E8A16 --description "spec 已审核，agent 可认领" --force
gh label create agent:local  --color 0E8A16 --description "由本地 session 实现（不被 agent-dispatch 认领）" --force
gh label create agent:wip    --color 1D76DB --description "agent 开发中" --force
gh label create agent:review --color 5319E7 --description "PR 已开，等 CI / review" --force
gh label create agent:found  --color C5DEF5 --description "AI 巡检发现，待人筛选" --force
gh label create risk:high    --color B60205 --description "高风险路径，必须人工审核，不开 auto-merge" --force
gh label create hold         --color 000000 --description "暂停 auto-merge，等人处理" --force
```
