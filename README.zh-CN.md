# Qubar

[English](README.md) | **简体中文**

一个基于 Go 语言、采用 DDD 领域驱动设计的现代化兴趣社区后端，类似百度贴吧/Reddit 社区。

## ✨ 功能特性

Qubar 是一个完整的兴趣社区平台，提供：

- **兴趣圈（圈子）** - 用户可创建、加入不同主题的兴趣社区
- **内容发布** - 支持图文帖子、多媒体上传
- **评论互动** - 二级扁平化评论结构，支持回复和点赞
- **用户系统** - 邮箱密码注册 + 多平台 OAuth 登录
- **权限管理** - 圈主/管理员/成员三级 RBAC 权限
- **圈子管理** - 任免管理员、转让圈主、禁言、拉黑、入圈审核、资料编辑
- **AI 机器人** - 平台超管维护的全局机器人 + 圈主/管理员维护的圈内机器人，支持关键词/手动/@提及触发
- **消息中心** - 基于 Redpanda 的通知扇出，未读数、标记已读，SSE 实时未读推送
- **@提及** - 帖子/评论 @提及精确绑定并持久化
- **全文搜索** - 支持用户、圈子、帖子的搜索引擎
- **发现与推荐** - 首页推荐流、热榜、发现页、浏览历史、收藏
- **异步统计** - 高并发场景下的 Write-Behind 缓存策略

## 🛠 技术栈

### 核心框架

