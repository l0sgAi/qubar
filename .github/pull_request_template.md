<!-- 一个 PR 只解决一个 issue；目标分支 develop。规则见 CLAUDE.md -->

Closes #

## 改动

-

## 验证

- [ ] `make verify` 通过
- [ ] 手动验证（`make dev-up && make dev-run` 后调用的接口 / 观察到的结果）：

## 未验证 / 已知限制

<!-- 例如：开发栈无 CDC，ES 同步未验证；依赖 S3 / OAuth / 邮件的路径未验证 -->

## 风险

- [ ] 触及高风险路径（迁移、部署、CI、鉴权 / 加密 / SSRF、配置、agent 规则）：已自动打 `risk:high`，需人工审核，不开 auto-merge
- [ ] 包含迁移：兼容正在运行的旧版本应用（expand / contract，见 migrations/README.md）
