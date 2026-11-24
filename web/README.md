# Qubar Frontend

**English** | [简体中文](README.zh-CN.md)

The web client for [Qubar](https://github.com/l0sgAi/qubar), an interest-based community similar to Baidu Tieba / Reddit.
Built with Vue 3 (JavaScript), Naive UI and Vite, with a Chinese / English interface.

Live site: <https://qubar.site>

## ✨ Features

- **Accounts** – Email/password registration with verification codes, password reset, and OAuth login
  (Google / GitHub / Azure AD) via a one-time code exchange. The same account can stay signed in on several
  devices at once.
- **Guest mode** – Feeds, posts, circles, user pages and search are readable without an account; actions such as
  liking or commenting prompt the visitor to log in.
- **Feeds and discovery** – Home feed, trending page, discover page, and full-text search across posts, circles and users.
- **Posts and comments** – Markdown editor with image upload, likes, favorites, and two-level comments with replies.
  Posts held for review are flagged to their author until a circle moderator approves them.
- **@mentions** – Mention picker in posts and comments; mentions are bound to user IDs and highlighted when rendered.
- **Interest circles** – Create, join and browse circles. Owners and admins get management pages for members
  (roles, mute, ban, join requests), circle profile editing, and a moderation queue for pending posts.
- **AI bots** – Circle owners and admins manage the circle's own bots (up to 5 per circle); platform admins manage
  site-wide bots.
- **Message center** – Notification list with mark-read / mark-all-read. The unread badge is pushed over SSE and
  falls back to 30-second polling when the stream is unavailable.
- **Profile** – Public user pages, profile editing with avatar cropping, my posts, my circles, favorites and history.
- **Mobile web view** – Screens up to 768px wide get a dedicated mobile layout with a top bar and bottom tab bar.
  Management-heavy pages show a "not supported on mobile" page instead (see [Mobile view](#-mobile-view)).
- **i18n** – Simplified Chinese and English, picked from the browser language and remembered locally.

## 🛠 Tech Stack

| Library | Purpose |
|---------|---------|
| [Vue 3](https://vuejs.org/) | UI framework (`<script setup>` SFCs) |
| [Vite](https://vite.dev/) | Dev server and build |
| [Vue Router](https://router.vuejs.org/) | Routing, auth guard, per-route mobile config |
| [Naive UI](https://www.naiveui.com/) | Component library (dark theme) |
| [vue-i18n](https://vue-i18n.intlify.dev/) | zh-CN / en-US translations |
| [axios](https://axios-http.com/) | HTTP client with a shared request wrapper |
| [md-editor-v3](https://github.com/imzbf/md-editor-v3) | Markdown editor |
| [cropperjs](https://github.com/fengyuanchen/cropperjs) | Image cropping |
| [Vitest](https://vitest.dev/) + [Vue Test Utils](https://test-utils.vuejs.org/) | Unit tests (jsdom) |

## 🚀 Quick Start

### Requirements

- **Node.js 20** (the version CI uses) and npm
- A running [Qubar backend](https://github.com/l0sgAi/qubar), local or remote

### Install and run

```bash
git clone https://github.com/l0sgAi/qubar-frontend.git
cd qubar-frontend
npm install
npm run dev
```

The dev server starts at <http://localhost:5173> and opens a browser tab.

| Command | What it does |
|---------|--------------|
| `npm run dev` | Start the Vite dev server (uses `.env.development`) |
| `npm run build` | Production build into `dist/` (uses `.env.production`) |
| `npm run preview` | Serve the production build locally |
| `npm test` | Run all unit tests once |
| `npm run test:watch` | Run tests in watch mode |
| `npm run test:coverage` | Run tests with coverage; fails below the thresholds |
| `npm run check` | Pre-commit self-check: coverage tests + production build |

### Environment variables

The backend address is **not** hard-coded. Vite reads it from `.env.*` at the repo root, and
[src/config/index.js](src/config/index.js) exposes it to the rest of the app as `API_BASE` / `APP_DOWNLOAD_URL`.

| Variable | Required | Purpose |
|----------|----------|---------|
| `VITE_API_BASE` | Yes | Base URL of the Qubar backend API, used for every backend call (axios wrapper, SSE notification stream, OAuth login) |
| `VITE_APP_DOWNLOAD_URL` | No | App download link shown on the mobile "not supported" page; the link is hidden when empty |

- `.env.development` is loaded by `npm run dev`; `.env.production` is baked into the bundle by `npm run build`.
- To point your local dev server somewhere else without touching the committed file, create
  `.env.development.local` (`*.local` files are git-ignored) and set `VITE_API_BASE` there.
- Only variables prefixed with `VITE_` reach the browser bundle, so never put secrets in these files.

## 📱 Mobile View

- The breakpoint and the master switch live in [src/constants/breakpoints.js](src/constants/breakpoints.js):
  `BP_MOBILE_MAX = 768` and `MOBILE_SHELL_ENABLED`. With the switch on, screens ≤ 768px render the mobile shell
  (`MobileTopBar` / `MobileTabBar` in `src/components/layout/mobile/`); turning it off falls back to the desktop layout.
  CSS media queries can't read JS constants, so `@media` rules repeat the same `768px` by hand.
- Each route can set `meta.mobile` (defaults and helpers in [src/router/mobileMeta.js](src/router/mobileMeta.js)):
  - `supported` – `false` renders `MobileUnsupported.vue` in place of the page on mobile, without changing the URL
  - `topBar` – `main` (logo + search) or `back` (back button + title)
  - `tabBar` – whether to show the bottom tab bar
- Pages currently unsupported on mobile: create post, circle members / edit / bots / moderation, and bot admin.
- Design and scope: [docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md).

## 📁 Project Structure

```
qubar-frontend/
├── src/
│   ├── api/            # One module per backend domain (auth, post, comment, circle, notice, agent, moderation, ...)
│   ├── assets/         # main.css: global styles and design tokens
│   ├── components/     # Reusable components, grouped by area
│   │   ├── layout/     #   App shell, header, side nav, sidebar; mobile/ holds the mobile top bar and tab bar
│   │   ├── common/     #   Shared building blocks (AppDropdown, SmartLink, image cropper, ...)
│   │   └── ...         #   auth/, feed/, post/, circle/, user/, agent/
│   ├── composables/    # Composition functions (SSE unread count, breakpoints, mentions, uploads, like/favorite toggles, ...)
│   ├── config/         # Runtime config read from VITE_* env vars
│   ├── constants/      # Shared constants (breakpoints, circle roles, moderation enums)
│   ├── locales/        # zh-CN / en-US messages
│   ├── router/         # Routes, auth guard, meta.mobile helpers
│   ├── utils/          # Request wrapper, auth/token storage, guest access, sanitizing, mention parsing, ...
│   ├── views/          # Route pages: auth/, feed/, post/, circle/, user/, admin/, plus Notifications and fallback pages
│   ├── App.vue
│   └── main.js
├── tests/              # Vitest specs, mirroring src/
├── public/             # Copied as-is: CNAME, favicon, standalone landing page (/landing)
├── docs/               # Design and integration notes (Chinese)
├── scripts/            # Build diagnostics script
├── .github/workflows/  # CI, deploy, Claude review
├── .env.development    # Dev env vars
├── .env.production     # Production env vars
├── vite.config.js
└── vitest.config.js    # Reuses vite.config.js; coverage thresholds
```

## 🧪 Testing and CI

```bash
npm test        # all unit tests
npm run check   # coverage tests + production build, same as CI
```

- Specs live in `tests/` with the same layout as `src/` and are named `*.spec.js`. Coverage is measured on
  `src/utils`, `src/composables` and `src/router`; thresholds are in [vitest.config.js](vitest.config.js).
- After editing `src/locales/`, run `npm test`: [tests/i18n.spec.js](tests/i18n.spec.js) compiles every message with
  the production compiler and checks that both languages have the same keys and placeholders.
- See [CONTRIBUTING.md](CONTRIBUTING.md#testing) for how to write tests.

Workflows in [.github/workflows](.github/workflows):

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| `ci.yml` | PRs to `main` (also called by `deploy.yml`) | Two checks: `unit` (tests + coverage thresholds) and `build` (production build) |
| `deploy.yml` | Push to `main` | Runs the same CI first, then builds and deploys to GitHub Pages |
| `claude-review.yml` | Non-draft PRs to `main` from this repo | Automatic Claude code review (inline comments + a summary comment) |
| `claude.yml` | `@claude` in an issue / PR comment or review | Runs Claude on request; only users with write access can trigger it |

## 🌐 Deployment

The site is deployed to GitHub Pages by `deploy.yml` on every push to `main`. The custom domain is set in
`public/CNAME`, and the build copies `index.html` to `404.html` so deep links work as an SPA.
Pages setup, branch protection and troubleshooting are covered in [DEPLOY.md](DEPLOY.md).

## 📚 Documentation

Docs in this repo are written in Chinese.

| Document | Contents |
|----------|----------|
| [DEPLOY.md](DEPLOY.md) | GitHub Pages deployment, CI checks and branch protection |
| [OAUTH_CALLBACK.md](OAUTH_CALLBACK.md) | OAuth login callback and one-time code exchange |
| [HANDOFF.md](HANDOFF.md) | Point-in-time handover notes from the mobile portal work (October 2026); not kept up to date |
| [docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md) | Mobile web portal plan: scope, breakpoints, `meta.mobile`, per-page checklist |
| [docs/mobile-circle-card-plan.md](docs/mobile-circle-card-plan.md) | Compact "joined circles" cards on mobile |
| [docs/testing-ci-implementation-plan.md](docs/testing-ci-implementation-plan.md) | Test setup, coverage thresholds and CI gates |
| [docs/like-collect-idempotent-integration-plan.md](docs/like-collect-idempotent-integration-plan.md) | Integrating the idempotent like / favorite APIs |
| [docs/mention-backend-requirements.md](docs/mention-backend-requirements.md) | Backend requirements for precise @mention binding |
| [docs/mention-user-ids-integration.md](docs/mention-user-ids-integration.md) | Persisting and returning `mention_user_ids` |
| [docs/notice-sse-backend-requirements.md](docs/notice-sse-backend-requirements.md) | Backend requirements for the SSE unread-count stream |
| [docs/comment-locate-backend-requirements.md](docs/comment-locate-backend-requirements.md) | Backend requirements for jumping from a notification to its comment |

Backend:

- [l0sgAi/qubar](https://github.com/l0sgAi/qubar) – the Go backend
- [Backend docs on `develop`](https://github.com/l0sgAi/qubar/tree/develop/docs) – the source of truth for APIs.
  Frontend integration docs are the `*-frontend-api.md` files there and the
  [`docs/api/`](https://github.com/l0sgAi/qubar/tree/develop/docs/api) directory.

## 🤝 Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, conventions,
testing and the PR workflow.

## 📄 License

[MIT License](LICENSE)
