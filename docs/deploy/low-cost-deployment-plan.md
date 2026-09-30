# Qubar low-cost deployment plan (alternative to the AWS plan)

> Status: proposal + first implementation (`deploy/`, `.github/workflows/`).
> Revised 2026-09-30 for the real audience: **Singapore, Japan/Korea and mainland China**.
> Prices are from web-search snippets dated Sep 2026. Vendor pricing pages were not reachable from the
> sandbox this was written in — **re-check every price before you buy anything** (Hetzner repriced three
> times in 2026). Latency numbers marked *(src)* come from a cited article; the rest are typical figures
> for those routes, **not measured by me** — measure before committing (§3.2).
> Builds on the AWS plan (`docs/deploy/deployment-architecture.md` on branch
> `claude/deployment-iac-architecture-kkgqzv`). Where this doc disagrees with it, this doc is right (§1).

---

## 0. TL;DR

- **Do this:** one VPS running the whole stack in Docker Compose, **in Hong Kong** (the only single
  location that serves Singapore, Japan/Korea *and* mainland China acceptably), **Cloudflare** in front
  (DNS, TLS, CDN, Tunnel → *zero open inbound ports*), **Cloudflare R2** for media and backups,
  **GitHub Actions + GHCR** for CI/CD. If mainland China turns out to be a small share of users, **Tokyo** is
  the cheaper fallback (§3.1).
- **Cost:** ≈ **$40/mo** for an 8 GB Hong Kong box (Tencent Cloud Lighthouse 4 vCPU/8 GB, $36 per search
  snippet), ≈ **$12–15/mo** for an 8 GB Tokyo box (Contabo). The AWS "Launch" profile was ≈ $200/mo
  (priced in Singapore; Hong Kong/Tokyo are usually pricier). That is ~80 % / ~93 % cheaper.
- **Not Hetzner (for this audience):** Hetzner's cheap ARM boxes are Europe-only → ~170–260 ms extra per API
  call to your users, and poor/unstable routes into mainland China. It stays available as an optional
  Terraform root (`deploy/terraform/hetzner`) for anyone who wants it.
- **Mainland China is the hard part, and it is mostly not a hosting problem** (§3.3): Cloudflare's free plan
  is mediocre from inside China, Google login is blocked there, and serving a community app to mainland
  users has legal/regulatory implications. Plan for a measured "Plan B edge".
- **What you give up vs AWS:** high availability. One box = one failure domain. Recovery is "rebuild +
  restore last night's dump" (RPO ≤ 24 h, RTO ≈ 1 h). §7 says how to tighten that.
- **Merge to `main` → deploy:** CI → multi-arch image → GHCR → SSH via Cloudflare Access → `deploy.sh` →
  `/readyz` gate → auto-rollback → public smoke test. §5.
- **Needs you:** a domain on Cloudflare, a VPS account, ~10 GitHub secrets/vars (§5.2), and to reconcile the
  Debezium/ES connector templates with your working local setup (§8).

## 1. Corrections to the earlier AWS doc (found while building this)

| Earlier claim | Reality | Consequence |
|---|---|---|
| "Elasticsearch is soft — warn, DB fallback" | Home feed, circle posts, my/user posts, post/user/circle search all read ES (`pkg/domains/*/infrastructure/*_searcher_es.go` → `elasticsearch.Search*`). No DB fallback. | ES **and** the Debezium CDC pipeline are effectively required. A "lean, no-ES" profile is not viable without code changes. Minimum box is 8 GB. |
| "Sa-Token Redis URL suspect broken with a password" | **Confirmed.** `redis://pw@host` parses as *username=pw, password empty* in go-redis. | A Redis with `requirepass` would break login/session. **Fixed** in this branch (`pkg/server/auth/sa_token_init.go`, tested). |
| (not noticed) | App connects to Redpanda/ES **only at startup** and never retries. If it starts before them it silently runs degraded until restarted. | Compose `depends_on: service_healthy` + a post-reboot restart unit (`qubar-postboot.service`). Real fix = lazy reconnect in code (§9). |
| (earlier README of this branch) "`manage_server=false` skips the Hetzner token" | **Wrong** — Terraform still configured the Hetzner provider and demanded a token. | Terraform is now two independent roots: `deploy/terraform` (Cloudflare) and optional `deploy/terraform/hetzner`. |

