# Agent 工作流维护手册（l0sgAi/qubar#67）

规则本身见 [CLAUDE.md](../../CLAUDE.md)，分支规则与标签见 [.github/rulesets/README.md](../../.github/rulesets/README.md)。
本文只记维护者日常要做的操作和排障方法。

## 日常操作

| 要做的事 | 怎么做 |
|---|---|
| 交给 Actions agent | issue 写清背景 / 验收标准后，把 `agent:triage` 改成 `agent:ready`；`agent-dispatch` 会在打标签时或每小时第 17 分钟认领（同时最多 3 个 `agent:wip`） |
| 交给本地 session | 把 `agent:triage` 改成 `agent:local`，在 app 里为它开一个独立 worktree 的 session（brief 写明 issue 号、验收标准、禁止 `make dev-reset` / `dev-down`）；PR 开出后 issue 改 `agent:review`，并给 PR 打开 Auto-fix |
| 批准高风险 PR | 在 PR 的 Conversation 底部发**普通评论** `/approve <head-sha 前 7 位>`，或在终端：`gh pr comment <PR> -R l0sgAi/qubar --body "/approve <sha>"`。不是 Review changes → Approve |
| 批准后又有新提交 | head sha 变了，之前的 `/approve` 自动失效，要对新 sha 再发一次 |
| 暂停某个 PR 的自动合并 | 打 `hold` 标签；去掉后 `pr-gate` 会重新评估 |
| 发版 | 合并 `develop → main` 的 PR（merge commit）。只改工作流 / 文档时 release 只跑 `changes`，线上不变 |
| 筛选巡检 issue | 看 `agent:found` 的 issue（`gh issue list -R l0sgAi/qubar -l agent:found`）：有价值就改成 `agent:triage` / `agent:ready` / `agent:local`，不需要就关闭。见下文「每日巡检」 |
| 查看 / 重置 agent 预算 | 见下文「月度预算」 |
| 看效果指标 | 每月 1 日 `agent-metrics` 自动跑；手动：`gh workflow run agent-metrics.yml -R l0sgAi/qubar -f month=2026-11`，看 run 的 Summary |

### 选哪种模式

| 用 `agent:ready`（Actions） | 用 `agent:local`（本地 session） |
|---|---|
| 改动小、需求清楚 | 触及高风险路径（鉴权、迁移、部署、CI、配置） |
| 低风险、单领域 | 跨领域，或需要先讨论 / 做决策 |
| 不需要额外上下文 | 需要本地记忆或 codegraph |
| | 在 Actions 里失败过 |

注意：

- 已经是 `agent:wip`（Actions 认领了）的 issue 不要改成 `agent:local`；先等它退回 `agent:triage`，或关掉它的 PR。
- 两种模式的 PR 闸口完全相同；区别只在谁写代码、出错谁修（Actions：`agent-fix`；本地：app 的 PR Auto-fix）。
- **开发栈 `qubar-dev` 是共享的**：多个 session 并行时，任何 session 都不能 `make dev-reset` / `make dev-down`。

## 排障

### `claude-review` 是红的

先看状态描述（PR 的 Checks 区，或 `gh api repos/l0sgAi/qubar/commits/<sha>/statuses`）：

| 描述 | 含义 / 处理 |
|---|---|
| `skipped: CLAUDE_CODE_OAUTH_TOKEN secret not set` | 仓库没有该 secret，见下文「轮换 token」 |
| `skipped: claude-review.yml is not on main yet` / `differs from main` | 本 PR 改了 `claude-review.yml`，或 develop 上的版本还没发版到 main。claude-code-action 只运行与 main 完全一致的工作流文件，属预期；规则要求它为必需检查时需要 admin 越过规则合并 |
| `Claude run failed, see run log` | 打开 run 看 `review` job。动作默认隐藏完整输出，看不到原因时用下文的诊断方法 |
| `Claude run failed, see run log`，日志里有 `Workflow initiated by non-human actor` | 触发者是 bot、且不在 `allowed_bots` 里（#122）。`claude-review` / `agent-fix` 已放行 `claude`、`github-actions`；出现别的 bot 名字时按需加入，不要用 `'*'`。临时绕过：维护者手动 `gh workflow run claude-review.yml -R l0sgAi/qubar --ref develop -f pr=<N>`（触发者是人） |
| `N blocking issue(s)` | 真实审查结论。`agent/` 分支会自动交给 `agent-fix`（最多 3 次） |

### 看不到 Claude 的报错原因

claude-code-action 默认隐藏完整输出，而改 Claude 工作流本身又会触发「与 main 不一致」而被跳过。做法：开一个**草稿 PR**，加一个临时 `pull_request` 工作流，直接用 `curl -fsSL https://claude.ai/install.sh | bash` 装 CLI，带 `CLAUDE_CODE_OAUTH_TOKEN` 跑 `claude -p ... --output-format json`，只打印 `result` 字段；看完关闭 PR 并删分支。#77（token 无效）和 #87（权限规则写法）都是这样定位的。

