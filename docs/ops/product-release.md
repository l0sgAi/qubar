# API 与 Pages 统一发布

前端在 `web/`，Go 模块、迁移和部署目录在仓库根。业务提交进 `develop`，发布负责人用 **Create a merge commit** 将 `develop` 合入 `main`。[跨仓交接与验收](https://github.com/l0sgAi/qubar-frontend/issues/62)

## 发布范围

`release-prod` 串行处理发布和 Pages 预览。API 与 Web 对比最近发布的产品 tag。基础设施和迁移对比最近成功 release 的 SHA。发布前可在有完整历史的仓库执行 `python3 scripts/release-plan.py <上次运行SHA> <候选SHA> <上个产品tag>` 和 `python3 scripts/test-product-release.py`。

| 变更 | 构建与部署 | 版本与恢复点 |
| --- | --- | --- |
| 仅 Web | Linux 测试、构建并暂存 tar，核实运行中的 API、检查兼容接口，再部署 Pages | 新版本记录运行中 API 的 digest |
| 仅 API | Go 测试、构建镜像，兼容迁移、按 digest 部署、外网检查 | 将当前已发布的静态包按原字节纳入新版本 |
| 两端或共享契约 | 并行测试和构建，固定两份产物，依次执行迁移、API、Pages 和用户流程验收 | 一个产品 tag。共享契约包括 `docs/api/` 和 `*frontend-api.md` |
| 仅迁移或基础设施 | 按改动运行 Terraform / Ansible 或 migrate，重新登记 connector，记录运行镜像、SHA 和部署运行 | 保留已有产品版本 |
| 一般文档或纯导入 | PR 检查上报结果，纯导入构建待切换候选包 | 保留已有产品版本 |

首次合入这组三个 PR 会改动 `release.yml`，按现有规则执行 **changes、web-build、infra、deploy**。其中 deploy 重新登记 connector、读取实际镜像并冒烟。默认跳过 Go 镜像、数据库迁移、生产 Pages 和产品 Release。首次切换由负责人在同一 `main` 上运行 release 并勾选 `force_web`。如混入新的业务改动，应在发布窗口重新执行预演。

## 首次切换

1. 取回旧仓运行 45387861482 的内层 `artifact.tar`。交接时原包已单独留存。在临时目录将其复制为 `pages.tar`，核对 SHA-256 为 `1444231edc98dfad5ede59111a6d1b1fd8cf470bd0a8ecfbf283263164e60a4b`。执行 `python3 scripts/verify-pages.py <pages.tar>`。初始恢复点还记录了 v0.9.0 的生产镜像和两条原部署运行。[基线清单](web-baseline-v0.9.0.json)
2. 发布负责人将基线清单复制为同目录的 `manifest.json`，在主仓持久保存这两个原文件。在该目录执行下列命令，然后从 Release 重新下载并校验摘要。原包不可用时先从维护者取回。重新构建会产生独立摘要，应走新的基线核对流程。

   ```bash
   gh release create web-baseline-v0.9.0 pages.tar manifest.json -R l0sgAi/qubar \
     --target 8828539beb628f4e43379f601d53738744b6397d --latest=false \
     --title 'v0.9.0 Pages 接管恢复点' --notes '接管前的 API 与 Pages 恢复清单。'
   ```

3. 在主仓设置 Pages 来源 **GitHub Actions**，限制 `github-pages` 和 `product-acceptance` environment 到 `main`。为后者设置实际执行账号冒烟的 required reviewer。单维护者仓库需要允许发起人完成此项验收。生产工作流在修改服务之前会检查该验收环境。
4. 将临时子域 `preview.qubar.site` 指向主仓 Pages 并等待 HTTPS 就绪。运行 **pages-preview**，输入首次 main release 的运行 ID。它会核实运行来源、提交和临时域名，取回原 tar，在独立域名根路径验证资源和深链。两次部署共用生产并发组。
5. 在发布窗口冻结旧仓 Pages 部署，记录原 Pages 设置和 DNS。将 `qubar.site` 的自定义域名绑定从旧仓移至主仓，核对 DNS 和 HTTPS，运行 main 的 release / `force_web`。Actions 部署的自定义域名按仓库 Pages 设置生效。[GitHub Pages 配置](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site)
6. API 作业先读取容器实际运行的镜像，调用 `/healthz`、`/readyz` 和旧前端使用的 `/category/get`。Pages 自动检查域名根、SPA fallback、落地页、入口静态资源和微软身份 JSON。发布负责人用测试账号完成下表，在环境审核备注中记录结果后放行 Latest。

| 流程 | 验收 |
| --- | --- |
| OAuth | 登录后从 `/success` 回到站内，刷新后身份正常 |
| 关注 | 关注、刷新、取关，关系与计数一致 |
| 私信 / SSE | 从 `/messages/:userId` 首次会话、发消息，对端收到未读更新，读后清零 |
| 分享 | 打开帖子分享深链并直接刷新，正文与静态资源正常 |
| 邀请 | 从 `/invite/:token` 预览并加入，刷新后状态正确 |

## 产物、部分失败与重试

- 每个候选在部署前写入同一产品号的 GitHub Release 草稿。`candidate.json` 固定 SHA、镜像 digest、静态包摘要和上一个恢复点。`pages.tar` 在 Release 中持久保存。API 部署按 digest 拉取。已发布版本可用相同 SHA 的 `force_deploy` 或 `force_web` 重新部署其留存产物。
- API 成功后，如果 Pages 部署、静态冒烟或用户流程验收失败，`recover-pages` 部署已校验的上一份包并再次冒烟。兼容 API 继续运行，草稿保留。若证书或 Pages 控制面故障影响自动恢复，负责人将域名切回旧仓并恢复旧仓部署。数据库执行 forward-only 迁移，紧急 API 回滚沿用 release 的 `rollback`。
- 修复环境后在原运行选择 **Re-run all jobs**。新 runner 从同一草稿取回原镜像 digest 和静态包，跳过重建。发生恢复后，最终发布要求本次 attempt 成功部署相同候选。只重跑失败的末尾步骤不足以发布。如果候选业务代码必须更换，由负责人先确认恢复后的线上组合，保存草稿产物及摘要，再撤回草稿并发起新版本。
- 只有自动冒烟和受保护的账号验收成功后才写入 `manifest.json`、给实际运行的镜像打产品号标签并发布 Latest。清单含产品 SHA、API 镜像、Pages SHA-256、可下载 URL、部署运行和上一份恢复清单。
- 首次统一发布和恢复演练均完成后，在交接单回填链接并归档旧仓。保留历史 PR、review、部署和截图。未发布的 `feature/like-optimization-20260926`（`36ad196`）与 `feature/mobile-compatibility-20260911`（`06886d8`）继续保存在旧仓，归档前由维护者逐一记录移植 issue 或放弃原因。
