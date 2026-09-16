# Qubar

**English** | [简体中文](README.zh-CN.md)

A modern interest-based community backend written in Go with DDD (Domain-Driven Design), similar to Baidu Tieba / Reddit.

## ✨ Features

Qubar is a full-featured interest community platform offering:

- **Interest Circles** – Users can create and join topic-based communities
- **Content Publishing** – Rich text posts with image/video uploads
- **Comments** – Two-level flattened comment structure with replies and likes
- **User System** – Email/password registration + multi-platform OAuth login
- **Permission Management** – Three-tier RBAC (owner / admin / member) per circle
- **Circle Management** – Role assignment, ownership transfer, mute, ban, join review, profile editing
- **AI Agents (Bots)** – Global agents managed by platform admins + circle-scoped agents managed by circle owners/admins, with keyword / manual / @mention triggers
- **Message Center** – Notification fan-out via Redpanda, unread count, mark-read, and SSE real-time unread push
- **@Mentions** – Precise mention binding persisted for posts and comments
- **Full-text Search** – Search engine for users, circles and posts
- **Discovery & Recommendation** – Home feed, trending posts, discover page, browsing history, favorites
- **Async Statistics** – Write-Behind cache strategy for high-concurrency scenarios

## 🛠 Tech Stack

### Core Frameworks