## 2. Why AWS is expensive here

Rough AWS "Launch" bill (from the earlier doc): RDS t4g.medium ≈ $72, two EC2 ≈ $93, CloudFront + WAF ≈ $15,
NAT/fck-nat + IPv4 ≈ $8, EBS ≈ $12, misc ≈ $10 → **≈ $200**. Almost none of it is the workload — the whole
launch-scale workload fits in ~6 GB of RAM. You are paying for isolation (separate DB host, NAT, managed
edge). Isolation is worth it later; it is not worth 5–15× at launch.

## 3. Where to host (audience: Singapore, Japan/Korea, mainland China)

### 3.1 Options

Latency = typical round trip from the user's country to the origin; API calls are dynamic, so every call pays
it in full (Cloudflare only shortens the TLS handshake and serves cached media).

| Origin | ≈ $/mo (8 GB) | SG | JP | KR | Mainland CN | Verdict |
|---|---|---|---|---|---|---|
| **Hong Kong** (Tencent Cloud Lighthouse 4c/8GB/180GB, 35 Mbps, 5.1 TB/mo) | **36** *(src)* | ~35 | ~50 | ~50 | **28–55 ms, <0.1 % loss on CN2-GIA-class lines** *(src, premium HK lines)*; other HK lines vary | **Recommended** — best balance for all three groups. Tencent's own HK line quality into China is *unverified* → test first. Bandwidth is capped (35 Mbps) and metered (5.1 TB). |
| Hong Kong, boutique CN2-GIA VPS providers | not found | same | same | same | best | Cheap-looking, tiny vendors, trust/support risk. Not for the primary DB. |
| **Tokyo** (Contabo Cloud VPS, "8 GB ≈ €4.50–€8" + location fee — verify plan/setup fee) | **~10–13** | ~70 | ~5–15 | ~30–40 | varies (~40–90+); source says Contabo's lines are "not particularly friendly to Chinese users" | **Cheap fallback** if CN is a minor share. Best JP/KR. Shared/oversold CPU. |
| Tokyo/Seoul (Vultr High Frequency 3 vCPU/8 GB) | 72 *(src)* | ~70 | ~10 | ~5–35 | varies | Reliable but 2× Tencent HK. |
| Singapore (Contabo SG ≈ $5.28 + $3.10 fee) | ~9–12 | ~1–5 | ~70 | ~90 | **80–160 ms peak, 3–8 % loss** *(src)* | Only if you were SEA-only. Worst option for CN. |
| AWS EC2 in HK/Tokyo/SG + self-hosted PG + S3 | ≥ ~70–100 (t4g.large ≈ $49 in the cheapest US region; Asian regions cost more) | — | — | — | — | Most reliable; ~2× Tencent HK. |
| AWS Launch profile (RDS + 2 EC2 + CloudFront/WAF) | ~200+ | | | | | Baseline. Overkill for launch. |
| Hetzner CAX21 (EU only) | ~14 (€10.49) | ~170–200 | ~220–260 | ~230–260 | poor/unstable | **Rejected for this audience.** Hetzner Singapore = CPX only, ≥ $38 (April price, repriced since). |
| Oracle Always Free A1 | 0 | — | — | — | — | Halved 15 Jun 2026 to 2 OCPU/12 GB (PAYG accounts reportedly still 4/24 — sources conflict); idle-reclaim rule; "out of capacity"; termination reports. **Staging only.** |
| Northflank managed | ~85–125 (estimate) | | | | | 4–8× the VPS; ES/Redpanda/Connect aren't ready-made addons. See chat notes; not pursued. |
| Managed free tiers / PaaS / k3s | — | | | | | Don't fit (ES+IK, CDC, Kafka Connect) or cost more. |
| Today's Mac + `cloudflared` tunnel | 0 | | | | | Legit **stop-gap** for a public HTTPS URL this week. Not production. |

