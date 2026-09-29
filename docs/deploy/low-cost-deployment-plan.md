# Qubar low-cost deployment plan (alternative to the AWS plan)

> Status: proposal + first implementation (`deploy/`, `.github/workflows/`). Date: 2026-09-29.
> Prices are from web-search snippets dated Sep 2026. Vendor pricing pages were not reachable from the
> sandbox this was written in — **re-check every price before you buy anything** (Hetzner repriced three
> times in 2026).
> Builds on the AWS plan (`docs/deploy/deployment-architecture.md` on branch
> `claude/deployment-iac-architecture-kkgqzv`). Where this doc disagrees with it, this doc is right (§1).

---

## 0. TL;DR

- **Do this:** one VPS running the whole stack in Docker Compose, **Cloudflare** in front (DNS, TLS, CDN,
  Tunnel → *zero open inbound ports*), **Cloudflare R2** for media and backups, **GitHub Actions + GHCR**
  for CI/CD. Terraform for the Hetzner server + Cloudflare bits.
- **Cost:** ≈ **$14/mo** (Hetzner CAX21, 8 GB) or ≈ **$25/mo** (CAX31, 16 GB, recommended once traffic is real).
  The AWS "Launch" profile was ≈ $200/mo. That is ~90 % cheaper.
- **Why it is cheap:** the stack has six stateful services (PG, Redis, ES, Redpanda, Kafka Connect + the
  app). On AWS every one of them becomes a billable managed piece or a separate EC2. On a VPS they share
  one machine, egress is free, and Cloudflare replaces CloudFront + WAF + ALB + NAT for $0.
- **What you give up:** high availability. One box = one failure domain. Recovery is "rebuild + restore
  last night's dump" (RPO ≤ 24 h, RTO ≈ 1 h). §7 says how to tighten that.
- **Merge to `main` → deploy:** CI (build/vet/test, docker build + scan, config checks, terraform
  validate) → build multi-arch image → push to GHCR → SSH through Cloudflare Access → `deploy.sh` →
  wait for `/readyz` → auto-rollback on failure → public smoke test. §5.
- **Needs you:** a domain on Cloudflare, a Hetzner (or other VPS) account, ~10 GitHub secrets/vars (§5.2),
  and to reconcile the two Debezium/ES connector templates with your working local setup (§8).

## 1. Corrections to the earlier AWS doc (found while building this)

| Earlier claim | Reality | Consequence |
|---|---|---|
| "Elasticsearch is soft — warn, DB fallback" | Home feed, circle posts, my/user posts, post/user/circle search all read ES (`pkg/domains/*/infrastructure/*_searcher_es.go` → `elasticsearch.Search*`). No DB fallback. | ES **and** the Debezium CDC pipeline are effectively required. A "lean, no-ES" profile is not viable without code changes. Minimum box is 8 GB. |
| "Sa-Token Redis URL suspect broken with a password" | **Confirmed.** `redis://pw@host` parses as *username=pw, password empty* in go-redis. | A Redis with `requirepass` would break login/session. **Fixed** in this branch (`pkg/server/auth/sa_token_init.go`, tested). |
| (not noticed) | App connects to Redpanda/ES **only at startup** and never retries. If it starts before them it silently runs degraded until restarted. | Compose `depends_on: service_healthy` + a post-reboot restart unit (`qubar-postboot.service`). Real fix = lazy reconnect in code (§9). |

## 2. Why AWS is expensive here

Rough AWS "Launch" bill (from the earlier doc): RDS t4g.medium ≈ $72, two EC2 ≈ $93, CloudFront + WAF ≈ $15,
NAT/fck-nat + IPv4 ≈ $8, EBS ≈ $12, misc ≈ $10 → **≈ $200**. Almost none of it is the workload — the whole
launch-scale workload fits in ~6 GB of RAM. You are paying for isolation (separate DB host, NAT, managed
edge). Isolation is worth it later; it is not worth 15× at launch.

## 3. Options investigated

