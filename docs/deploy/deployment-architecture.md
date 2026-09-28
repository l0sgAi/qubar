# Qubar Deployment Architecture (IaC)

> Caveman mode. Short words. Big rocks first.
> Status: proposal. Date: 2026-09. Region: `ap-southeast-1` (S3 already there).
> Prices = ballpark USD/month, on-demand list price, from memory. Check AWS Pricing Calculator before commit.

---

## 0. TL;DR

- Now: Go binary on Mac. PG + Redis from Homebrew. ES / Redpanda / Debezium / Nacos in Docker. Hand-made. Not repeatable.
- Target: **Terraform (AWS infra) + Docker Compose (runtime) + GitHub Actions (build/deploy)**. One arm64 image. Mac M-series = Graviton arm64 = same image everywhere.
- Pick **Profile B "Launch"** (~$200/mo, ~$150 w/ 1-yr Savings Plan):
  - RDS PostgreSQL 18 (managed backups/PITR; PG = only source of truth).
  - 1× EC2 `app` (Qubar, 1 replica).
  - 1× EC2 `data` (ES+IK, Redpanda, Kafka Connect/Debezium, Redis w/ AOF).
  - CloudFront (TLS, WAF) → VPC origin → private subnets. No public IP on app/data. No SSH.
- Drop Nacos in cloud. Config from SSM Parameter Store. Saves 1–2 GB RAM + a JVM to babysit.
- **Must fix in code before prod** (§6 P0): SSRF via AI agent `base_url`, secrets committed in `configs/config.yaml`, no health endpoint, no env override for config, stats lost on restart.
- App stays **1 replica** until SSE hub + syncers made cluster-safe (§6 P2). Fine for launch. Scale vertical first.

---

## 1. What code need (investigation)

### 1.1 Process shape

One binary `cmd/main.go` → `apps.Run()`. Does everything in one process:

| Thing | Where | Deploy impact |
|---|---|---|
| HTTP API (Hertz, `:8888`, body 50 MB) | `pkg/server/router/router.go` | behind CDN/LB. Upload up to 50 MB → edge must allow. |
| SSE `/notice/stream` | `pkg/domains/notice`, hub in-process | long-lived conns. Heartbeat 25 s. Proxy must not buffer. **Hub is per-process** → multi-replica = users miss pushes. |
| 8 Redpanda consumers + aggregators | `pkg/server/storage/redpanda/*_consumer.go` | consumer groups → safe multi-replica. But see 1.4 data loss. |
| 4 ticker "syncers" (circle hot, trending, discover, item-CF) | `pkg/server/storage/redpanda/*_syncer.go` | **no distributed lock** → N replicas = N× work. Item-CF heavy (24 h). |
| AI agent async replies (goroutines) | `pkg/domains/aiagent/application/reply_service.go` | outbound HTTP to user-supplied `base_url` (see §5 SSRF). |
| Argon2id hashing (64 MB, 4 threads / hash) | `pkg/util/password` | RAM spike per login/register. 20 parallel logins ≈ 1.3 GB. Size RAM + rate-limit `/auth/*`. |

Logs → stdout only (zap). Good for containers. `log.director` unused.

### 1.2 Dependencies

| Dep | Required? | Startup behaviour | Code limits (matter for managed svc) |
|---|---|---|---|
| PostgreSQL **18** (`uuidv7()`) | **hard** | `os.Exit(1)` if down | DSN from config; `sslmode` via `pgsql.config` string (ok). Runtime role `qubar_web_app` DML only; DDL by owner. **No migration tool** – DDL lives in `docs/pgsql-ddl/*.md`. |
| Redis 7+ | **hard** | `Fatal` | Holds Sa-Token sessions + write-behind counters + Lua scripts. **No TLS, no ACL username.** Sa-Token URL built as `redis://<pw>@host` unescaped → suspect broken when password set / has special chars. Test. |
| S3 (+CloudFront domain) | **hard** | `Fatal` | **Static keys only** (`NewStaticCredentialsProvider`). No IAM role path. |
| Elasticsearch 8.x + **IK plugin** | soft | warn, DB fallback | `go-elasticsearch/v8` → **not OpenSearch** (product check). **No auth option** (URL only). Qubar creates template `pg_domains_template` + `circle` index; post/users/comment docs come from **Debezium CDC**. 1 shard, 0 replica. |
| Redpanda (Kafka API) | soft | warn, stats only in Redis | **No SASL/TLS**. `AllowAutoTopicCreation: true`. |
| Debezium (Kafka Connect) PG→Redpanda→ES | external | – | Not in repo. Index names `pg.domains.<table>` ⇒ `topic.prefix=pg`. Needs PG `wal_level=logical` + replication slot. |
| Nacos 3 | optional | fallback to `-c` file | `-b ""` skips. |
| Mailtrap API, OAuth (Google/GitHub/Azure), LLM APIs | soft | – | outbound HTTPS egress needed. |