**Pick by CN share of users:**
- **CN is a meaningful share (say ≥ 15–20 %) or China-first:** Hong Kong.
- **CN is small / best-effort; JP/KR/SG dominate:** Tokyo (Contabo) — cheaper, better JP/KR, weaker SG (~70 ms).
- **Never** Europe, and Singapore only if you drop China.

The compose stack, CI, backups and the Cloudflare Terraform are identical for all of these; only the server
is different (§8).

### 3.2 Measure before you commit (30 minutes, free)

1. Rent the candidate for a month (or use a trial), run `qubar` (or just `nginx` returning 200) on it.
2. From each target country test `curl -o /dev/null -s -w "connect=%{time_connect} ttfb=%{time_starttransfer}\n" https://<origin>/`.
   Singapore/Japan/Korea: any cloud shell or a friend. **Mainland China: use a China ping/HTTP probe service
   (e.g. itdog.cn) or a mainland phone on mobile data, at evening peak (20:00–23:00), on all three carriers.**
3. Do it both **through Cloudflare** and **direct to the origin IP** — the gap is exactly what §3.3 is about.
4. Decide with numbers: p95 latency and packet loss, not averages.

### 3.3 Mainland China specifics (read this before promising China support)

1. **Cloudflare free plan is mediocre from inside China.** Mainland users are served from overseas edges with
   high latency and packet loss (sources: Cloudflare docs, community reports). The **China Network** (edges
   inside China) is Enterprise-only and requires an **ICP license** → not available to you. Effect: SG/JP/KR
   users get the full Cloudflare benefit; CN users may do **better going straight to a Hong Kong origin**
   than through Cloudflare.
2. **"Plan B edge" (not built — build only if §3.2 measurements say so):** give CN clients a second hostname,
   `api-cn.<domain>`, as a **DNS-only** record straight to the HK origin, TLS terminated on the box (add a
   `caddy` service to compose, publish 443, allow it in ufw/provider firewall). Costs: exposed origin IP, no
   Cloudflare WAF/DDoS for that hostname (use rate limits in Caddy + provider DDoS protection), CORS/OAuth
   redirect config for two hostnames, and the client must pick the hostname (locale/probe). Keep
   `api.<domain>` on Cloudflare for everyone else.
3. **Media has the same issue:** R2 via Cloudflare is slow from China. Options later: an HK object-storage
   bucket (Tencent COS speaks S3; the app already supports a custom `s3.endpoint`) fronted directly, or
   accept it. Mainland CDNs require an ICP.
4. **Sign-in:** Google is blocked in mainland China, GitHub is unreliable there; Microsoft generally works.
   Also verify transactional email actually lands in QQ/163 mailboxes (Mailtrap deliverability into China is
   unproven). For real China traffic you will probably need phone/SMS or WeChat login — a product decision
   that affects success more than latency does.
5. **Compliance:** an ICP filing is needed only to host **inside** mainland China. Serving mainland users from
   Hong Kong needs none, but access can be throttled/blocked, and a user-generated-content community aimed
   at mainland users carries content-regulation obligations. This is not legal advice — talk to local
   counsel before making China a target market.
6. Servers in Hong Kong/Tokyo/Singapore are **outside** the GFW: GitHub, Docker Hub/GHCR, Google/OpenAI/
   Anthropic APIs and OAuth callbacks all work from the box (a server *inside* mainland China would not).

**Provider risk:** Hetzner raised prices in April *and* June 2026 and other providers move too. The design
keeps the provider layer thin: `host/bootstrap.sh`, compose, CI and the Cloudflare layer work on any
Ubuntu/Debian VPS; swapping the server is a half-day job.

## 4. Chosen architecture