| # | Option | ≈ $/mo (8 GB class) | Verdict |
|---|---|---|---|
| A | AWS Profile B (RDS + 2 EC2 + CloudFront/WAF) | ~200 | Baseline. Overkill for launch. |
| B | AWS single EC2 + S3, self-host PG | ~70–90 | Cheaper, but you keep AWS egress/EBS/IPv4 fees and get nothing Hetzner doesn't give you. |
| **C** | **Hetzner Cloud CAX21/CAX31 (ARM, EU) + Cloudflare + R2** | **~14 / ~25** | **Recommended default.** Best price/perf, first-class Terraform provider, ARM matches Apple-silicon dev. EU-only → higher latency to Asia (see below). |
| D | Contabo Cloud VPS, Singapore | ~9–12 | Cheapest 8 GB in Asia (≈ $5.28 + $3.10 SG fee per snippet; plans/prices inconsistent across sources — verify, check setup fee). Shared/oversold CPU, weaker support/API. Use if users are mostly in Asia. Same compose/CI; only `server.tf` changes (`manage_server=false`). |
| E | Hetzner Singapore (CPX only) | ≥ 38 (CPX32 was $38.49 in April, then repriced again) | Right region, wrong price. Rejected. |
| F | Oracle Cloud Always Free (A1) | 0 | Halved on 15 Jun 2026 to **2 OCPU / 12 GB** (PAYG accounts reportedly still 4/24 — sources conflict). Idle-reclaim rule (A1 <20 % CPU/net/mem 95th pct over 7 days), "out of capacity", account-termination reports. **Staging only**, never the primary DB. |
| G | Managed free tiers (Neon/Supabase PG, Upstash Redis, Elastic Cloud, Confluent…) | 0 → 100+ | Don't fit: ES needs the IK plugin + no auth support in code, CDC needs logical replication + Kafka Connect. The pieces that matter have no free tier. |
| H | PaaS (Fly/Railway/Render) | est. 60+ | Per-service billing × JVM-heavy services. Not cheaper than C. |
| I | k3s / Kubernetes on the same box | +0 $ / +lots of ops | No benefit at 1 node. |
| J | **Today's Mac + `cloudflared` tunnel** | 0 | Legit **stop-gap** to get a public HTTPS URL this week. Not production: sleep, power, home ISP, no backups. |

**Latency caveat (choose deliberately):** Hetzner CAX/CX are **EU-only**. If your users are in East/Southeast
Asia, API round trips are ~150–250 ms slower than a Singapore origin. Media is unaffected (R2 + Cloudflare
CDN is global). Options: (1) accept it for launch; (2) Contabo SG (D); (3) later add an Asian read path. The
repo config uses `TimeZone=Asia/Shanghai` and the earlier plan used `ap-southeast-1`, so this is probably a real
question for you — see §10.

**Provider risk:** Hetzner raised prices in April *and* June 2026 (CAX21 €7.99 → €10.49; CPX up to +176 %).
So the design keeps the provider layer thin: only `deploy/terraform/server.tf` is Hetzner-specific;
`host/bootstrap.sh`, compose, CI and the Cloudflare layer work on any Ubuntu/Debian VPS.

## 4. Chosen architecture

```
 Users ── HTTPS ──► Cloudflare (free plan: DNS, TLS, CDN, basic WAF/rate-limit)
                      │  api.<domain> ───────────────┐   media.<domain> ─► R2 (public, cached)
                      │  ssh.<domain> (Access: service token only)
                      ▼
              cloudflared (container, host network) ── outbound-only tunnel ──┐
 ┌────────────────────────── one VPS (Ubuntu 24.04, Hetzner CAX21/31) ────────┴──────┐
 │  ufw/cloud firewall: NO inbound rules.  sshd: key-only, user `deploy`.             │
 │                                                                                    │
 │  127.0.0.1:8888 ─► qubar (distroless, read-only, no caps, 768 MB)                  │
 │        │  docker network (nothing else published)                                  │
 │        ├─► postgres:18   (wal_level=logical, roles owner/web_app/debezium, 1.5 GB) │
 │        ├─► redis:7       (requirepass, AOF everysec, maxmemory 512 MB)             │
 │        ├─► redpanda      (1 core, 1 GB, topics auto-created, 3-day retention)      │
 │        ├─► elasticsearch 8 + IK (single node, heap 1 GB, 2 GB cap)                 │
 │        └─► kafka-connect (Debezium PG source → ES sink, 1 GB)                      │
 │  systemd: qubar-backup.timer (daily pg_dump → R2 [+age]), qubar-postboot.service   │
 └────────────────────────────────────────────────────────────────────────────────────┘
 CI: GitHub Actions ─► GHCR image ─► ssh (via Cloudflare Access) ─► deploy.sh
```

### 4.1 Memory budget (8 GB box)