### 1.3 Config

- Viper, YAML. `-c configs/config.yaml`, `-b configs/bootstrap.yaml` (Nacos).
- **No env var override** (`AutomaticEnv` not used). Only env read: `APP_ENV` (Nacos ns select).
- Hot reload (fsnotify / Nacos) only touches runtime fields; DB/Redis/MQ clients need restart.
- ⚠️ `configs/config.yaml` **committed with real-looking secrets**: `pgsql.password: 1q2w3e4r5t`, `security.data_key`. Also `server.mode: debug`, `pgsql.log_mode: debug` (logs SQL), CORS allows `http://localhost:*`.

### 1.4 Reliability gaps found

- **Stats loss on restart.** Consumers use `ReadMessage` + auto-commit every 1 s, but aggregators flush every 2–27 min (`flush_interval: 27` min for circle stats). Offsets committed before DB write ⇒ crash/redeploy drops buffered deltas. Shutdown path only stops notification consumer + some syncers; `StopPostAggregator()` exists but never called. Every deploy = counter drift in PG (Redis still right until TTL).
- No `/healthz` / `/readyz`. LB/orchestrator can't tell alive vs ready.
- No rate-limit middleware (only per-endpoint OTP limits in Redis).
- Hertz graceful exit ≈ 5 s default. SSE conns just cut; clients reconnect (`retry_ms: 5000`). OK.

### 1.5 Resource guess (small launch, <10k DAU)

| Component | RAM | CPU | Disk |
|---|---|---|---|
| qubar | 300–600 MB steady, +64 MB per concurrent hash | 0.5–1 vCPU | – |
| PostgreSQL | 1–2 GB | low | 20–50 GB |
| Redis | 256 MB–1 GB data (+×2 on AOF rewrite fork) | low | AOF |
| Elasticsearch | heap 1–2 GB + page cache | burst | 10–50 GB |
| Redpanda | 1 GB (`--smp 1 --memory 1G`) | low | 10–20 GB |
| Kafka Connect (Debezium + ES sink) | heap 512 MB–1 GB | low | – |
| Nacos | 1–2 GB | – | **drop** |

All have arm64 images. Go cross-compile trivial. IK plugin = pure Java. ⇒ **Graviton (t4g/m7g/m8g)** ~20% cheaper.

---

## 2. Options weighed

| Choice | Pick | Why | Rejected |
|---|---|---|---|
| Cloud | **AWS ap-southeast-1** | S3/CloudFront already. Asia latency. | GCP/Azure (re-do storage). Hetzner/VPS ≈ 1/4 price but no IAM/VPC/managed PG; OK for hobby. |
| IaC | **Terraform / OpenTofu** | Standard, huge AWS provider, state in S3 (`use_lockfile`, no DynamoDB). | CDK/Pulumi (fine, but more code); CloudFormation (verbose). |
| Runtime | **Docker Compose on EC2**, driven by SSM | Cheapest. Stateful stuff (ES/Redpanda) easy. Same compose on Mac. | EKS (~$73/mo control plane alone, overkill). ECS Fargate for app: ok later, but needs ALB (~$20) + no gain at 1 replica. |
| PostgreSQL | **RDS PG 18** `db.t4g.medium` Single-AZ → Multi-AZ later | PITR, snapshots, patching. PG = only truth. Supports logical replication (`rds.logical_replication=1`). | Self-host on data box (Profile A only). Aurora (min cost higher, check PG18 support). |
| Redis | **self-host on data box, AOF everysec** (Profile B) | Code has no TLS → ElastiCache would run unencrypted anyway. Save ~$30. | ElastiCache/Valkey → Profile C after TLS code. ElastiCache Serverless forces TLS → blocked. |
| Search | **self-host ES 8 + IK** | Client = ES v8 (OpenSearch fails product check). IK needs custom image. Data rebuildable from PG via CDC ⇒ low-risk to self-host. | AWS OpenSearch (incompatible). Elastic Cloud (~$100+/mo, needs auth code). |
| MQ | **self-host Redpanda single node** | Events transient, loss tolerable. | MSK (IAM/SASL needed, $$$). Redpanda Cloud (SASL/TLS needed). |
| CDC | **Kafka Connect + Debezium PG source + ES sink** (container) | Keep existing pipeline. | Rewrite to app-side ES writes (big change). |
| Config | **SSM Parameter Store SecureString → rendered `config.yaml`** | Free (standard tier). IAM-scoped. | Nacos (extra JVM + DB). Secrets Manager ($0.40/secret, rotation not needed yet). |
| Edge | **CloudFront + VPC origin** (+ WAF) | TLS, HTTP/3, DDoS, no public IP on hosts, free 1 TB/mo. Heartbeat 25 s < CF 30 s origin read timeout ⇒ SSE ok. | ALB (+$20/mo, public). Nginx on public IP (need own TLS, exposed). |
| Egress | **fck-nat on t4g.nano** (B) → managed NAT GW (C) | ~$4 + IP vs ~$45+ + $0.059/GB. | IPv6-only egress (api.github.com etc. lack IPv6). |
| Registry | **ECR** | IAM pull, scan-on-push. Lifecycle keep 20. | GHCR (fine too; needs pull token on box). |
| CI auth | **GitHub OIDC → IAM role** | No long-lived AWS keys in GitHub. | Access keys. |
| Host access | **SSM Session Manager** | No port 22, audited. | SSH/bastion. |

