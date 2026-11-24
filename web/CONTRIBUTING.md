# Contributing to Qubar Frontend

**English** | [简体中文](CONTRIBUTING.zh-CN.md)

Thanks for your interest in Qubar! Bug reports, ideas, docs fixes and code are all welcome.
This guide covers what you need to go from a fresh clone to a merged pull request.

## Before you start

- **Small fixes** (typos, docs, obvious bugs): just open a PR.
- **Anything larger** (new pages, new API integrations, layout or mobile changes): open an issue first so we can
  agree on the approach before you write code. Comment on an existing issue if you'd like to take it.
- Issue titles follow the Conventional Commit style, e.g. `feat(notice): show review results` or
  `docs: add a contributing guide`. A useful issue body has these parts:
  - **Background** – what's wrong or missing today, and why it matters
  - **Backend API** – the endpoints involved, linking the backend docs (see [API source of truth](#api-source-of-truth))
  - **Acceptance criteria** – a checklist that defines "done"
  - **Out of scope** – what this issue deliberately doesn't cover
  - **Files involved** – the files you expect to touch
- Existing issues are mostly written in Chinese, but issues in English are perfectly fine.
- Found a security problem? Please don't open a public issue — contact the maintainer privately first.

## Local setup

You need **Node.js 20** (the version CI uses) and npm.

```bash
git clone https://github.com/<your-fork>/qubar-frontend.git
cd qubar-frontend
npm install
npm run dev     # http://localhost:5173
```

On Node 25 and later, Node's built-in `localStorage` shadows jsdom's and every test fails with
`Cannot read properties of undefined (reading 'clear')`. Use Node 20, or run the tests with
`NODE_OPTIONS=--no-webstorage`.

The app needs a backend. `VITE_API_BASE` in `.env.development` decides which one `npm run dev` talks to:

- **Local backend** – run [l0sgAi/qubar](https://github.com/l0sgAi/qubar) yourself (its README has a Docker dev stack)
  and point `VITE_API_BASE` at it.
- **Production backend** – use the same value as `.env.production`. This works for reading, but you'll be acting on
  real data, so be careful with anything that writes.

Put personal overrides in `.env.development.local` (git-ignored) rather than editing the committed file.
See the [README](README.md#environment-variables) for every variable.

## Project layout and conventions

The [README](README.md#-project-structure) has the directory overview. Day-to-day rules:

- **Components** – Vue 3 single-file components with `<script setup>`, plain JavaScript (no TypeScript).
  Use Naive UI for UI primitives, and reach for the shared components in `src/components/common/` before writing
  your own: `AppDropdown` for dropdown menus, `SmartLink` for in-app links (real `<a href>` with SPA navigation), etc.
- **Styles** – Read the frontend-constraints skill before touching styles, Naive UI overrides, animations or locale
  messages: [.claude/skills/frontend-constraints/SKILL.md](.claude/skills/frontend-constraints/SKILL.md) (an
  identical copy lives in `.agents/skills/`). In short: reuse the design tokens in `src/assets/main.css` instead of
  hard-coded colors, and style teleported Naive UI parts (dropdowns, modals, popovers) globally — `scoped` styles
  don't reach them.
- **i18n** – Every user-facing string goes through vue-i18n. Add each new key to **both** `src/locales/zh-CN.js` and
  `src/locales/en-US.js`. `@`, `{` and `}` are reserved in messages; write a literal `@` as `{'@'}`.
- **API calls** – Wrap every endpoint in the matching module under `src/api/` (one module per backend domain), using
  the shared `request` from `src/utils/request.js`, with a JSDoc comment for its parameters and return value.
  Don't call axios directly from components, and don't hard-code the backend address — it comes from
  `API_BASE` in `src/config/index.js`.
- **Mobile** – Screens ≤ 768px use the mobile shell. A new route that doesn't work on mobile must set
  `meta: { mobile: { supported: false } }`; otherwise choose its `topBar` / `tabBar` (see
  [src/router/mobileMeta.js](src/router/mobileMeta.js)). Use `useBreakpoint()` for logic and a `768px` media query
  for styles, and check the page at a phone width. The full rules are in
  [docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md).
- **Comments and docs** – Code comments and internal docs are in Chinese to match the existing code. English is fine
  in issues and PR discussion.
- Never commit secrets. Anything in a `VITE_*` variable ends up in the public bundle.

### API source of truth

Backend behavior is defined by the docs in the backend repo's `develop` branch:
[l0sgAi/qubar/docs](https://github.com/l0sgAi/qubar/tree/develop/docs) (the `*-frontend-api.md` files and
[`docs/api/`](https://github.com/l0sgAi/qubar/tree/develop/docs/api)). If the docs and the backend's actual
responses disagree, raise it on the backend repo instead of working around it silently.

## Testing

```bash
npm test               # all unit tests
npm run test:watch     # re-run on change
npm run test:coverage  # with coverage report (coverage/index.html)
npm run check          # coverage tests + production build — same as CI
```

- `npm run check` must pass before you open a PR.
- Tests live in `tests/`, mirroring `src/` (e.g. `src/utils/share.js` → `tests/utils/share.spec.js`), and are named
  `*.spec.js`. Import source files with the `@/` alias.
- Tests run in jsdom. [tests/setup.js](tests/setup.js) clears `localStorage` / `sessionStorage`, restores real timers
  and undoes `vi.spyOn` / `vi.stubGlobal` after every test.
- Use `vi.useFakeTimers()` for anything time-based (debounce, retries, polling); never wait on real time.
- HTTP: when testing `src/utils/request.js` itself, attach `axios-mock-adapter` to the real instance; when testing
  anything else, `vi.mock('@/api/xxx')`.
- If you touch `src/locales/`, run `npm test`. [tests/i18n.spec.js](tests/i18n.spec.js) compiles every message with
  the production compiler and checks that both languages have the same keys and placeholders. vue-i18n swallows
  these compile errors in dev mode, so this test is the only early warning.
- Coverage thresholds are set in [vitest.config.js](vitest.config.js) and **only go up**. If your tests raise
  coverage, raise the thresholds to match; never lower them to make CI pass.

## Branches, commits and pull requests

- Branch from **`main`** and target **`main`** with your PR. Every merge to `main` deploys to production.
- Branch names: `<type>/<topic>-<YYYYMMDD>`, e.g. `feature/multi-device-login-20261022`,
  `fix/sse-conn-evicted-20261022`, `ci/claude-review-20261022`.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/) in English, e.g.
  `feat(circle): add pending-post moderation queue page` or `fix(notice): close SSE on conn-evicted`.
- One PR solves one issue (`Closes #N` in the description). Say what changed, how you verified it (`npm run check`,
  pages you clicked through, desktop and mobile widths), and what you couldn't verify.

### Review and merge

- Every PR to `main` runs two required checks from `ci.yml`: `unit` (tests + coverage thresholds) and `build`
  (production build).
- Non-draft PRs from branches in this repo also get an automatic Claude review (`claude-review.yml`), which posts
  inline comments and a summary. PRs from forks skip it because they can't access the repository secrets.
- Anyone with write access can ask Claude for more by commenting `@claude ...` on an issue or PR (`claude.yml`).
- A maintainer reviews and merges. If `main` moves ahead of your branch, merge `origin/main` into it (or use
  "Update branch" on GitHub).

## A note on AI agents

Much of this repo is developed with AI coding agents. Their changes go through exactly the same process: an issue,
a PR, the `unit` / `build` checks, and a human review before merging.

Agent-facing config lives in the repo:

- [.claude/skills/frontend-constraints](.claude/skills/frontend-constraints) and
  [.agents/skills/frontend-constraints](.agents/skills/frontend-constraints) – the same frontend rules, packaged as a
  skill for Claude Code and for other agents
- `.claude/launch.json` – how an agent starts the dev server for previews
- [.github/workflows/claude-review.yml](.github/workflows/claude-review.yml) – the review prompt used on PRs

You don't need any of that as a human contributor; the skill is still a good read for the styling and i18n rules.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