### `--allowedTools` 规则写法

- `Write(<路径>)` **永远不匹配**；写文件用不带路径的 `Write`，或 `Edit(tmp/**)`、`Edit(//绝对路径)`。
- `Edit(/路径)`（单斜杠）是相对项目根，不是绝对路径。
- Bash 里用 heredoc / `$(...)` / 管道 / `&&` 拼多行正文会被拒：先写文件，再 `--body-file` / `git commit -F`。
- **`gh pr comment` 在 Actions 里任何写法都被拒**（`--body`、`-F`、`--body-file`、精确匹配规则都不行）；
  `gh pr create`、`gh pr edit`、`gh issue comment`、`gh issue create`、`gh issue edit` 可以用。
  所以 PR 评论一律让 Claude 放进结构化输出（`--json-schema`），由工作流的 github-script 步骤代发：
  `claude-review` 的 `summary`、`agent-fix` 的 `note`。

## 每日巡检（`agent-patrol`，#114）

每天 01:23 UTC（北京时间 09:23）扫描 develop，按 `.github/ISSUE_TEMPLATE/` 的模板开 `agent:found` issue，等人筛选。

- 材料由工作流先收集到 `tmp/patrol/`：Go 代码里的 TODO / FIXME、`go test -short -cover ./pkg/...` 覆盖率、`govulncheck`、现有 issue 列表。
  Claude 只有 `Read,Glob,Grep`，把发现写进结构化输出；issue 由 github-script 步骤渲染模板后创建（与 #94 同一做法：Claude 不调 `gh`）。
  它不推分支、不开 PR、不改标签，不会把 issue 转成 `agent:ready`。
- 每天（UTC）最多开 N 个：手动运行的输入 `max_issues` > 仓库变量 `AGENT_PATROL_MAX_ISSUES` > 默认 3；当天已开的会扣掉，所以多跑几次也不会超。
- 去重：标题（忽略大小写、空白、标点）与任一 open issue 相同，或去重键与任一 open issue / 已关闭的巡检 issue 相同，就不开。
  去重键写在 issue 正文的 `<!-- agent-patrol-key: ... -->`。**关闭一个巡检 issue = 以后不再报同一个 key**；想让它重新被报告就改掉正文里的 key。
- 巡检 issue 的作者是 `github-actions[bot]`，正文首行有 `<!-- agent-patrol -->` 标记。每次运行的 Summary 列出了开了哪些、跳过了哪些及原因。
- 手动跑：`gh workflow run agent-patrol.yml -R l0sgAi/qubar`（可加 `-f max_issues=1`）。依赖告警用的是 `govulncheck`
  （`GITHUB_TOKEN` 读不了 Dependabot alerts API）；版本落后由 Dependabot 每周 PR 处理，巡检不重复报。

## 月度预算（`agent-budget`，#114）

`agent-dispatch` / `agent-fix` / `agent-patrol` 调 Claude 前先调用可复用工作流 `agent-budget.yml`，超出预算就不跑：

| 工作流 | 超出后的行为 |
|---|---|
| `agent-dispatch` | 不认领，issue 保持 `agent:ready`；本月剩余次数也限制每次认领的数量 |
| `agent-fix` | 不修复、不计入每个 PR 的 3 次，在 PR 上留言请人处理（同一 head 只留一次） |
| `agent-patrol` | 当天不巡检 |

超出时还会开一个标题为「agent 月度预算已用完：<工作流> <YYYY-MM>」的 issue（每个工作流每月一个），处理完手动关闭。
`claude-review` 不计入：它是每个 PR（包括人写的 PR）的必需检查，停掉会卡住所有合并；它每个 head 只审一次，`pr-sync` 合入 develop 时沿用结论。

**记录方式**：每次 Claude 运行结束上传一个 artifact，名字是 `agent-usage.<workflow>.<key>.<run_attempt>.t<tokens>`
（tokens = input + output + cache_creation，不含 cache_read；文件里有完整 usage、`total_cost_usd`、轮数），保留 90 天。
Claude 没跑起来（没有执行文件，例如工作流与 main 不一致被跳过）不计。

**配置**（仓库变量，不是 secret；只在 GitHub 上配置，不进仓库）：

| 变量 | 含义 | 默认 |
|---|---|---|
| `AGENT_BUDGET` | JSON：各工作流每月 Claude 运行次数上限 + 三者合计 token 上限，0 表示不限，没写的字段用默认 | `{"dispatch":60,"fix":90,"patrol":40,"tokens":0}` |
| `AGENT_BUDGET_RESET_AT` | 只统计这个时间之后的用量（重置用），ISO 8601，早于本月 1 日时无效 | 空 |
| `AGENT_PATROL_MAX_ISSUES` | 巡检每天最多开几个 issue | 3 |