---

## 3. Target architecture (Profile B "Launch")

```
                    Users (web / app)
                          │ HTTPS (h2/h3)
                          ▼
   ┌──────────────────────────────────────────────────────┐
   │ CloudFront  qubar.site / api.qubar.site              │
   │  + WAF: managed rules, rate limit /auth/*, geo opt   │
   │  behaviours:                                         │
   │   /media/*  → S3 (OAC, cached)                       │
   │   /*        → VPC origin app:8888 (CachingDisabled,  │
   │               fwd satoken + all qs, 50MB body ok)    │
   └───────────────┬───────────────────────┬──────────────┘
                   │ AWS backbone          │ OAC
   VPC 10.20.0.0/16│                       ▼
   ┌───────────────┼──────────────┐   S3 media bucket
   │ private-a     ▼              │   (BPA on, SSE-S3,
   │  ┌──────────────────────┐    │    versioning, lifecycle)
   │  │ EC2 app  t4g.medium  │────┼──► S3 gateway endpoint (free)
   │  │  qubar (1 replica)   │    │
   │  └─┬────┬────┬────┬─────┘    │
   │    │5432│6379│9200│19092     │
   │    ▼    │    ▼    ▼          │
   │ ┌──────┐│ ┌───────────────────────────────┐
   │ │ RDS  ││ │ EC2 data  t4g.large (gp3 100G) │
   │ │ PG18 │◄┼─┤ redis (AOF)  elasticsearch+IK │
   │ │      ││ │ redpanda     kafka-connect    │
   │ └──────┘│ │   (debezium src → ES sink)    │
   │   ▲ logical repl slot ◄────────┘         │
   │         └──────────────────────────────── │
   │ public-a: fck-nat (t4g.nano) ──► IGW ──► LLM / OAuth / Mailtrap
   └──────────────────────────────────────────┘
   Ops: SSM Session Mgr · CloudWatch Logs/Alarms · DLM EBS snapshots · ECR
   CI:  GitHub Actions ──OIDC──► ECR push ─► SSM RunCommand "compose pull && up -d"
```

### 3.1 Network + security groups

| SG | Ingress | Egress |
|---|---|---|
| `sg-app` | 8888 from CloudFront VPC-origin SG only | 5432→`sg-rds`, 6379/9200/19092→`sg-data`, 443→0.0.0.0/0 (via NAT) |
| `sg-data` | 6379, 9200, 19092 from `sg-app`; 9200 from Connect (same host) | 5432→`sg-rds` (Debezium), 443→S3 endpoint |
| `sg-rds` | 5432 from `sg-app`, `sg-data` | none |
| `sg-nat` | all from VPC CIDR | all |

- No public IP on app/data/RDS. RDS `publicly_accessible=false`.
- IMDSv2 required (`http_tokens=required`, hop limit 2 for containers). Blocks IMDS via SSRF POST.
- EBS encrypted (default KMS). RDS encrypted. S3 SSE-S3.
- Only 1 AZ for compute in B (cost). RDS subnet group spans 2 AZ (required) → easy Multi-AZ flip.