```
 Users (SG / JP / KR / CN) ── HTTPS ──► Cloudflare (free plan: DNS, TLS, CDN, basic WAF/rate-limit)
                      │  api.<domain> ───────────────┐   media.<domain> ─► R2 (public, cached)
                      │  ssh.<domain> (Access: service token only)
                      ▼
              cloudflared (container, host network) ── outbound-only tunnel ──┐
 ┌──────────────────── one VPS (Ubuntu 24.04, Hong Kong — or Tokyo) ──────────┴──────┐
 │  firewall: NO inbound rules.  sshd: key-only, user `deploy`.                       │
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

The Tunnel connects out to the nearest Cloudflare edge (Hong Kong for a HK box), so the Cloudflare → origin
hop is short for SG/JP/KR users.

### 4.1 Memory budget (8 GB box)

| Service | Cap | Notes |
|---|---|---|
| qubar | 768 MB | `GOMEMLIMIT=600MiB`. Argon2id uses 64 MB per concurrent hash (config `security.password_hash.memory`, min 19456 KiB) — rate-limit `/auth/*` at Cloudflare. |
| PostgreSQL | 1.5 GB | `shared_buffers=512MB`, `max_connections=150` (app pool is 100). |
| Redis | 640 MB | `maxmemory 512mb`, `volatile-lru` (evicts only TTL keys: sessions/caches, not counters). |
| Redpanda | 1.4 GB | `--smp=1 --memory=1G`. |
| Elasticsearch | 2 GB | heap 1 GB. First thing to grow. |
| Kafka Connect | 1 GB | heap ≤ 640 MB. |
| cloudflared | 128 MB | |
| **Sum of caps** | **≈ 7.4 GB** | caps are ceilings, typical use ≈ 5–6 GB; **2 GB swap** file is the safety net. |

Tight but workable for launch. Move to 16 GB when swap is used regularly or ES heap pressure shows; double
`shared_buffers` and ES heap then.

## 5. CI/CD

### 5.1 Flow

```
PR opened/updated ─► ci.yml   go build/vet/test -race · docker build + trivy · shellcheck ·
                              compose config · config-key drift · actionlint ·
                              terraform fmt/validate (cloudflare root + hetzner root)
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
  `terraform fmt / validate (…)` (both), `gitleaks`. `deploy.yml` also re-runs CI, so a direct push can't
  bypass it.

### 5.2 GitHub configuration you must add

Environment **`production`** (Settings → Environments; optionally add *required reviewers* for a manual gate):

| Kind | Name | Value |
|---|---|---|
| secret | `PROD_ENV_FILE` | whole `.env` (template: `deploy/compose/.env.example`) |
| secret | `DEPLOY_SSH_KEY` | private half of the key whose public half you gave `bootstrap.sh` (`DEPLOY_SSH_PUBKEY`) |
| secret | `CF_ACCESS_CLIENT_ID` / `CF_ACCESS_CLIENT_SECRET` | `terraform output access_client_id` / `access_client_secret` |
| variable | `SSH_HOSTNAME` | `ssh.<domain>` (`terraform output ssh_hostname`) |
| variable | `SSH_HOST_KEY` | server's ed25519 host key line for `known_hosts` (recommended; otherwise trust-on-first-use with a warning) |
| variable | `PUBLIC_API_URL` | `https://api.<domain>` |

Why one `PROD_ENV_FILE` secret instead of ~25: rotation is one edit; compose and the backup script read the
same file. Trade-off: it's one big blob — treat "who can edit environment secrets" as prod-admin.

## 6. Security

| Area | Design |
|---|---|
| Inbound | **None.** Provider firewall with zero inbound rules + `ufw default deny`; sshd not reachable from the internet. Public traffic and CI reach the box only through the Cloudflare Tunnel (outbound connection from the server). Optional break-glass: `ALLOW_SSH_FROM` in `bootstrap.sh`. *(Plan B edge in §3.3 deliberately opens 443 for one hostname.)* |
| Docker vs ufw | Docker-published ports bypass ufw. Every published port is bound to `127.0.0.1`; only `cloudflared` (host network) reaches them. Never write `"8888:8888"`. |
| CI access | GitHub → Cloudflare Access **service token** → tunnel → `deploy` user, key-only. The `deploy` user is in the `docker` group (≈ root on the box): the SSH key and Access token are prod-admin credentials. Tokens expire yearly (Terraform renews within 30 days of expiry — then update the GitHub secret). |
| Secrets | Not in git (gitleaks in CI). Live in the GitHub Environment secret → `.env` 0600 on the box. No cloud secret manager (cost); rotate by editing the secret and redeploying. |
| Datastores | PG/Redis/ES/Redpanda/Connect are only on the compose network (Connect's REST API on 127.0.0.1). Redis has a password (needs the fix in §1). ES has **no auth** because the app has no ES auth option — network isolation is the control; SSRF (#48, already merged) is what protects it. |
| DB roles | `qubar_owner` (DDL), `qubar_web_app` (DML only), `debezium` (replication + SELECT). Tested: web_app has no TRUNCATE, debezium has no INSERT. |
| Container hardening | App: distroless nonroot, read-only rootfs, `cap_drop: ALL`, `no-new-privileges`. Trivy gate in CI (fixable CRITICAL/HIGH). |
| Edge | Cloudflare TLS, DDoS absorb, cache. **Manual (not in Terraform v1):** WAF managed rules + a rate-limit rule on `/auth/*` (free plan allows a small number — verify). Argon2 (64 MB/hash) makes login floods a memory-DoS risk; this rule matters. |
| Limits to know | Free plan: ~100 s proxy idle timeout (SSE heartbeat is 25 s — fine), 100 MB request body (app limit 50 MB — fine). |
| Supply chain | Actions pinned to major tags, not SHAs (Dependabot config included; consider SHA pinning). `cloudflared` is downloaded from GitHub Releases `latest` in CI — pin a version when you can. |
| Provider account | New-account verification/suspension friction is a recurring complaint at several budget providers (Hetzner especially). Keep DB backups off-provider (R2 — already the design). |

## 7. Backup & disaster recovery

| Data | Mechanism | RPO / RTO |
|---|---|---|
| PostgreSQL (only source of truth) | `backup.sh` daily 19:30 UTC → R2 `qubar-backup/pg/`, 14-day retention, optional `age` encryption (public key on box, private key offline), size sanity check | **RPO ≤ 24 h** / RTO ≈ 1 h |
| Redis | AOF everysec on disk. Loss = users re-login + unflushed counters (stats are batched anyway). | ~1 s on crash; box loss = sessions gone |
| Elasticsearch | Rebuildable: re-register the Debezium source with a new slot → snapshot re-indexes. | RTO 1–2 h |
| Redpanda | Transient events. | – |
| Media | R2 (no versioning by default — enable if you want undo). | – |
| Whole box | Provider snapshots (Tencent Lighthouse / Hetzner backups) — cheap second safety net; includes Redis/ES state. | RPO = snapshot age |
| Infra | Cloudflare layer: `terraform apply` recreates tunnel, DNS, Access, buckets (state in R2). Server: `bootstrap.sh` + first deploy. | ~30 min + restore |

**Tighten later (recommended before you have paying users):** WAL archiving with pgBackRest or wal-g to R2
→ RPO of minutes and PITR. Not in v1 because it deserves its own restore drill. **Do a restore drill**
(`scripts/restore.sh` into a scratch box) before launch — an untested backup is a hope, not a backup.

## 8. What is built (this branch) and how far it was verified

Layout: `deploy/{compose,config,scripts,host}`, `deploy/terraform` (**Cloudflare root**) and
`deploy/terraform/hetzner` (**optional Hetzner root**, own state key), `.github/workflows/{ci,deploy}.yml`,
`.github/dependabot.yml`. Operator runbook: `deploy/README.md`.

**Server provisioning is provider-agnostic:** for Tencent Lighthouse / Contabo / AWS / anything else, create an
Ubuntu 24.04 (or Debian 12) box ≥ 8 GB and run `DEPLOY_SSH_PUBKEY="ssh-ed25519 …" bash deploy/host/bootstrap.sh`
as root (`sudo -i` first if the image logs you in as `ubuntu`). Everything after that (Terraform for
Cloudflare, CI, compose, backups) is the same. Note the bootstrap **disables root SSH and password login and
blocks inbound ports** — make sure the tunnel/Access path works, and know how to reach the provider's web
console, before you log out.

| Piece | Verified how |
|---|---|
| Redis-password fix | Unit test; end-to-end: app booted against Redis with `requirepass`, `/readyz` → redis ok |
| `compose.prod.yml` | `docker compose config` ✔; **ran** postgres + redis + redpanda + qubar locally |
| Postgres 18 init (roles, schema, default privileges, publication) | Ran; privileges and publication queried and correct |
| Redpanda flags | Found and fixed a real bug (`--set=k=v` is rejected; needs two args); healthy, topic create ✔, app consumers start ✔ |
| Shell scripts | `shellcheck` ✔. **`deploy.sh` rollback path, `backup.sh`, `restore.sh`, `bootstrap.sh` were not executed** (no target host / R2 in sandbox) |
| Workflows | `actionlint` ✔. Not run on GitHub yet |
| Terraform — Hetzner root | `fmt` ✔, `validate` ✔ (provider built locally) |
| Terraform — Cloudflare root | `fmt` ✔; resource arguments checked by hand against the v4.52.5 docs; **`validate` not possible in the sandbox** (registry blocked, provider not buildable via Go proxy). First CI run on a PR will tell. No `.terraform.lock.hcl` committed for the same reason — run `terraform init` once and commit it. |
| ES + IK image, Kafka Connect image, connector JSON | **Not built or run** (`docker.elastic.co` blocked in the sandbox). The repo has no record of your local CDC setup, so `ES_VERSION`, `DEBEZIUM_TAG`, `ES_SINK_VERSION` are required inputs with no guessed defaults, and `connectors/*.json` are templates to reconcile with `curl localhost:8083/connectors/<n>/config` on your Mac. See `deploy/compose/connect/connectors/README.md`. |
| Tencent Lighthouse / Tokyo specifics | **Nothing here was run on those providers.** Assumed: Ubuntu 24.04 image, Docker apt repo reachable, outbound HTTPS to GHCR/Cloudflare works (all true outside mainland China). |

## 9. Follow-ups (ranked)

**Before you pick the region**
1. Run the §3.2 latency test (SG/JP/KR + China at evening peak, through Cloudflare and direct) — it decides HK vs Tokyo and whether Plan B edge is needed.
2. Decide the China strategy explicitly (target market vs best-effort), including login methods and legal review (§3.3).

**Before real users**
3. Reconcile CDC templates with the working local pipeline; run a full first-load rehearsal (restore dump → register connectors → check ES doc counts).
4. Restore drill from R2 backup. Then WAL archiving (RPO minutes).
5. Cloudflare dashboard: WAF managed rules, `/auth/*` rate limit, bind `media.<domain>` to the R2 bucket.
6. Branch protection on `main` (§5.1).
7. Lower Argon2 memory (`security.password_hash.memory` 32768, `threads: 2`) and benchmark hash time on the chosen box.

**Code (small, high value)**
8. Lazy reconnect for Redpanda/ES at runtime (today: startup-only → silent degradation; worked around by ordering + post-boot restart).
9. Real migrations (`migrations/*.sql` + goose/golang-migrate run by CI as `qubar_owner`) — DDL currently lives in `docs/pgsql-ddl/*.md` (and those files contain `DROP TABLE`; `restore.sh` refuses to run on a populated DB for that reason).
10. Client: batch/parallelize API calls per screen — with 35–70 ms (HK/Tokyo) or 100+ ms (China) round trips, sequential calls are the main UX cost.
11. ES auth option, Redis/Kafka TLS options (only matters once services leave the box).

**When one box stops being enough**
12. Split data services to a second VPS (private network), then app replicas — needs SSE hub via Redis pub/sub + syncer leader lock (see AWS doc §6 P2).
13. Managed PG (any provider with PG 18 + logical replication) if you'd rather buy HA than build it.

## 10. Decisions I need from you

1. **Hong Kong (~$40) or Tokyo (~$13)?** Rule of thumb in §3.1: China share ≥ ~15–20 % → Hong Kong.
2. **China strategy:** target market (then: Plan B edge, login methods, legal review, probably a mainland-friendly media path) or best-effort.
3. **Domain** on Cloudflare (needed for Tunnel/Access/R2 custom domain). Which?
4. **ES version, Debezium tag, ES sink connector + version** from your Mac (`curl localhost:9200`, `docker ps`, `curl localhost:8083/connector-plugins`).
5. Is the repo private? (Private → GitHub's 2,000 free Actions min/mo applies; a CI run is ~5–8 min, so ~250 runs/mo. Public → unlimited.)

## Sources

- Hosting location and China routing: [Server.HK — Singapore vs Hong Kong VPS](https://server.hk/blog/singapore-vps-vs-hong-kong-vps-best-for-southeast-asia-in-2026/), [Server.HK — HK vs SG for APAC apps](https://server.hk/blog/hong-kong-vps-vs-singapore-vps-which-for-your-asia-pacific-app-in-2026/), [Jtti — Japan vs Hong Kong for mainland access](https://m.jtti.cc/supports/3162.html), [VPS.DO — USA/SG/JP/HK comparison](https://vps.do/usa-singapore-japan-hongkong-vps-server-location/)
- Cloudflare in China: [Cloudflare China Network docs](https://developers.cloudflare.com/china-network/), [Cloudflare China — what works (Chinaready)](https://chinaready.co/insights/cloudflare-and-china-what-works/), [Xiaozha: Cloudflare optimization for mainland China](https://xiaozha.org/en/article/cloudflare-ip-optimization/)
- Prices: [Tencent Cloud Lighthouse](https://www.tencentcloud.com/products/lighthouse), [Contabo Tokyo launch](https://contabo.com/blog/tokyo-datacenter/), [Cybernews — Singapore VPS](https://cybernews.com/vps/best-singapore-vps-services/), [Vultr High Frequency](https://www.vultr.com/products/high-frequency-compute/), [comparevps — Vultr](https://www.comparevps.com/hosting/vultr), [Server.HK VPS](https://server.hk/vps/), [Jtti — HK CN2 GIA](https://www.jtti.cc/supports/1160.html)
- Hetzner 2026 repricing: [Northflank](https://northflank.com/blog/hetzner-cloud-server-price-increases), [Better Stack review](https://betterstack.com/community/guides/web-servers/hetzner-cloud-review/), [Hetzner price adjustment page](https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/), [reviewcost](https://reviewcost.com/reviews/hetzner-cloud-review-2026-excellent-infrastructure-value-but-price-hikes-and-support-gaps-narrow-the-advantage)
- Oracle free-tier cut: [InfoQ](https://www.infoq.com/news/2026/07/oracle-cloud-free-tier-limits/), [Linuxiac](https://linuxiac.com/oracle-quietly-cuts-free-tier-ampere-a1-resources-in-half/), [TerminalBytes](https://terminalbytes.com/oracle-cloud-free-tier-changes-2026/)
- Cloudflare R2: [pricing summary](https://mecanik.dev/en/posts/cloudflare-r2-pricing-explained-real-costs-vs-s3-and-backblaze/), [Cloudflare R2](https://www.cloudflare.com/products/r2/); Tunnel/WebSocket limits: [Cloudflare docs](https://developers.cloudflare.com/network/websockets/); Access SSH from CI: [cloudflared-ssh actions](https://github.com/NX1X/cloudflare-tunnel-ssh-action)
- GitHub Actions pricing: [GitHub](https://github.com/resources/insights/2026-pricing-changes-for-github-actions)
- IK plugin install URL: [infinilabs/analysis-ik](https://github.com/infinilabs/analysis-ik)
