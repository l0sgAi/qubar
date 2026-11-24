# GitHub Pages 部署指南

本项目使用 GitHub Actions 自动部署到 GitHub Pages。

## 部署步骤

### 1. 启用 GitHub Pages

1. 进入你的 GitHub 仓库
2. 点击 **Settings** (设置)
3. 在左侧菜单找到 **Pages**
4. 在 **Source** 下拉菜单中选择 **GitHub Actions**

### 2. 配置基础路径 (重要)

如果你的仓库名不是 `qubar-frontend`，需要修改 [vite.config.js](vite.config.js) 中的 `base` 配置：

```javascript
base: '/your-repo-name/' // 替换为你的仓库名
```

### 3. 推送代码触发部署

当你推送代码到 `main` 或 `master` 分支时，GitHub Actions 会自动构建并部署：

```bash
git add .
git commit -m "Your commit message"
git push origin main
```

### 4. 查看部署状态

1. 在 GitHub 仓库中点击 **Actions** 标签
2. 查看最新的工作流运行状态
3. 构建成功后，访问 `https://qubar.site/`

## 手动触发部署

你也可以手动触发部署：

1. 在 GitHub 仓库中点击 **Actions** 标签
2. 选择 **Deploy to GitHub Pages** 工作流
3. 点击 **Run workflow** 按钮
4. 选择分支并点击 **Run workflow**

## 工作流说明

[`.github/workflows/deploy.yml`](.github/workflows/deploy.yml) 工作流会：

1. **CI 检查**：调用 [`.github/workflows/ci.yml`](.github/workflows/ci.yml)，运行单元测试（含覆盖率门槛）与生产构建；**任一失败则终止，不会部署**
2. **安装依赖**：使用 `npm ci` 安装项目依赖
3. **构建项目**：运行 `npm run build` 生成生产环境构建
4. **部署**：将构建产物部署到 GitHub Pages

## CI 检查与分支保护

`ci.yml` 会在每个指向 `main` 的 PR 上自动运行，包含两个检查：

| 检查 | 内容 |
|---|---|
| `unit` | `npm run test:coverage`：全部单元测试 + 覆盖率门槛 |
| `build` | `npm run build`：生产构建 |

GitHub Actions 无法自行开启分支保护，需要仓库管理员手动设置：

1. 进入仓库 **Settings** → **Branches**（或 **Rules → Rulesets**），为 `main` 添加规则
2. 勾选 **Require a pull request before merging**
3. 勾选 **Require status checks to pass before merging**，搜索并添加 `unit`、`build`
4. 勾选 **Require branches to be up to date before merging**
5. （可选）勾选 **Do not allow bypassing the above settings**

> 必须等 CI 在某个 PR 上至少运行过一次，`unit` / `build` 才会出现在可选列表中。

## 配置 Google OAuth 回调

部署到 GitHub Pages 后，你需要更新 Google OAuth 应用设置：

### 授权重定向 URI

在 Google Cloud Console 中，将以下地址添加到授权重定向 URI：

```
https://qubar.site/success
```

本项目已配置自定义域名 `qubar.site`（见 `public/CNAME`），直接使用该域名即可；若使用默认 GitHub Pages 域名，请将上述地址替换为 `https://your-username.github.io/your-repo-name/success`。

## 本地开发

本地开发时使用：

```bash
npm run dev
```

访问 `http://localhost:5173`

## 常见问题

### 构建失败

- 若失败发生在 `ci / unit` 或 `ci / build`，说明检查未通过、未部署：本地运行 `npm run check` 复现
- 检查 Node.js 版本是否匹配（当前使用 Node.js 20）
- 确保 `package.json` 中的依赖版本正确
- 查看 Actions 日志获取详细错误信息

### 页面样式问题

- 确保 `vite.config.js` 中的 `base` 路径配置正确
- 检查资源路径是否使用相对路径

### 路由 404 错误

GitHub Pages 只支持单页应用，如果遇到刷新页面 404 的问题，可能需要：
- 使用 Hash 模式路由（`createWebHashHistory`）
- 添加 404 重定向页面
