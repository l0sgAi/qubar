# Contributing to Qubar

**English** | [简体中文](CONTRIBUTING.zh-CN.md)

Thanks for your interest in Qubar! Bug reports, ideas, docs fixes and code are all welcome.
This guide covers what you need to go from a fresh clone to a merged pull request.

## Before you start

- **Small fixes** (typos, docs, obvious bugs): just open a PR.
- **Anything larger** (new endpoints, schema changes, new domains): open an issue first so we can agree on the
  approach before you write code. Comment on an existing issue if you'd like to take it.
- Issue templates live in [.github/ISSUE_TEMPLATE](.github/ISSUE_TEMPLATE). They are written in Chinese, but issues
  in English are perfectly fine. New issues start as `agent:triage` until a maintainer reviews them.
- Found a security problem? Please don't open a public issue — contact the maintainer privately first.

## Local setup

You need **Go 1.26+** and **Docker**. Nothing else has to be installed locally.

```bash
git clone https://github.com/<your-fork>/qubar.git
cd qubar
make dev-up     # PG 18 / Redis / Elasticsearch (IK) / Redpanda + run all migrations
make dev-run    # app on http://localhost:8888 (configs/config.dev.yaml); check /healthz and /readyz
```

Dev stack ports: PG `25432`, Redis `26379`, ES `29200`, Redpanda `29092`. `make dev-psql q="select ..."` queries the dev database.

Known limitations of the dev stack:

- There is no CDC, so business indices in Elasticsearch are **not** synced from PostgreSQL.
- S3, OAuth and email use placeholder config; endpoints depending on them won't work locally.
  If your change touches these paths, say what you couldn't verify in the PR.

## Project layout and conventions

Qubar is a Go modular monolith organized by domain (`pkg/domains/<domain>/{domain,application,infrastructure,interfaces/http}`).
The [README](README.md#-architecture) has the overview; the full rules are in
[.agents/skills/qubar-skill/SKILL.md](.agents/skills/qubar-skill/SKILL.md). The ones that most often trip people up:

- `domain/` is pure Go — no GORM / Redis / Elasticsearch / Hertz imports, no imports of sibling domains.
- Cross-domain calls go through a Facade/Port interface plus a bridge in `pkg/composition/`, never a direct import.
- Handlers use `appctx.AppContext` and the `pkg/shared/httputil` response helpers, not `c.JSON(...)`.
- Primary keys are UUIDv7 with no `BeforeCreate` hook: call `sharedomain.NewID()` on **every** insert.
- No GORM `AutoMigrate`. The schema only comes from forward-only migrations in [migrations/](migrations/)
  (`make migrate-new name=...`, rules in [migrations/README.md](migrations/README.md)).
- New config keys must be added to both `configs/config.example.yaml` and `configs/config.dev.yaml`.
- Never commit real secrets. `configs/config.yaml` is git-ignored; `config.dev.yaml` only holds dev-stack credentials.
- Code comments and internal docs are in Chinese to match the existing code. English is fine in issues and PR discussion.

## Testing

```bash
make verify            # fmt + vet + lint + unit tests + migration checks and replay — same as CI
make test-integration  # integration tests against the dev stack (needs make dev-up)
```

- `make verify` must pass before you open a PR. Fix formatting with `gofmt -w cmd pkg`; lint must report zero issues.
- If you change `infrastructure/`, SQL / migrations, Redis Lua scripts, or Redpanda producers/consumers,
  `make test-integration` must pass too. A `SKIP` in the output means the stack wasn't reachable, which doesn't count as passing.
- New infrastructure code needs an integration test: a `*_integration_test.go` file next to the code, starting with
  `//go:build integration`, using the fixtures in [pkg/shared/testkit](pkg/shared/testkit). Use random IDs
  (`sharedomain.NewID()`), clean up what you insert, and make the test safe to run in parallel and repeatedly.

## Branches, commits and pull requests

- Branch from **`develop`** and target **`develop`** with your PR. `main` is the production release branch and only
  maintainers merge into it.
- Branch names: `feature/<slug>-<yyyymmdd>`, or `fix/<issue-number>-<slug>` for issue fixes.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/) in English, e.g.
  `feat(post): add draft status` or `fix(auth): handle expired state (#48)`.
- One PR solves one issue (`Closes #N` in the description). Try to keep business-code changes under ~500 lines
  (tests excluded); split larger work into several issues.
- Fill in the [PR template](.github/pull_request_template.md): what changed, how you verified it
  (`make verify` result, endpoints you called), and what you couldn't verify.

### Review and merge

- Every PR runs CI (`go` / `db` / `image` / `iac` / `gitleaks` / `integration`) and gets an automated Claude review
  in addition to a maintainer's review. Checks that need repository secrets may not run on PRs from forks;
  a maintainer will take care of those.
- PRs touching high-risk paths are labeled `risk:high` automatically and always need explicit maintainer approval:
  `migrations/**`, `deploy/**`, `.github/**`, `Dockerfile`, `pkg/domains/auth/**`, `pkg/server/auth/**`,
  `pkg/util/netguard/**`, `pkg/util/crypto/**`, `pkg/conf/**`. Please call these changes out in the PR description.
- If `develop` moves ahead of your branch, merge `origin/develop` into it (or use "Update branch" on GitHub).

## A note on AI agents

Much of Qubar's development is done with AI coding agents. [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md) are
instructions for those agents, and labels such as `agent:ready` / `agent:local` route issues to them. You don't need
any of that as a human contributor. Follow this guide; if you do use an agent yourself, pointing it at `CLAUDE.md`
works well.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