### 3.2 Hosts

**app** (`t4g.medium`, 4 GB, AL2023 arm64, gp3 20 GB)
- docker + compose plugin via cloud-init. Container: `qubar` distroless nonroot, read-only rootfs, `mem_limit: 3g`, `GOMEMLIMIT=2500MiB`, `stop_grace_period: 30s`.
- Log driver `awslogs` → CloudWatch group `/qubar/app`, retention 14 d.
- Config: boot script `aws ssm get-parameters-by-path /qubar/prod/` → render `/etc/qubar/config.yaml` (0400) → bind-mount RO. Run `-c /etc/qubar/config.yaml -b ""`.

**data** (`t4g.large`, 8 GB, gp3 100 GB 3000 IOPS; → `t4g.xlarge` when ES index > ~5 GB or heap pressure)

| Container | Limits | Notes |
|---|---|---|
| `redis:7` / `valkey:8` | 1 GB, `maxmemory 768mb`, `maxmemory-policy volatile-lru` | `appendonly yes`, `appendfsync everysec`, `requirepass`, `rename-command FLUSHALL/CONFIG ""`. bind docker net only. |
| `elasticsearch:8.x` + IK (custom image, IK version == ES version) | heap `-Xms1g -Xmx1g` (2g on xlarge), mem 2.5 GB | single-node, `xpack.security.enabled=false` **until code supports auth** (then enable). `repository-s3` snapshots. |
| `redpanda` | `--smp 1 --memory 1G --reserve-memory 0M --overprovisioned` | `auto_create_topics_enabled=true` (code relies on it). retention 3 d. |
| `kafka-connect` (Debezium PG + ES sink) | heap 768 MB | connectors JSON in repo, POSTed by deploy script. Offsets/configs in Redpanda topics. |

Host sysctl: `vm.max_map_count=262144`, `vm.overcommit_memory=1` (Redis fork).

**RDS** `db.t4g.medium` PG 18, gp3 50 GB autoscale → 200 GB.
- Param group: `rds.logical_replication=1`, `max_slot_wal_keep_size=10240` (cap WAL if Debezium dies → no disk-full outage), `log_min_duration_statement=500`, `rds.force_ssl=1` after app sets `sslmode=require`.
- Backup 7 d PITR, deletion protection, final snapshot, Performance Insights (free 7 d), auto minor upgrade.
- Roles: `qubar_owner` (DDL, migrations CI only), `qubar_web_app` (DML), `debezium` (`rds_replication` + SELECT on `domains`). Publication `dbz_publication FOR TABLES IN SCHEMA domains` (or listed tables).

### 3.3 Edge (CloudFront)

- Alt domains `qubar.site`, `api.qubar.site`; ACM cert in `us-east-1`.
- API behaviour: `CachingDisabled`, origin request `AllViewerExceptHostHeader`, allow all methods. Origin timeout 30 s (SSE heartbeat 25 s fits), keepalive 60 s.
- Media behaviour: S3 via OAC, `CachingOptimized`. Set `s3.cloudfront_domain` to this.
- WAF (or CloudFront **flat-rate plan**, bundles WAF+DDoS — check current tiers): AWS managed Common + KnownBadInputs + IP reputation; rate rule `/auth/*` e.g. 100 req / 5 min / IP; body limit rule exempt `/upload/*` (50 MB).
- Response headers policy: HSTS, nosniff, frame DENY, referrer strict.
- Qubar sees client IP in `X-Forwarded-For` / `CloudFront-Viewer-Address`. Hertz `ClientIP()` trusts XFF blindly → OK only because origin reachable from CF only. Don't use IP for auth decisions.

### 3.4 CI/CD

```
PR  → ci.yml:        go vet, go test ./pkg/..., golangci-lint, docker build (no push), trivy scan, terraform fmt/validate/plan (comment)
main→ deploy.yml:    build arm64 image (buildx, cache) → ECR :sha
                     → migrate job (owner role, SSM param creds) → SSM RunCommand on app: pull :sha, compose up -d, wait /readyz
                     → smoke: curl https://api.qubar.site/healthz
infra→ terraform.yml: plan on PR, apply on main w/ GitHub environment approval
```
- GitHub OIDC role scoped: ECR push, `ssm:SendCommand` on tagged instances, `ssm:GetParameters` for migrate only.
- Rollback = redeploy previous `:sha` (keep 20 tags via ECR lifecycle).
- Deploy = stop old → start new ⇒ ~5–15 s gap at 1 replica. Acceptable at launch. (Brief overlap of 2 processes is harmless: syncers idempotent/atomic, consumer group rebalances, SSE clients reconnect.)