| Service | Cap | Notes |
|---|---|---|
| qubar | 768 MB | `GOMEMLIMIT=600MiB`. Argon2id uses 64 MB per concurrent hash — rate-limit `/auth/*` at Cloudflare. |
| PostgreSQL | 1.5 GB | `shared_buffers=512MB`, `max_connections=150` (app pool is 100). |
| Redis | 640 MB | `maxmemory 512mb`, `volatile-lru` (evicts only TTL keys: sessions/caches, not counters). |
| Redpanda | 1.4 GB | `--smp=1 --memory=1G`. |
| Elasticsearch | 2 GB | heap 1 GB. First thing to grow. |
| Kafka Connect | 1 GB | heap ≤ 640 MB. |
| cloudflared | 128 MB | |
| **Sum of caps** | **≈ 7.4 GB** | caps are ceilings, typical use ≈ 5–6 GB; **2 GB swap** file is the safety net. |

Tight but workable for launch. Move to CAX31 (16 GB, ≈ €21) when swap is used regularly or ES heap
pressure shows; double `shared_buffers` and ES heap then.

## 5. CI/CD

### 5.1 Flow

```
PR opened/updated ─► ci.yml   go build/vet/test -race · docker build + trivy · shellcheck ·
                              compose config · config-key drift · actionlint · terraform fmt/validate
                              (+ secret-scan.yml / gitleaks)
merge to main     ─► deploy.yml
                       ci (same workflow, reused)  ── fail ─► stop, nothing deployed
                       build: buildx linux/amd64+arm64 → ghcr.io/<owner>/qubar:sha-<7> (+:latest)
                       deploy (environment: production, serialized):
                          tar deploy/{compose,config,scripts} ─ssh─► /opt/qubar
                          PROD_ENV_FILE ─ssh stdin─► /opt/qubar/.env (0600)
                          docker login ghcr.io with the job's short-lived GITHUB_TOKEN
                          deploy.sh sha-xxxxxxx: pull → up -d → poll /readyz (≤120 s)
                             ok   → record tag, register connectors, prune images
                             fail → roll back to previous tag, exit 1 (job red)
                          smoke: curl https://api.<domain>/healthz
manual            ─► Actions → deploy → Run workflow, `tag=sha-…` = redeploy/rollback an old image
```

- Docs-only merges (`docs/**`, `**/*.md`) don't deploy; use *Run workflow* to force.
- Deploys are serialized (`concurrency: deploy-production`, no cancel).
- Deploy = stop old, start new → **~5–20 s of downtime** at 1 replica. Consumer groups rebalance, SSE
  clients reconnect (`retry_ms: 5000`). Acceptable at launch; zero-downtime needs >1 replica, which needs
  the code work in §9 (SSE hub, syncer leader lock).
- "CI has to pass before merge" is a repo setting, not code: **Settings → Branches → protect `main`**,
  require a PR and these checks: `go build / vet / test`, `docker build + scan`, `deploy files (…)`,
  `terraform fmt / validate`, `gitleaks`. `deploy.yml` also re-runs CI, so a direct push can't bypass it.

### 5.2 GitHub configuration you must add

Environment **`production`** (Settings → Environments; optionally add *required reviewers* for a manual gate):

| Kind | Name | Value |
|---|---|---|
| secret | `PROD_ENV_FILE` | whole `.env` (template: `deploy/compose/.env.example`) |
| secret | `DEPLOY_SSH_KEY` | private half of the key whose public half you gave Terraform (`deploy_ssh_public_key`) |
| secret | `CF_ACCESS_CLIENT_ID` / `CF_ACCESS_CLIENT_SECRET` | `terraform output access_client_id` / `access_client_secret` |
| variable | `SSH_HOSTNAME` | `ssh.<domain>` (`terraform output ssh_hostname`) |
| variable | `SSH_HOST_KEY` | server's ed25519 host key line for `known_hosts` (recommended; otherwise trust-on-first-use with a warning) |
| variable | `PUBLIC_API_URL` | `https://api.<domain>` |

Why one `PROD_ENV_FILE` secret instead of ~25: rotation is one edit; compose and the backup script read the
same file. Trade-off: it's one big blob — treat "who can edit environment secrets" as prod-admin.

## 6. Security

