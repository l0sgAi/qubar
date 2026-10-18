# Agent 工作流维护手册（l0sgAi/qubar#67）

规则本身见 [CLAUDE.md](../../CLAUDE.md)，分支规则与标签见 [.github/rulesets/README.md](../../.github/rulesets/README.md)。
本文只记维护者日常要做的操作和排障方法。

## 日常操作

| 要做的事 | 怎么做 |
|---|---|
| 让 agent 开工 | issue 写清背景 / 验收标准后，把 `agent:triage` 改成 `agent:ready`；`agent-dispatch` 会在打标签时或每小时第 17 分钟认领（同时最多 3 个 `agent:wip`） |
| 批准高风险 PR | 在 PR 的 Conversation 底部发**普通评论** `/approve <head-sha 前 7 位>`，或在终端：`gh pr comment <PR> -R l0sgAi/qubar --body "/approve <sha>"`。不是 Review changes → Approve |
| 批准后又有新提交 | head sha 变了，之前的 `/approve` 自动失效，要对新 sha 再发一次 |
| 暂停某个 PR 的自动合并 | 打 `hold` 标签；去掉后 `pr-gate` 会重新评估 |
| 发版 | 合并 `develop → main` 的 PR（merge commit）。只改工作流 / 文档时 release 只跑 `changes`，线上不变 |

## 排障

### `claude-review` 是红的

先看状态描述（PR 的 Checks 区，或 `gh api repos/l0sgAi/qubar/commits/<sha>/statuses`）：

| 描述 | 含义 / 处理 |
|---|---|
| `skipped: CLAUDE_CODE_OAUTH_TOKEN secret not set` | 仓库没有该 secret，见下文「轮换 token」 |
| `skipped: claude-review.yml is not on main yet` / `differs from main` | 本 PR 改了 `claude-review.yml`，或 develop 上的版本还没发版到 main。claude-code-action 只运行与 main 完全一致的工作流文件，属预期；规则要求它为必需检查时需要 admin 越过规则合并 |
| `Claude run failed, see run log` | 打开 run 看 `review` job。动作默认隐藏完整输出，看不到原因时用下文的诊断方法 |
| `N blocking issue(s)` | 真实审查结论。`agent/` 分支会自动交给 `agent-fix`（最多 3 次） |

### 看不到 Claude 的报错原因

claude-code-action 默认隐藏完整输出，而改 Claude 工作流本身又会触发「与 main 不一致」而被跳过。做法：开一个**草稿 PR**，加一个临时 `pull_request` 工作流，直接用 `curl -fsSL https://claude.ai/install.sh | bash` 装 CLI，带 `CLAUDE_CODE_OAUTH_TOKEN` 跑 `claude -p ... --output-format json`，只打印 `result` 字段；看完关闭 PR 并删分支。#77（token 无效）和 #87（权限规则写法）都是这样定位的。

### `--allowedTools` 规则写法

- `Write(<路径>)` **永远不匹配**；写文件用不带路径的 `Write`，或 `Edit(tmp/**)`、`Edit(//绝对路径)`。
- `Edit(/路径)`（单斜杠）是相对项目根，不是绝对路径。
- Bash 里用 heredoc / `$(...)` / 管道 / `&&` 拼多行正文会被拒：先写文件，再 `--body-file` / `git commit -F`。

## 轮换 `CLAUDE_CODE_OAUTH_TOKEN`

在**这个 app 之外**的终端（macOS Terminal / iTerm）执行，不要在聊天里用 `!`，否则 token 会进对话记录：

```bash
claude setup-token
gh secret set CLAUDE_CODE_OAUTH_TOKEN -R l0sgAi/qubar
gh secret list -R l0sgAi/qubar
```

第二条命令提示时粘贴 token（不回显）。完成后 ⌘K 清屏。token 有效期一年。

## 生效条件（为什么改了工作流没反应）

- `pull_request_target`（`pr-gate`）、`issues` / `issue_comment`（`agent-dispatch`、`claude`、`/approve`）、`schedule`（`pr-sync`、`agent-dispatch` 补位）、`workflow_run`（`agent-fix`）都只按 **main** 上的工作流运行。
- 用 `GITHUB_TOKEN` 推的提交 / 打的标签 / 触发的合并不会再触发别的工作流（`workflow_dispatch` 除外）；claude[bot] 用 App token 推的会触发。
- 所以：改了工作流 → 合并到 develop → **发版到 main** 才生效。