### 3.5 Backup / DR

| Data | How | RPO / RTO |
|---|---|---|
| PG (RDS) | automated PITR 7 d + weekly manual snapshot copy (AWS Backup) | 5 min / ~30 min |
| Redis | AOF everysec on EBS + DLM daily EBS snapshot | 1 s (process crash) / 24 h (host loss). Loss = users re-login + unflushed counters. Acceptable. |
| ES | rebuildable: drop index → Debezium re-snapshot. Optional weekly S3 snapshot | – / 1–2 h |
| Redpanda | transient, no backup | loss = ≤ flush window of counters |
| S3 media | versioning, noncurrent → IA 30 d → expire 180 d. Optional CRR (costs ×2) | 0 / 0 |
| Config | SSM params + Terraform state (S3 versioned) | 0 |
| Infra | `terraform apply` rebuilds everything except data | ~1 h |

Run restore drill quarterly: RDS PITR into new instance → point staging at it.

### 3.6 Observability (cheap)

- CloudWatch Logs (app + data containers), 14 d retention. Keep `log.level: info` (SQL debug logs = $$ + PII).
- Alarms → SNS email: EC2 status check (auto-recover action), EBS disk > 80%, data host mem > 90% (CW agent), RDS CPU > 80% / FreeStorage < 20% / `OldestReplicationSlotLag` > 1 GB / `TransactionLogsDiskUsage` climbing, CloudFront 5xx rate > 2%, Route 53 health check on `/healthz` ($0.50–1).
- Later: Prometheus `/metrics` + Grafana Cloud free tier. Kafka consumer lag (Redpanda exposes metrics).

---

## 4. Profiles + cost

Ballpark, ap-southeast-1, on-demand, 730 h. Verify.

| Item | A "Solo" | **B "Launch"** | C "Scale" |
|---|---|---|---|
| App compute | on data box | EC2 t4g.medium ~$31 | ECS/EC2 ASG 2–4× t4g/m7g.large ~$130–260 + ALB ~$25 |
| Data box | 1× t4g.large 8 GB, **everything incl. PG** ~$62 | t4g.large ~$62 | split: ES 3× m7g.large or Elastic Cloud ~$250+; Redpanda 3 node or Redpanda Cloud |
| PostgreSQL | in compose (+wal-g → S3) | RDS t4g.medium SAZ ~$65 + 50 GB ~$7 | RDS m7g.large Multi-AZ ~$300 + read replica |
| Redis | in compose | in compose | ElastiCache Valkey Multi-AZ t4g.small×2 ~$60 (needs TLS code) |
| EBS | 150 GB ~$15 | 20+100 GB ~$12 | ~$40 |
| NAT | fck-nat ~$4 + IPv4 ~$4 | same ~$8 | NAT GW ~$45 + data |
| CloudFront + WAF | ~$0–15 | ~$15 | ~$30–100 |
| S3 + logs + R53 + ECR | ~$8 | ~$10 | ~$30+ |
| **Total** | **≈ $90–110** | **≈ $200–210** | **≈ $900–1,200** |
| 1-yr Savings Plan / RI | ≈ $70 | ≈ $150 | ≈ $700 |
| Good for | demo, <1k DAU, can stomach PG host-loss (restore from S3 WAL) | launch → ~20k DAU | after P2 code changes, real HA |

Cost levers:
- Graviton everywhere (done). Savings Plan once load stable (≈ −30%).
- Stop dev/staging at night (Instance Scheduler / Lambda cron) ≈ −60% on non-prod.
- Staging = Profile A shape (one box) with `APP_ENV=dev`.
- Avoid managed NAT GW until Scale. Use S3 gateway endpoint (free) so media bytes skip NAT.
- CloudFront always-free tier: 1 TB out + 10 M req/mo.
- Keep CloudWatch log level `info`, retention 14 d.
- Hobby floor: Lightsail 8 GB (~$40) running Profile A compose — loses VPC origin/IAM niceties; Terraform still works.

---

## 5. Security checklist