- **[CloudWeGo Hertz](https://github.com/cloudwego/hertz)** – High-performance HTTP framework (replaced Gin)
- **[CloudWeGo Eino](https://github.com/cloudwego/eino)** – LLM application framework (OpenAI / Claude / Gemini model components)
- **[GORM](https://gorm.io/)** – ORM for database access
- **[Sa-Token](https://github.com/dromara/sa-token)** – Lightweight authentication framework
- **[Viper](https://github.com/spf13/viper)** – Configuration management
- **[Zap](https://github.com/uber-go/zap)** – High-performance logging

### Data Stores & Middleware

| Component | Version | Purpose |
|-----------|---------|---------|
| **PostgreSQL** | 18 | Primary database, UUIDv7 primary keys, JSONB support |
| **Redis** | 7+ | Cache & session store, atomic ops via Lua scripts |
| **Elasticsearch** | 8.x | Full-text search with real-time index sync (via CDC) |
| **Redpanda** | latest | Kafka-compatible message queue for async stats aggregation & notification fan-out |
| **AWS S3** | - | Object storage (images/videos) with presigned URLs |
| **Nacos** | 3.x | Configuration center with multi-environment support |
| **Mailtrap** | - | Email delivery (verification codes / notifications) |

### Authentication

- Google OAuth 2.0
- GitHub OAuth
- Azure AD OAuth
- Email/password with verification code

## 🏗 Architecture

### DDD (Domain-Driven Design)

The project follows a **modular monolith** architecture, with packages split by domain boundaries so it can be smoothly decomposed into microservices later:

```
pkg/
├── composition/          # Composition layer: wire dependencies, register routes, cross-domain Facade bridges
│   ├── hertzadapter/     # Hertz framework adapter → framework-agnostic routing abstraction
│   └── middleware/       # Global middleware (CORS, logging)
│
├── domains/              # Domain layer (each domain is self-contained)
│   ├── auth/             # Authentication (login, registration, OAuth)
│   ├── user/             # User (profiles, search)
│   ├── category/         # Category (circle categories)
│   ├── circle/           # Circle (creation, membership, permissions, management)
│   ├── post/             # Post (publishing, lists, details)
│   ├── comment/          # Comment (two-level flattened structure)
│   ├── like/             # Like (atomic ops + events)
│   ├── collect/          # Favorites
│   ├── history/          # Browsing history
│   ├── discover/         # Discover page aggregation
│   ├── trending/         # Trending posts
│   ├── recommend/        # Home feed recommendation
│   ├── notice/           # Message center (notifications + SSE unread stream)
│   ├── aiagent/          # AI agents (global + circle-scoped bots, reply triggers)
│   ├── storage/          # Storage (file uploads)
│   └── [domain]/
│       ├── application/      # Application services: use-case orchestration
│       ├── domain/           # Domain layer: models, repository interfaces, core business rules
│       ├── infrastructure/   # Infrastructure: repository impls, cache, search, events
│       └── interfaces/http/  # Interface layer: handlers, routes, DTOs
│
├── shared/               # Shared kernel (domain-agnostic)
│   ├── appctx/           # Context abstraction
│   ├── domain/           # Domain base classes (BaseModel)
│   ├── httputil/         # HTTP response utilities
│   └── routing/          # Framework-agnostic routing abstraction
│
├── conf/                 # Configuration loading (Nacos + local fallback)
└── logger/               # Logging initialization
```

### Key Design Decisions

1. **UUIDv7 primary keys** – First 48 bits are a timestamp; lexicographic order = chronological order, natively supporting keyset cursor pagination
2. **Framework-agnostic routing** – Domain code never depends on Hertz via the `routing.RouterGroup` abstraction
3. **Cross-domain Facades** – Domains call each other through interfaces without direct coupling; switching to microservices only requires swapping implementations
4. **Write-Behind caching** – Real-time Redis updates + async batch persistence via Redpanda for high concurrency
5. **Two-level flattened comments** – `root_id` marks hierarchy, avoiding recursive queries and enabling efficient pagination
6. **Dual-scope AI agents** – Global agents (platform admin) and circle agents (circle owner/admin) are fully isolated: cross-scope access returns 404, never leaking existence

## 🤖 AI Agents

Qubar ships with a two-tier AI agent system:

### Global Agents (`/agent/*`)

- Maintained by **platform super admins** (`users.role=1`) only
- Site-wide reply triggers: keyword matching on comments, manual triggering, and @mention on post publish

### Circle Agents (`/circle/agent/*`)

- Managed by **circle owners/admins**, up to **5 agents per circle**
- Field-level permission matrix:
  - List / detail / create / update operational fields (name, avatar, model, prompts, trigger config, rate limits, status): **owner + admin**
  - Credential fields (`api_protocol` / `base_url` / `api_key`) and delete: **owner only**
- Agents only reply to posts in their own circle; safe rate-limit defaults (30 replies/hour + 60s interval) are applied on creation to protect the owner's API key
- Mention scope guardrail: circle bots are filtered out of @mention pickers outside their circle (via `users.agent_circle_id` synced to ES through CDC)

### Reply Triggers

| Trigger | Description |
|---------|-------------|
| Keyword (`trigger_mode=2`) | New comments containing configured keywords trigger a bot reply (async, silent-fail) |
| @Mention | Mentioning an enabled bot when publishing a post triggers it directly, regardless of mode |
| Manual (`trigger_mode=3`) | `POST /agent/:id/reply/:postId` (admin) or `POST /circle/agent/:id/reply/:postId` (circle owner), synchronous |

### Security

- `api_key` is **encrypted at rest** and never echoed back – responses only contain `has_api_key` + `api_key_masked`
- Every bot is backed by a system user account (`role=2`) used as its commenting identity
- Permissions are checked against the live membership record on every request (no cache): role changes take effect immediately

Design docs: [docs/agent-reply-design.md](docs/agent-reply-design.md), [docs/circle-agent-manage-design.md](docs/circle-agent-manage-design.md), API reference: [docs/circle-agent-manage-api.md](docs/circle-agent-manage-api.md)

## 📁 Project Structure

```
qubar/
├── cmd/
│   ├── main.go           # Program entry point
│   └── apps/
│       └── server.go     # Service initialization & resource orchestration
│
├── configs/
│   ├── config.yaml       # Local config file (fallback when Nacos is unavailable)
│   └── bootstrap.yaml    # Nacos bootstrap config (address, namespace, group)
│
├── docs/
│   ├── pgsql-ddl/        # Database schemas (UUIDv7 PK version, split by domain)
│   ├── db.md             # DDL entry point (redirects to pgsql-ddl/)
│   ├── api/              # API docs
│   └── design/           # Design docs
│
├── pkg/
│   ├── composition/      # Composition layer (see architecture)
│   ├── domains/          # Business domains (see architecture)
│   ├── shared/           # Shared kernel
│   ├── conf/             # Configuration management
│   ├── logger/           # Logging setup
│   └── server/           # Legacy infrastructure (being migrated)
│       ├── auth/         # OAuth provider implementations
│       ├── storage/      # DB/Redis/ES/Redpanda/S3 initialization
│       └── utils/        # Utility functions
│
├── go.mod
└── go.sum
```

## 🚀 Quick Start

### Requirements

- Go 1.25.4+
- PostgreSQL 18 (with `uuidv7()` function enabled)
- Redis 7+
- Elasticsearch 8.x (optional; falls back to DB search without it)
- Redpanda (optional; stats stay in Redis only without it)

### 1. Clone the Project

```bash
git clone https://github.com/l0sgAi/qubar.git
cd qubar
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Set Up the Database

Create the database and schema:

```sql
CREATE DATABASE qubar;
CREATE SCHEMA IF NOT EXISTS domains;
```

For table schemas and seed data, see [docs/pgsql-ddl/](docs/pgsql-ddl/) (split by domain; entry point: [README.md](docs/pgsql-ddl/README.md))

### 4. Configure the App

#### Option 1: Local config (quick development)

Edit `configs/config.yaml` with your database, Redis and other connection info:

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

#### Option 2: Nacos config center (recommended for production)

Create `configs/bootstrap.yaml`:

```yaml
nacos:
  endpoint: "your-nacos-address:8848"
  namespace: "dev-namespace-uuid"
  group: "QUBAR_GROUP"
  data_id: "qubar-dev-conf"
  username: "nacos"
  password: "nacos"
```

### 5. Run the App

```bash
# Start with local config
go run cmd/main.go -c configs/config.yaml -b ""

# Start with Nacos config
go run cmd/main.go -c configs/config.yaml -b configs/bootstrap.yaml
```

The service starts at `http://localhost:8888`

## 🌐 API Endpoints

### Auth (no login required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/auth/google/login` | Google OAuth login redirect |
| `GET` / `POST` | `/auth/google/callback` | Google OAuth callback / one-time code exchange |
| `GET` | `/auth/github/login` | GitHub OAuth login |
| `GET` / `POST` | `/auth/github/callback` | GitHub OAuth callback / code exchange |
| `GET` | `/auth/azure/login` | Azure AD OAuth login |
| `GET` / `POST` | `/auth/azure/callback` | Azure AD OAuth callback / code exchange |
| `POST` | `/auth/register/send-code` | Send registration verification code |
| `POST` | `/auth/register/verify` | Verify the code |
| `POST` | `/auth/register/complete` | Complete registration |
| `POST` | `/auth/login` | Email/password login |
| `POST` | `/auth/password/send-code` | Send password-reset code |
| `POST` | `/auth/password/verify` | Verify password-reset code |
| `POST` | `/auth/password/reset` | Reset password |

### User (login required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/user/get` | Get current logged-in user |
| `PUT` | `/user/update` | Update profile |
| `GET` | `/user/search` | Search users |
| `GET` | `/user/detail/:id` | Get user detail |
| `POST` | `/auth/logout` | Revoke current token |

### Category (login required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/category/get` | Get all categories |

### Circle

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/circle/create` | Create a circle (login) |
| `GET` | `/circle/list` | Search/browse circles (public) |
| `GET` | `/circle/active` | Recently active circles (public) |
| `GET` | `/circle/random` | Random circles (public) |
| `GET` | `/circle/detail/:id` | Circle detail (public) |
| `GET` | `/circle/user` | Circles a user joined (public) |
| `GET` | `/circle/posts` | Posts in a circle (public) |
| `GET` | `/circle/my` | Circles I joined (login) |
| `POST` | `/circle/join` | Join a circle (login) |
| `POST` | `/circle/leave` | Leave a circle (login) |
| `GET` | `/circle/members` | Member list (admin+) |
| `GET` | `/circle/manage/list` | Circles I can manage (owner/admin) |
| `POST` | `/circle/manage/role` | Assign/revoke admin (owner) |
| `POST` | `/circle/manage/transfer` | Transfer ownership (owner) |
| `POST` | `/circle/manage/mute` / `unmute` | Mute / unmute member (admin+) |
| `POST` | `/circle/manage/ban` / `unban` | Ban / unban member (admin+) |
| `POST` | `/circle/manage/review` | Join-request review (admin+) |
| `PUT` | `/circle/update` | Edit circle profile (owner/admin, field-level) |

### Post

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/post/create` | Publish a post (login, supports @mentions) |
| `GET` | `/post/list` | Search posts (public) |
| `GET` | `/post/home` | Home recommendation feed (public) |
| `GET` | `/post/my` | My posts (login) |
| `GET` | `/post/user/:user_id` | Posts by a user (public) |
| `GET` | `/post/detail/:id` | Post detail (public) |

### Comment

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/comment/create` | Post a comment/reply (login) |
| `GET` | `/comment/list` | Top-level comments (public) |
| `GET` | `/comment/replies` | Replies within a thread (public) |
| `GET` | `/comment/detail/:id` | Single comment detail (public) |

### Like & Favorite (login required)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/like/toggle` | Like/unlike (posts & comments) |
| `POST` | `/collect/toggle` | Favorite/unfavorite a post |
| `GET` | `/collect/posts` | My favorite posts |

### Message Center (login required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/notice/list` | Notification list |
| `GET` | `/notice/unread-count` | Unread count |
| `POST` | `/notice/read` | Mark notifications as read |
| `POST` | `/notice/read-all` | Mark all as read |
| `GET` | `/notice/stream` | SSE stream for real-time unread count push |

### Discovery & History

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/discover/` | Discover page aggregation (login) |
| `GET` | `/trending/` | Trending posts (public) |
| `GET` | `/history/posts` | Browsing history (login) |

### AI Agents – Global (platform admin only)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/agent` | Create a global agent |
| `GET` | `/agent/list` | List global agents (offset pagination) |
| `GET` | `/agent/:id` | Agent detail |
| `PUT` | `/agent/:id` | Update agent (partial) |
| `DELETE` | `/agent/:id` | Soft-delete agent |
| `POST` | `/agent/:id/reply/:postId` | Manually trigger a reply |

### AI Agents – Circle (circle owner/admin)

| Method | Path | Description | Permission |
|--------|------|-------------|------------|
| `POST` | `/circle/agent` | Create a circle agent (≤5 per circle) | admin+ |
| `GET` | `/circle/agent/list` | List circle agents | admin+ |
| `GET` | `/circle/agent/:id` | Agent detail | admin+ |
| `PUT` | `/circle/agent/:id` | Update agent | operational fields: admin+; credential fields: owner only |
| `DELETE` | `/circle/agent/:id` | Soft-delete agent | owner only |
| `POST` | `/circle/agent/:id/reply/:postId` | Manually trigger a reply (post must be in the agent's circle) | owner only |

### File Upload (login required)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/upload/image` | Single image upload |
| `POST` | `/upload/post-images` | Multi-image upload for posts |
| `POST` | `/upload/video` | Video upload |
| `DELETE` | `/upload/delete` | Delete a file |
| `GET` | `/upload/presign` | Get a presigned upload URL |

### Request Header

Include this header for all endpoints requiring login:

```bash
satoken: your-token-here
```

## 🔐 Security

- ✅ CORS protection (configurable allowed origins)
- ✅ Sa-Token session management (3-day validity, 30-minute active timeout)
- ✅ RBAC role-based access control
- ✅ Email verification code registration
- ✅ Logical deletion for data protection
- ✅ S3 presigned URLs (no credential exposure)
- ✅ Agent API keys encrypted at rest, masked in all responses
- ✅ Dual-scope agent isolation (global ↔ circle, cross-scope returns 404)

## ⚡ Performance

1. **Multi-level Redis caching** – Layered caching for user profiles, circle info and statistics
2. **Lua atomic operations** – Likes and view counts via Lua scripts
3. **Write-Behind strategy** – Stats go to Redis first, then async batch persistence via Redpanda
4. **Covering index optimization** – Carefully designed PostgreSQL indexes avoid table lookups
5. **ES full-text search** – Hot searches via Elasticsearch, cold data via DB (posts reach ES through external CDC)
6. **JSONB fields** – Multimedia and extended info stored as PostgreSQL JSONB

## 📝 Development Guide

### Adding a New Domain

1. Create a domain directory under `pkg/domains/` following the `application/domain/infrastructure/interfaces` layering
2. Register dependency wiring and routes in `pkg/composition/`
3. For cross-domain calls, add a bridge implementation in `composition/facade_bridges.go`

### Adding a New OAuth Provider

1. Add a provider implementation in `pkg/server/auth/` (see `google.go`)
2. Register it in `provider.go`
3. Update the `auth` domain routes

### Configuration

Key configuration items:

```yaml
# CORS allowed origins
cors:
  allowed_origins:
    - "https://qubar.site"
    - "http://localhost:*"

# Sa-Token session config
sa_token:
  token_name: "satoken"
  timeout: 259200        # 3 days (seconds)
  active_timeout: 1800   # 30-minute activity check
  is_concurrent: true    # Allow concurrent logins

# File upload size limit (50MB configured in code)
# server.WithMaxRequestBodySize(50 << 20)
```

## 🧪 Testing

```bash
# Run unit tests
go test ./pkg/...

# Run tests for a specific package
go test ./pkg/composition/middleware/...
```

## 📄 License

[MIT License](LICENSE)

## 🤝 Contributing

Issues and Pull Requests are welcome!

## 📧 Contact

For questions or suggestions, please open an Issue or contact the maintainer.

---

**Note**: Before the first run, make sure all required parameters are configured correctly, especially database connections and OAuth credentials.
