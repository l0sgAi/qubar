# 仓库闸口配置（l0sgAi/qubar#67 P2）

GitHub 上的分支规则、仓库设置和标签不在代码里，这里记录它们的权威定义和应用命令（需要仓库 admin）。
修改这些设置时先改本目录的文件、走 PR，合并后再执行命令。

## develop 分支规则（`develop.json`）

- 只能通过 PR 合入；禁止 force push、禁止删除分支。
- 必须通过的检查：`go` / `db` / `image` / `iac` / `gitleaks` / `integration`，且来源限定为 GitHub Actions（`integration_id: 15368`），防止伪造的同名 status 放行。新增必须的 CI job 时同步加到这里。
- 要求分支与 develop 保持最新（strict）。没有 merge queue（个人账号仓库不支持），由它防止两个各自绿的 PR 合并后互相冲突；落后时 `gh pr update-branch <PR>` 或在 PR 页面点 Update branch。
- 不要求 approve：仓库只有一个人，GitHub 不允许作者 approve 自己的 PR。人工审核靠 `risk:high` 标签 + 不开 auto-merge 的约定（见下）。
- admin 可以在 PR 上越过规则合并（`bypass_mode: pull_request`），但不能直接 push 到 develop。

```bash
# 首次创建
gh api -X POST repos/l0sgAi/qubar/rulesets --input .github/rulesets/develop.json
# 修改：先查 id，再整体更新
id=$(gh api repos/l0sgAi/qubar/rulesets --jq '.[] | select(.name=="develop") | .id')
gh api -X PUT repos/l0sgAi/qubar/rulesets/$id --input .github/rulesets/develop.json
```

## 仓库设置

```bash
gh api -X PATCH repos/l0sgAi/qubar -F allow_auto_merge=true -F delete_branch_on_merge=true
```

## 标签

| 标签 | 含义 |
|---|---|
| `agent:triage` | 新 issue，spec 待人审核（issue 模板默认带上） |
| `agent:ready` | spec 已审核，agent 可以认领 |
| `agent:wip` | agent 开发中 |
| `agent:review` | PR 已开，等 CI / review |
| `agent:found` | AI 巡检发现的问题，待人筛选 |
| `risk:high` | 触及高风险路径，必须人工审核，不开 auto-merge（`risk-labeler` 按 `.github/labeler.yml` 自动打） |

```bash
gh label create agent:triage --color FBCA04 --description "新 issue，spec 待人审核" --force
gh label create agent:ready  --color 0E8A16 --description "spec 已审核，agent 可认领" --force
gh label create agent:wip    --color 1D76DB --description "agent 开发中" --force
gh label create agent:review --color 5319E7 --description "PR 已开，等 CI / review" --force
gh label create agent:found  --color C5DEF5 --description "AI 巡检发现，待人筛选" --force
gh label create risk:high    --color B60205 --description "高风险路径，必须人工审核，不开 auto-merge" --force
```