**Critical (code + infra)**
1. **SSRF via AI agent `base_url`.** Any circle owner sets `base_url`; server POSTs to it (`pkg/domains/aiagent/infrastructure/llm_eino.go`). Target can be `http://<data-host>:9200/...` (ES has no auth) or other internal svc. SG can't stop it — app legitimately talks to ES. Fix in code: https only, resolve host, reject loopback/private/link-local/CGNAT/ULA at **dial time** (custom `net.Dialer.Control`, beats DNS rebinding), no redirects to private. Infra: IMDSv2 required; ES auth once supported.
2. **Secrets in git.** `configs/config.yaml` has DB password + `data_key`. Rotate DB password. Generate **new** prod `data_key` (32+ random bytes) — never reuse dev key. Note: key rotation unsupported (no key id in ciphertext) → losing/changing key = all stored agent `api_key`s unreadable. Back up key in SSM + offline. Replace committed file with `config.example.yaml`; git-ignore real one; purge history if repo public.
3. Prod config: `server.mode: release`, `pgsql.log_mode: error`, `log.level: info`, CORS only real origins (no `localhost:*`).

**Network**
- Nothing listens publicly except CloudFront. No SSH. SSM only.
- ES / Redpanda / Redis have no auth in code → private SG only, bind to docker net, never `0.0.0.0` on a public host. Redis `requirepass` anyway (after verifying Sa-Token URL bug).
- PG `sslmode=require` (then `verify-full` w/ RDS CA bundle baked into image).

**Identity**
- EC2 instance role: S3 media bucket prefix RW, SSM params `/qubar/prod/*` read, CloudWatch logs, ECR pull. Nothing else.
- Until S3 IAM-role code lands: dedicated IAM user, bucket-scoped policy, keys in SSM. Then delete user.
- GitHub → AWS via OIDC, repo+branch conditioned.
- DB: owner vs app vs debezium roles split (already designed in `docs/pgsql-ddl/README.md`).

**App**
- Rate limit at edge now (WAF on `/auth/login`, `/auth/*/send-code`, `/auth/password/*`, `/upload/*`). Argon2 64 MB/hash → login flood = OOM. Consider `threads: 2` on 2-vCPU box.
- Container: distroless nonroot, read-only rootfs, `no-new-privileges`, drop all caps.
- Image scan (ECR scan-on-push / trivy in CI). Dependabot for Go modules + base images.
- S3 bucket: Block Public Access, OAC only, presigned PUT with content-type + size conditions.

---

## 6. Code changes needed (ranked)

**P0 — before prod**
1. SSRF guard for LLM HTTP client (§5.1).
2. Remove secrets from `configs/config.yaml`; add `configs/config.example.yaml`.
3. `GET /healthz` (process up) + `GET /readyz` (PG ping, Redis ping; ES/Redpanda optional flags). Exempt from auth + logging noise.
4. Env override: in `initFromFile`, `v.SetEnvPrefix("QUBAR"); v.SetEnvKeyReplacer(strings.NewReplacer(".", "_")); v.AutomaticEnv()` → `QUBAR_PGSQL_PASSWORD` etc. (Keys must exist in YAML for Unmarshal to see them — they do.) Lets compose/ECS inject secrets without rendering files.
5. Graceful shutdown drains **all** aggregators (call `StopPostAggregator` + equivalents for circle/like/collect/history/hot/interaction) before `CloseRedis`. Cut flush windows (27 min → ≤ 2 min) or switch to `FetchMessage` + commit after DB write. Otherwise every deploy loses counter deltas.
6. Dockerfile (below).

**P1 — to unlock managed services / hardening**
7. S3: if keys empty → `config.LoadDefaultConfig` default chain (IAM role).
8. ES: `username/password/api_key/ca_cert` config → enable xpack security.
9. Redis: TLS + ACL username options; build Sa-Token URL with `url.UserPassword` (escape) — or pass the go-redis client.
10. Redpanda: SASL/SCRAM + TLS dialer options.
11. Migrations: move DDL from `docs/pgsql-ddl/*.md` into `migrations/*.sql`, run with golang-migrate/goose/Atlas as owner role in CI.
12. App-level rate limiter (Redis token bucket) for auth + upload + AI trigger endpoints.

