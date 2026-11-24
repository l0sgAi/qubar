# 前端构建与交接

主仓根目录的 CI 在 Linux / Node 20.20.2 下执行 `npm ci`、覆盖率测试和生产构建，下载并校验同次上传的 `github-pages/artifact.tar`。本地在主仓根目录运行 `npm --prefix web ci` 和 `npm --prefix web run check`。

生产包使用站点根路径 `/`，包含 `index.html`、相同内容的 `404.html`、`landing/`、`assets/` 和 `.well-known/`。预览该包时使用独立域名根路径。GitHub Pages 的域名绑定在仓库 Settings → Pages 管理。

迁移期间由旧仓提供线上站点。主仓 PR 目标为 `develop`；发布负责人在域名、证书、回退包和冒烟流程就绪后接管。[迁移交接单](https://github.com/l0sgAi/qubar-frontend/issues/62)