```bash
# 查看本月用量：手动跑一次，看 run 的 Summary（表格列出各工作流次数、token 与上限）
gh workflow run agent-budget.yml -R l0sgAi/qubar
# 或直接列记录（改成本月 1 日）
gh api --paginate 'repos/l0sgAi/qubar/actions/artifacts?per_page=100' \
  --jq '.artifacts[] | select(.name | startswith("agent-usage.")) | select(.created_at >= "2026-11-01") | "\(.created_at) \(.name)"'

# 调整预算（整段 JSON 覆盖）
gh variable set AGENT_BUDGET -R l0sgAi/qubar --body '{"dispatch":80,"fix":120,"patrol":40,"tokens":30000000}'
# 本月提前重置：从现在起重新计数（下个月 1 日自然重置，不用改回）
gh variable set AGENT_BUDGET_RESET_AT -R l0sgAi/qubar --body "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# 临时完全停掉某个自动运行（预算 0 表示不限，不能用来暂停）
gh workflow disable agent-patrol.yml -R l0sgAi/qubar
```

`AGENT_BUDGET` 写错（不是合法 JSON）时预算检查失败，三个工作流都不会跑（fail closed），run 里能看到 jq 报错。

## 效果指标（`agent-metrics`，#114）

每月 1 日 02:37 UTC 统计上个月，只用 `gh api` + `jq`，结果在 run 的 Summary：

- **issue → 合入周期**：当月合入 develop 的 PR，从关联 issue 创建到合入的天数（中位数 / 平均 / P90），按模式分：
  `actions`（`agent/` 分支）、`local`（提交带 `Co-Authored-By: Claude`）、`dependabot`、`human`。
  develop 不是默认分支，GitHub 不建立 `Closes #N` 关联，所以从正文的 `Closes / Fixes / Resolves #N` 和 `agent/<N>-` 分支名解析。
- **每个 PR 的人工介入**：人工提交（非合并提交、作者是真人账号且没有 `Co-Authored-By: Claude`）、人工评论（不含 `/approve`）、
  `/approve` 次数、人工 review 次数；Summary 末尾有逐个 PR 的明细。
- **`agent:found` 采纳率**：当月新建、时间线里打过 `agent:found` 的 issue 中，后来打过 `agent:ready` 的比例（另列 `agent:local`、
  关闭未采纳、待筛选），按开 issue 的账号分来源（`patrol` = 巡检）。按创建月份统计，月底开的还没筛选完，重跑上个月数字会变。
- **agent 用量**：当月 `agent-usage.*` 记录的次数和 token。

手动：`gh workflow run agent-metrics.yml -R l0sgAi/qubar -f month=2026-10`（留空 = 上个月）。

## 轮换 `CLAUDE_CODE_OAUTH_TOKEN`

在**这个 app 之外**的终端（macOS Terminal / iTerm）执行，不要在聊天里用 `!`，否则 token 会进对话记录：

```bash
claude setup-token
gh secret set CLAUDE_CODE_OAUTH_TOKEN -R l0sgAi/qubar
gh secret list -R l0sgAi/qubar
```

第二条命令提示时粘贴 token（不回显）。完成后 ⌘K 清屏。token 有效期一年。

## 生效条件（为什么改了工作流没反应）

- `pull_request_target`（`pr-gate`）、`issues` / `issue_comment`（`agent-dispatch`、`claude`、`/approve`）、`schedule`（`pr-sync`、`agent-dispatch` 补位、`agent-patrol`、`agent-metrics`）、`workflow_run`（`agent-fix`）都只按 **main** 上的工作流运行。
- 用 claude-code-action 的工作流（`agent-dispatch`、`agent-fix`、`agent-patrol`、`claude-review`、`claude`）还要求文件与 main 上的版本完全一致（换 token 时校验，`claude-review` 已验证），手动 `workflow_dispatch` 也一样。
- 用 `GITHUB_TOKEN` 推的提交 / 打的标签 / 触发的合并不会再触发别的工作流（`workflow_dispatch` 除外）；claude[bot] 用 App token 推的会触发。
- 所以：改了工作流 → 合并到 develop → **发版到 main** 才生效。

## 验证记录（2026-10-18 ~ 19 激活时）

| 行为 | 验证于 |
|---|---|
| 低风险 PR：`risk-gate` 成功 → 开启 auto-merge → 检查通过后自动合并 | #75、#86、#90 |
| 高风险路径：自动打 `risk:high`、`risk-gate` 等待、不开 auto-merge | #76、#80、#81、#88、#94 |
| 维护者评论 `/approve <sha>` → `risk-gate` 秒级变绿；新提交使批准失效 | #80、#81、#94 |
| `claude-review` 真实审查并给出结论（OAuth token 有效） | #76、#86、#90 |
| OAuth token 无效时的报错（401）及诊断方法 | #77 |
| `--allowedTools` 写法、`gh pr comment` 在 Actions 里一律被拒 | #87、#91 |
| Dependabot 只改依赖的 PR 不依赖密钥通过 `claude-review` | #81（逻辑审阅；待真实 Dependabot PR 验证） |