**P2 — before >1 replica**
13. SSE fan-out via Redis pub/sub `notice:unread:push` (already planned in `docs/design/sse-notification-design.md` §P1).
14. Syncer leader lease: Redis `SET lock:syncer:<name> <id> NX PX <ttl>` + renew; only holder runs tick.
15. `-role=api|worker|all` flag: API replicas scale on CPU; 1 worker runs consumers + syncers.
16. `/metrics` (Prometheus): HTTP latency, consumer lag, flush duration, SSE conns.

---

## 7. Repo layout (IaC)

```
deploy/
  docker/
    Dockerfile                    # multi-stage, arm64+amd64
  compose/
    compose.dev.yml               # Mac: PG18(wal_level=logical) + Redis + ES/IK + Redpanda + Connect → replaces Homebrew
    compose.data.yml              # prod data host
    compose.app.yml               # prod app host
    elasticsearch/Dockerfile      # FROM elasticsearch:8.x + IK plugin (same version)
    connect/Dockerfile            # debezium/connect + ES sink connector
    connect/connectors/
      pg-source.json              # topic.prefix=pg, plugin.name=pgoutput, slot.name=qubar_dbz, publication dbz_publication
      es-sink.json                # topics.regex=pg\.domains\..*, key.ignore=false, behavior.on.null.values=delete
    redis/redis.conf
  scripts/
    render-config.sh              # SSM → /etc/qubar/config.yaml (until P0#4)
    deploy.sh                     # pull image, compose up, wait /readyz
  terraform/
    bootstrap/                    # state bucket, GitHub OIDC provider + roles (apply once, local state)
    modules/
      network/                    # VPC, 2 AZ subnets, fck-nat, S3 gateway endpoint
      edge/                       # CloudFront, VPC origin, ACM, WAF, Route53
      media/                      # S3 bucket, OAC, lifecycle, CORS
      rds/                        # PG18, param group, subnet group, SG
      host/                       # EC2 + role + SG + cloud-init (reused for app & data)
      ops/                        # CloudWatch logs/alarms, SNS, DLM, AWS Backup
      ssm/                        # parameter placeholders (values set out-of-band, lifecycle ignore_changes)
    envs/
      staging/                    # Profile A
      prod/                       # Profile B
.github/workflows/
  ci.yml  deploy.yml  terraform.yml
```

Dockerfile sketch:

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/qubar ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/qubar /qubar
EXPOSE 8888
USER nonroot
ENTRYPOINT ["/qubar", "-c", "/etc/qubar/config.yaml", "-b", ""]
```

Secrets rule: Terraform creates SSM params with dummy value + `ignore_changes = [value]`. Real values set by human (`aws ssm put-parameter --overwrite`). No secrets in tfstate or git.

---

## 8. Local dev (Mac) change

Now: Homebrew PG/Redis + ad-hoc containers. Drift risk (PG version, `wal_level`, Redis config, IK version).

New: `docker compose -f deploy/compose/compose.dev.yml up -d` → same images as prod, arm64 native on Apple Silicon. Run qubar with `go run` or as container (`--profile app`). Keep Nacos optional (`--profile nacos`) if still wanted locally.

Migration: `pg_dump` from Homebrew PG → restore into compose PG18. Stop Homebrew services (`brew services stop postgresql@18 redis`) to free ports 5432/6379.

---

## 9. Rollout plan

1. **Week 0** — P0 code (§6 1–6). Dockerfile. `compose.dev.yml`; team switches off Homebrew.
2. **Week 1** — `terraform/bootstrap` + `network` + `media` + `rds` + `host` in **staging** (Profile A). Load DDL. Wire Debezium. Smoke test all endpoints + SSE via CloudFront.
3. **Week 2** — CI/CD pipelines, alarms, backup drill (RDS PITR restore). WAF rules in count mode → block.
4. **Week 3** — prod (Profile B). Migrate data (`pg_dump`/`pg_restore` into RDS, re-snapshot CDC to ES). DNS cut. Watch 48 h.
5. **Later** — P1 items → move Redis to ElastiCache, enable ES auth. P2 → Profile C when single app box > ~60% CPU sustained or HA needed.

---

## 10. Open questions (owner decides)

- Real traffic target (DAU, peak RPS)? Changes box size, not shape.
- Audience mostly mainland China? CloudFront has no mainland PoPs w/o ICP; Singapore origin still best non-ICP choice.
- Keep Nacos in prod? Doc assumes **no**.
- Downtime ok on deploy (~10 s) until P2? Doc assumes **yes**.
- Need staging always-on or on-demand?