- **[CloudWeGo Hertz](https://github.com/cloudwego/hertz)** - 高性能 HTTP 框架（替代原 Gin）
- **[CloudWeGo Eino](https://github.com/cloudwego/eino)** - LLM 应用编排框架（OpenAI / Claude / Gemini 模型组件）
- **[GORM](https://gorm.io/)** - ORM 数据库操作
- **[Sa-Token](https://github.com/dromara/sa-token)** - 轻量级权限认证框架
- **[Viper](https://github.com/spf13/viper)** - 配置管理
- **[Zap](https://github.com/uber-go/zap)** - 高性能日志库

### 数据存储与中间件

| 组件 | 版本 | 用途 |
|------|------|------|
| **PostgreSQL** | 18 | 主数据库，UUIDv7 主键，JSONB 支持 |
| **Redis** | 7+ | 缓存与会话存储，Lua 脚本原子操作 |
| **Elasticsearch** | 8.x | 全文检索，经 CDC 实时索引同步 |
| **Redpanda** | latest | Kafka 兼容的消息队列，异步统计聚合与通知扇出 |
| **AWS S3** | - | 对象存储（图片/视频），支持预签名 URL |
| **Nacos** | 3.x | 配置中心，支持多环境管理 |
| **Mailtrap** | - | 邮件发送服务（验证码/通知） |

### 认证方式

- Google OAuth 2.0
- GitHub OAuth
- Azure AD OAuth
- 邮箱密码 + 验证码注册登录

## 🏗 架构设计

### DDD 领域驱动设计

项目采用**模块化单体**架构，按领域边界划分包，未来可平滑拆分为微服务：

```
pkg/
├── composition/          # 编排层：装配依赖、注册路由、跨领域 Facade 桥接
│   ├── hertzadapter/    # Hertz 框架适配 → 框架无关路由抽象
│   └── middleware/      # 全局中间件（CORS、日志）
│
├── domains/             # 领域层（每个领域独立自治）
│   ├── auth/            # 认证领域（登录、注册、OAuth）
│   ├── user/            # 用户领域（资料、搜索）
│   ├── category/        # 分类领域（圈子分类）
│   ├── circle/          # 圈子领域（创建、成员、权限、管理）
│   ├── post/            # 帖子领域（发布、列表、详情）
│   ├── comment/         # 评论领域（二级扁平化结构）
│   ├── like/            # 点赞领域（原子操作 + 事件）
│   ├── collect/         # 收藏领域
│   ├── history/         # 浏览历史领域
│   ├── discover/        # 发现页聚合领域
│   ├── trending/        # 热榜领域
│   ├── recommend/       # 首页推荐领域
│   ├── notice/          # 消息中心领域（通知 + SSE 未读推流）
│   ├── aiagent/         # AI 机器人领域（全局 + 圈内机器人、回复触发）
│   ├── storage/         # 存储领域（文件上传）
│   └── [领域]/
│       ├── application/ # 应用服务层：用例编排
│       ├── domain/      # 领域层：模型、仓库接口、核心业务规则
│       ├── infrastructure/ # 基础设施层：仓库实现、缓存、搜索、事件
│       └── interfaces/http/ # 接口层：Handler、路由、DTO
│
├── shared/              # 共享内核（领域无关）
│   ├── appctx/          # 上下文抽象
│   ├── domain/          # 领域基类（BaseModel）
│   ├── httputil/        # HTTP 响应工具
│   └── routing/         # 框架无关路由抽象
│
├── conf/                # 配置加载（Nacos + 本地兜底）
└── logger/              # 日志初始化
```

### 关键设计决策

1. **UUIDv7 主键** - 前 48 位为时间戳，字典序 = 时间序，天然支持 keyset 游标分页
2. **框架无关路由** - 通过 `routing.RouterGroup` 抽象，领域代码不依赖 Hertz
3. **跨领域 Facade** - 领域间通过接口调用，不直接耦合，拆分微服务时只需换实现
4. **Write-Behind 缓存** - Redis 实时更新 + Redpanda 异步批量落库，应对高并发
5. **二级扁平化评论** - `root_id` 标记层级，避免递归查询，支持高效分页
6. **AI 机器人双作用域隔离** - 全局机器人（平台超管）与圈内机器人（圈主/管理员）完全隔离：跨作用域访问一律 404，不暴露存在性

## 🤖 AI 机器人

Qubar 内置两层 AI 机器人体系：

### 全局机器人（`/agent/*`）

- 仅**平台超管**（`users.role=1`）维护
- 全站生效的回复触发：评论关键词匹配、手动触发、发帖 @提及

### 圈内机器人（`/circle/agent/*`）

- 由**圈主/管理员**管理，每圈上限 **5** 个
- 字段级权限矩阵：
  - 列表/详情/创建/更新运营字段（名称、头像、模型、提示词、触发配置、限流、状态）：**圈主 + 管理员**
  - 凭据字段（`api_protocol` / `base_url` / `api_key`）与删除：**仅圈主**
- 机器人只回复**本圈**帖子；创建时兜底限流（30 次/时 + 60 秒间隔），防止圈主的 API key 被刷爆
- @提及可见范围护栏：圈内机器人在圈外的 @选人列表中被过滤（`users.agent_circle_id` 经 CDC 同步至 ES）

### 回复触发方式

| 触发方式 | 说明 |
|---------|------|
| 关键词（`trigger_mode=2`） | 新评论命中配置的关键词即触发机器人回复（异步、静默失败） |
| @提及 | 发帖时 @启用中的机器人直接触发，不校验 mode |
| 手动（`trigger_mode=3`） | `POST /agent/:id/reply/:postId`（超管）或 `POST /circle/agent/:id/reply/:postId`（圈主），同步执行 |

### 安全设计

- `api_key` **加密存储**，任何接口不回显明文/密文，响应仅含 `has_api_key` + `api_key_masked` 掩码
- 每个机器人绑定一个系统用户账号（`role=2`）作为发言身份
- 权限每次**直查成员记录**（无缓存）：任免管理员/转让圈主/禁言后下一次请求即生效

设计文档：[docs/agent-reply-design.md](docs/agent-reply-design.md)、[docs/circle-agent-manage-design.md](docs/circle-agent-manage-design.md)，接口文档：[docs/circle-agent-manage-api.md](docs/circle-agent-manage-api.md)

## 📁 项目结构

```
qubar/
├── cmd/
│   ├── main.go           # 程序入口
│   └── apps/
│       └── server.go     # 服务初始化与资源编排
│
├── configs/
│   ├── config.yaml       # 本地配置文件（Nacos 不可用时兜底）
│   └── bootstrap.yaml    # Nacos 引导配置（地址、命名空间、分组）
│
├── docs/
│   ├── pgsql-ddl/        # 数据库表结构（UUIDv7 主键版，按领域拆分）
│   ├── db.md             # DDL 跳转入口（指向 pgsql-ddl/）
│   ├── api/              # API 文档
│   └── design/           # 设计文档
│
├── pkg/
│   ├── composition/      # 编排层（见架构说明）
│   ├── domains/          # 业务领域（见架构说明）
│   ├── shared/           # 共享内核
│   ├── conf/             # 配置管理
│   ├── logger/           # 日志配置
│   └── server/           # 遗留基础设施（逐步迁移中）
│       ├── auth/         # OAuth Provider 实现
│       ├── storage/      # DB/Redis/ES/Redpanda/S3 初始化
│       └── utils/        # 工具函数
│
├── go.mod
└── go.sum
```

## 🚀 快速开始

### 环境要求

- Go 1.25.4+
- PostgreSQL 18（需启用 `uuidv7()` 函数）
- Redis 7+
- Elasticsearch 8.x（可选，无则降级为 DB 搜索）
- Redpanda（可选，无则统计仅走 Redis）

### 1. 克隆项目

```bash
git clone https://github.com/l0sgAi/qubar.git
cd qubar
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 配置数据库

创建数据库和 schema：

```sql
CREATE DATABASE qubar;
CREATE SCHEMA IF NOT EXISTS domains;
```

数据库表结构及种子数据请参考 [docs/pgsql-ddl/](docs/pgsql-ddl/)（按领域拆分，入口 [README.md](docs/pgsql-ddl/README.md)）

### 4. 配置应用

#### 方式一：本地配置（快速开发）

编辑 `configs/config.yaml`，填入数据库、Redis 等连接信息：

```yaml
server:
  port: 8888
  mode: debug

pgsql:
  path: 127.0.0.1
  port: 5432
  db_name: qubar
  username: your_username
  password: your_password

redis:
  host: 127.0.0.1
  port: 6379
  db: 0
```

#### 方式二：Nacos 配置中心（生产推荐）

创建 `configs/bootstrap.yaml`：

```yaml
nacos:
  endpoint: "your-nacos-address:8848"
  namespace: "dev-namespace-uuid"
  group: "QUBAR_GROUP"
  data_id: "qubar-dev-conf"
  username: "nacos"
  password: "nacos"
```

### 5. 运行应用

```bash
# 本地配置启动
go run cmd/main.go -c configs/config.yaml -b ""

# Nacos 配置启动
go run cmd/main.go -c configs/config.yaml -b configs/bootstrap.yaml
```

服务将在 `http://localhost:8888` 启动

## 🌐 API 端点

### 认证（无需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/auth/google/login` | Google OAuth 登录跳转 |
| `GET` / `POST` | `/auth/google/callback` | Google OAuth 回调 / 一次性 code 换 token |
| `GET` | `/auth/github/login` | GitHub OAuth 登录 |
| `GET` / `POST` | `/auth/github/callback` | GitHub OAuth 回调 / code 换 token |
| `GET` | `/auth/azure/login` | Azure AD OAuth 登录 |
| `GET` / `POST` | `/auth/azure/callback` | Azure AD OAuth 回调 / code 换 token |
| `POST` | `/auth/register/send-code` | 发送注册验证码 |
| `POST` | `/auth/register/verify` | 校验验证码 |
| `POST` | `/auth/register/complete` | 完成注册 |
| `POST` | `/auth/login` | 邮箱密码登录 |
| `POST` | `/auth/password/send-code` | 发送找回密码验证码 |
| `POST` | `/auth/password/verify` | 校验找回密码验证码 |
| `POST` | `/auth/password/reset` | 重置密码 |

### 用户（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/user/get` | 获取当前登录用户 |
| `PUT` | `/user/update` | 修改用户资料 |
| `GET` | `/user/search` | 搜索用户 |
| `GET` | `/user/detail/:id` | 获取用户详情 |
| `POST` | `/auth/logout` | 注销当前 Token |

### 分类（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/category/get` | 获取全部分类列表 |

### 圈子

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/circle/create` | 创建新圈子（需登录） |
| `GET` | `/circle/list` | 搜索/浏览圈子列表（访客可读） |
| `GET` | `/circle/active` | 近期活跃圈子（访客可读） |
| `GET` | `/circle/random` | 随机圈子（访客可读） |
| `GET` | `/circle/detail/:id` | 获取圈子详情（访客可读） |
| `GET` | `/circle/user` | 指定用户加入的圈子（访客可读） |
| `GET` | `/circle/posts` | 圈内帖子列表（访客可读） |
| `GET` | `/circle/my` | 我加入的圈子（需登录） |
| `POST` | `/circle/join` | 申请加入圈子（需登录） |
| `POST` | `/circle/leave` | 退出圈子（需登录） |
| `GET` | `/circle/members` | 成员列表（admin+） |
| `GET` | `/circle/manage/list` | 我可管理的圈子（owner/admin） |
| `POST` | `/circle/manage/role` | 任免管理员（owner） |
| `POST` | `/circle/manage/transfer` | 转让圈主（owner） |
| `POST` | `/circle/manage/mute` / `unmute` | 禁言 / 解除禁言（admin+） |
| `POST` | `/circle/manage/ban` / `unban` | 拉黑 / 解除拉黑（admin+） |
| `POST` | `/circle/manage/review` | 入圈审核（admin+） |
| `PUT` | `/circle/update` | 编辑圈子资料（owner/admin 分字段） |

### 帖子

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/post/create` | 发布新帖子（需登录，支持 @提及） |
| `GET` | `/post/list` | 搜索帖子列表（访客可读） |
| `GET` | `/post/home` | 首页推荐流（访客可读） |
| `GET` | `/post/my` | 我的帖子（需登录） |
| `GET` | `/post/user/:user_id` | 指定用户的帖子（访客可读） |
| `GET` | `/post/detail/:id` | 帖子详情（访客可读） |

### 评论

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/comment/create` | 发布评论/回复（需登录） |
| `GET` | `/comment/list` | 顶层评论列表（访客可读） |
| `GET` | `/comment/replies` | 楼层内回复列表（访客可读） |
| `GET` | `/comment/detail/:id` | 单条评论详情（访客可读） |

### 点赞与收藏（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/like/toggle` | 点赞/取消点赞（帖子/评论通用） |
| `POST` | `/collect/toggle` | 收藏/取消收藏帖子 |
| `GET` | `/collect/posts` | 我收藏的帖子 |

### 消息中心（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/notice/list` | 通知列表 |
| `GET` | `/notice/unread-count` | 未读数 |
| `POST` | `/notice/read` | 标记已读 |
| `POST` | `/notice/read-all` | 全部已读 |
| `GET` | `/notice/stream` | SSE 未读数实时推送流 |

### 发现与历史

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/discover/` | 发现页聚合（需登录） |
| `GET` | `/trending/` | 热榜帖子（访客可读） |
| `GET` | `/history/posts` | 浏览历史（需登录） |

### AI 机器人 - 全局（仅平台超管）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/agent` | 创建全局机器人 |
| `GET` | `/agent/list` | 机器人列表（offset 分页） |
| `GET` | `/agent/:id` | 机器人详情 |
| `PUT` | `/agent/:id` | 更新机器人（部分更新） |
| `DELETE` | `/agent/:id` | 软删机器人 |
| `POST` | `/agent/:id/reply/:postId` | 手动触发机器人回复 |

### AI 机器人 - 圈内（圈主/管理员）

| 方法 | 路径 | 说明 | 圈内权限 |
|------|------|------|---------|
| `POST` | `/circle/agent` | 创建圈内机器人（每圈 ≤5） | admin+ |
| `GET` | `/circle/agent/list` | 圈内机器人列表 | admin+ |
| `GET` | `/circle/agent/:id` | 机器人详情 | admin+ |
| `PUT` | `/circle/agent/:id` | 更新机器人 | 运营字段 admin+；凭据字段仅圈主 |
| `DELETE` | `/circle/agent/:id` | 软删机器人 | 仅圈主 |
| `POST` | `/circle/agent/:id/reply/:postId` | 手动触发回复（帖子须属本圈） | 仅圈主 |

### 文件上传（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/upload/image` | 单张图片上传 |
| `POST` | `/upload/post-images` | 帖子多图上传 |
| `POST` | `/upload/video` | 视频上传 |
| `DELETE` | `/upload/delete` | 删除文件 |
| `GET` | `/upload/presign` | 获取预签名上传 URL |

### 请求头

所有需登录接口请在请求头中携带：

```bash
satoken: your-token-here
```

## 🔐 安全特性

- ✅ CORS 跨域保护（可配置允许的源）
- ✅ Sa-Token 会话管理（3 天有效期，30 分钟活跃超时）
- ✅ RBAC 基于角色的访问控制
- ✅ 邮箱验证码注册
- ✅ 逻辑删除数据保护
- ✅ S3 预签名 URL（无需暴露凭证）
- ✅ 机器人 API key 加密存储，响应仅回掩码
- ✅ 机器人双作用域隔离（全局 ↔ 圈内，跨作用域 404）

## ⚡ 性能优化

1. **Redis 多级缓存** - 用户资料、圈子信息、统计数据分层缓存
2. **Lua 原子操作** - 点赞、浏览计数通过 Lua 脚本保证原子性
3. **Write-Behind 策略** - 统计更新先写 Redis，Redpanda 异步批量落库
4. **覆盖索引优化** - PostgreSQL 精心设计的索引避免回表
5. **ES 全文检索** - 热门搜索走 Elasticsearch，冷数据走 DB（帖子经外部 CDC 进 ES）
6. **JSONB 字段** - 多媒体、扩展信息用 PostgreSQL JSONB 存储

## 📝 开发指南

### 添加新领域

1. 在 `pkg/domains/` 下创建领域目录，遵循 `application/domain/infrastructure/interfaces` 分层
2. 在 `pkg/composition/` 中注册依赖装配和路由
3. 如需跨领域调用，在 `composition/facade_bridges.go` 中添加桥接实现

### 添加新 OAuth Provider

1. 在 `pkg/server/auth/` 中添加 Provider 实现（参考 `google.go`）
2. 在 `provider.go` 中注册
3. 更新 `auth` 领域路由

### 配置说明

关键配置项说明：

```yaml
# CORS 允许的源
cors:
  allowed_origins:
    - "https://qubar.site"
    - "http://localhost:*"

# Sa-Token 会话配置
sa_token:
  token_name: "satoken"
  timeout: 259200        # 3 天（秒）
  active_timeout: 1800   # 30 分钟活跃检测
  is_concurrent: true    # 允许并发登录

# 文件上传大小限制（代码中配置 50MB）
# server.WithMaxRequestBodySize(50 << 20)
```

## 🧪 测试

```bash
# 运行单元测试
go test ./pkg/...

# 运行特定包测试
go test ./pkg/composition/middleware/...
```

## 📄 许可证

[MIT License](LICENSE)

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📧 联系方式

如有问题或建议，请提交 Issue 或联系维护者。

---

**注意**: 首次运行前请确保正确配置所有必要参数，特别是数据库连接和 OAuth 凭证。