| Area | Design |
|---|---|
| Inbound | **None.** Hetzner firewall has zero inbound rules; `ufw default deny`; sshd not reachable from the internet. Public traffic and CI reach the box only through the Cloudflare Tunnel (outbound connection from the server). Optional break-glass: `break_glass_ssh_cidrs`. |
| Docker vs ufw | Docker-published ports bypass ufw. Every published port is bound to `127.0.0.1`; only `cloudflared` (host network) reaches them. Never write `"8888:8888"`. |
| CI access | GitHub → Cloudflare Access **service token** → tunnel → `deploy` user, key-only. The `deploy` user is in the `docker` group (≈ root on the box): the SSH key and Access token are prod-admin credentials. Tokens expire yearly (Terraform renews within 30 days of expiry — then update the GitHub secret). |
| Secrets | Not in git (gitleaks in CI). Live in the GitHub Environment secret → `.env` 0600 on the box. No AWS-style secret manager (cost); rotate by editing the secret and redeploying. |
| Datastores | PG/Redis/ES/Redpanda/Connect are only on the compose network (Connect's REST API on 127.0.0.1). Redis has a password (needs the fix in §1). ES has **no auth** because the app has no ES auth option — network isolation is the control; SSRF (#48, already merged) is what protects it. |
| DB roles | `qubar_owner` (DDL), `qubar_web_app` (DML only), `debezium` (replication + SELECT). Tested: web_app has no TRUNCATE, debezium has no INSERT. |
| Container hardening | App: distroless nonroot, read-only rootfs, `cap_drop: ALL`, `no-new-privileges`. Trivy gate in CI (fixable CRITICAL/HIGH). |
| Edge | Cloudflare TLS, DDoS absorb, cache. **Manual (not in Terraform v1):** WAF managed rules + a rate-limit rule on `/auth/*` (free plan allows a small number — verify). Argon2 (64 MB/hash) makes login floods a memory-DoS risk; this rule matters. |
| Limits to know | Free plan: ~100 s proxy idle timeout (SSE heartbeat is 25 s — fine), 100 MB request body (app limit 50 MB — fine). |
| Supply chain | Actions pinned to major tags, not SHAs (Dependabot config included; consider SHA pinning). `cloudflared` is downloaded from GitHub Releases `latest` in CI — pin a version when you can. |

## 7. Backup & disaster recovery

| Data | Mechanism | RPO / RTO |
|---|---|---|
| PostgreSQL (only source of truth) | `backup.sh` daily 19:30 UTC → R2 `qubar-backup/pg/`, 14-day retention, optional `age` encryption (public key on box, private key offline), size sanity check | **RPO ≤ 24 h** / RTO ≈ 1 h |
| Redis | AOF everysec on disk. Loss = users re-login + unflushed counters (stats are batched anyway). | ~1 s on crash; box loss = sessions gone |
| Elasticsearch | Rebuildable: re-register the Debezium source with a new slot → snapshot re-indexes. | RTO 1–2 h |
| Redpanda | Transient events. | – |
| Media | R2 (11 nines durability, no versioning by default — enable if you want undo). | – |
| Infra | `terraform apply` recreates server, tunnel, DNS, Access. State lives in R2. | ~30 min + restore |

**Tighten later (recommended before you have paying users):** WAL archiving with pgBackRest or wal-g to R2
→ RPO of minutes and PITR. Not in v1 because it deserves its own restore drill. **Do a restore drill**
(`scripts/restore.sh` into a scratch box) before launch — an untested backup is a hope, not a backup.

## 8. What is built (this branch) and how far it was verified

Layout: `deploy/{compose,config,scripts,host,terraform}`, `.github/workflows/{ci,deploy}.yml`,
`.github/dependabot.yml`. Operator runbook: `deploy/README.md`.

| Piece | Verified how |
|---|---|
| Redis-password fix | Unit test; end-to-end: app booted against Redis with `requirepass`, `/readyz` → redis ok |
| `compose.prod.yml` | `docker compose config` ✔; **ran** postgres + redis + redpanda + qubar locally |
| Postgres 18 init (roles, schema, default privileges, publication) | Ran; privileges and publication queried and correct |
| Redpanda flags | Found and fixed a real bug (`--set=k=v` is rejected; needs two args); healthy, topic create ✔, app consumers start ✔ |
| Shell scripts | `shellcheck` ✔. **`deploy.sh` rollback path, `backup.sh`, `restore.sh`, `bootstrap.sh` were not executed** (no target host / R2 in sandbox) |
| Workflows | `actionlint` ✔. Not run on GitHub yet |
| Terraform — Hetzner + random | `fmt` ✔, `validate` ✔ (provider built locally) |
| Terraform — Cloudflare | `fmt` ✔; resource arguments checked by hand against the v4.52.5 docs; **`validate` not possible in the sandbox** (registry blocked, provider not buildable via Go proxy). First CI run on a PR will tell. No `.terraform.lock.hcl` committed for the same reason — run `terraform init` once and commit it. |
| ES + IK image, Kafka Connect image, connector JSON | **Not built or run** (`docker.elastic.co` blocked in the sandbox). The repo has no record of your local CDC setup, so `ES_VERSION`, `DEBEZIUM_TAG`, `ES_SINK_VERSION` are required inputs with no guessed defaults, and `connectors/*.json` are templates to reconcile with `curl localhost:8083/connectors/<n>/config` on your Mac. See `deploy/compose/connect/connectors/README.md`. |

## 9. Follow-ups (ranked)

**Before real users**
1. Reconcile CDC templates with the working local pipeline; run a full first-load rehearsal (restore dump → register connectors → check ES doc counts).
2. Restore drill from R2 backup. Then WAL archiving (RPO minutes).
3. Cloudflare dashboard: WAF managed rules, `/auth/*` rate limit, bind `media.<domain>` to the R2 bucket.
4. Branch protection on `main` (§5.1).

**Code (small, high value)**
5. Lazy reconnect for Redpanda/ES at runtime (today: startup-only → silent degradation; we work around it with ordering + a post-boot restart).
6. Real migrations (`migrations/*.sql` + goose/golang-migrate run by CI as `qubar_owner`) — DDL currently lives in `docs/pgsql-ddl/*.md` (and those files contain `DROP TABLE`; `restore.sh` refuses to run on a populated DB for that reason).
7. ES auth option, Redis/Kafka TLS options (only matters once services leave the box).

**When one box stops being enough**
8. Split data services to a second VPS (private network), then app replicas — needs SSE hub via Redis pub/sub + syncer leader lock (see AWS doc §6 P2).
9. Managed PG (any provider with PG 18 + logical replication) if you'd rather buy HA than build it.

## 10. Decisions I need from you

1. **Where are your users?** Mostly Asia → Contabo SG (D) or accept EU latency (C). Mostly EU/US → C.
2. **Domain** on Cloudflare (needed for Tunnel/Access/R2 custom domain). Which?
3. **ES version, Debezium tag, ES sink connector + version** from your Mac (`curl localhost:9200`, `docker ps`, `curl localhost:8083/connector-plugins`).
4. 8 GB (~$14) to start, or 16 GB (~$25) from day one?
5. Is the repo private? (Private → GitHub's 2,000 free Actions min/mo applies; a CI run is ~5–8 min, so ~250 runs/mo. Public → unlimited.)

## Sources

- Hetzner 2026 repricing: [Northflank](https://northflank.com/blog/hetzner-cloud-server-price-increases), [Better Stack review](https://betterstack.com/community/guides/web-servers/hetzner-cloud-review/), [Hetzner price adjustment page](https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/), [bitdoze](https://www.bitdoze.com/hetzner-cloud-cost-optimized-plans/), [byteiota](https://byteiota.com/hetzner-june-2026-price-shock/)
- Oracle free-tier cut: [InfoQ](https://www.infoq.com/news/2026/07/oracle-cloud-free-tier-limits/), [Linuxiac](https://linuxiac.com/oracle-quietly-cuts-free-tier-ampere-a1-resources-in-half/), [TerminalBytes](https://terminalbytes.com/oracle-cloud-free-tier-changes-2026/)
- Contabo: [Cybernews Singapore VPS](https://cybernews.com/vps/best-singapore-vps-services/), [Contabo pricing guide](https://cybernews.com/best-web-hosting/contabo-review/pricing/)
- Cloudflare R2: [pricing summary](https://mecanik.dev/en/posts/cloudflare-r2-pricing-explained-real-costs-vs-s3-and-backblaze/), [Cloudflare R2](https://www.cloudflare.com/products/r2/); Tunnel/WebSocket limits: [Cloudflare docs](https://developers.cloudflare.com/network/websockets/); Access SSH from CI: [cloudflared-ssh actions](https://github.com/NX1X/cloudflare-tunnel-ssh-action)
- GitHub Actions pricing: [GitHub](https://github.com/resources/insights/2026-pricing-changes-for-github-actions)
- IK plugin install URL: [infinilabs/analysis-ik](https://github.com/infinilabs/analysis-ik)
