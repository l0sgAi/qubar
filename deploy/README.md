# deploy/ — single-VPS production stack

Design, cost and trade-offs: [`docs/deploy/low-cost-deployment-plan.md`](../docs/deploy/low-cost-deployment-plan.md).
This file is the operator runbook.

```
compose/    compose.prod.yml, .env.example, postgres init, redis.conf, ES(+IK) and Kafka Connect images, connector templates
config/     config.prod.yaml — non-secret app config (secrets/domains come in as QUBAR_* env vars)
scripts/    deploy.sh (rollout+rollback) · backup.sh · restore.sh · register-connectors.sh · postboot.sh
host/       bootstrap.sh — one-time VPS setup (Docker, swap, sysctl, deploy user, sshd, firewall, systemd timers)
terraform/  Cloudflare root: tunnel, DNS, Access (CI service token), R2 buckets
terraform/hetzner/  OPTIONAL separate root: Hetzner server + firewall (Hetzner is Europe-only for the cheap ARM plans — not recommended for an Asian audience)
```

## First-time setup

### 1. Accounts and tokens

| Need | Notes |
|---|---|
| Domain on Cloudflare (free plan) | Tunnel, Access and R2 custom domains need it. |
| Cloudflare API token | Account: *Cloudflare Tunnel: Edit*, *Access: Apps and Policies: Edit*, *Access: Service Tokens: Edit*, *Workers R2 Storage: Edit*; Zone: *DNS: Edit*. → `CLOUDFLARE_API_TOKEN` |
| A VPS ≥ 8 GB, Ubuntu 24.04 / Debian 12 | **Hong Kong** (e.g. Tencent Cloud Lighthouse 4c/8GB) or **Tokyo** (e.g. Contabo) for Singapore + Japan/Korea + mainland China users — see plan doc §3. Hetzner only via the optional `terraform/hetzner` root. |
| R2 bucket for Terraform state | Create once by hand (chicken-and-egg), private, e.g. `qubar-tfstate`, plus an R2 API token limited to it. |
| Deploy SSH key | `ssh-keygen -t ed25519 -f deploy_key -C qubar-ci-deploy` — public → `bootstrap.sh`, private → GitHub secret. |

### 2. Cloudflare (Terraform)

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars      # fill in
cp backend.hcl.example backend.hcl                # fill in R2 endpoint
export CLOUDFLARE_API_TOKEN=...
export AWS_ACCESS_KEY_ID=<r2-token-id> AWS_SECRET_ACCESS_KEY=<r2-token-secret>   # state backend
terraform init -backend-config=backend.hcl        # commit the generated .terraform.lock.hcl
terraform plan && terraform apply
```

Outputs you need: `tunnel_token`, `access_client_id`, `access_client_secret`, `ssh_hostname`.

> This root could not be `terraform validate`d where it was written (registry blocked). If `init`/`validate`
> complains, the fix is small and local — please report it or fix it in the PR.

### 2b. The server

**Any provider (recommended path):** create an Ubuntu 24.04 / Debian 12 VPS (≥ 8 GB), log in as root
(`sudo -i` if the image gives you `ubuntu`), then:

```bash
DEPLOY_SSH_PUBKEY="ssh-ed25519 AAAA… qubar-ci-deploy" bash deploy/host/bootstrap.sh
```

`bootstrap.sh` installs Docker, adds swap, sets kernel params, creates the `deploy` user, **disables root/password SSH,
and blocks all inbound ports**. Before you log out, be sure the tunnel/Access path works and you know how to open the
provider's web console. (`ALLOW_SSH_FROM=<your CIDR>` keeps port 22 open for that CIDR while you get set up.)

To make the tunnel reach the box, put `tunnel_token` into `.env` (`CLOUDFLARE_TUNNEL_TOKEN`); the first deploy starts
`cloudflared`.

**Hetzner only (optional):** `deploy/terraform/hetzner` is a separate root with its own state key:

```bash
cd deploy/terraform/hetzner
cp terraform.tfvars.example terraform.tfvars      # set deploy_ssh_public_key, server_type, location
export HCLOUD_TOKEN=...
terraform init -backend-config=../backend.hcl -backend-config="key=prod/hetzner.tfstate"
terraform apply
```

It creates the server with a zero-inbound firewall and runs `bootstrap.sh` via cloud-init.

### 3. Cloudflare dashboard (one-time, not in Terraform v1)

1. **R2 → `qubar-media` → Settings → Custom Domains** → `media.<domain>`.
2. **R2 → Manage API tokens**: two tokens, *Object Read & Write*, each limited to one bucket (`qubar-media`, `qubar-backup`). Access Key ID / Secret go into `.env`.
3. **Security → WAF**: enable managed rules; add a rate-limit rule for `/auth/*` (Argon2 = 64 MB per hash).

### 4. Server host key (recommended pinning)

From the provider console (or any earlier shell): `cat /etc/ssh/ssh_host_ed25519_key.pub`.
GitHub variable `SSH_HOST_KEY` = `ssh.<domain> ssh-ed25519 AAAA…` (one line).

### 5. GitHub

Environment `production` — secrets: `PROD_ENV_FILE` (the filled-in `compose/.env.example`), `DEPLOY_SSH_KEY`,
`CF_ACCESS_CLIENT_ID`, `CF_ACCESS_CLIENT_SECRET`. Variables: `SSH_HOSTNAME`, `SSH_HOST_KEY`, `PUBLIC_API_URL`.
Protect `main` (require PR + the CI checks). Details in the plan doc §5.

Generate secrets: `openssl rand -hex 24` for passwords; `openssl rand -base64 32` for `DATA_KEY`
(**back it up offline — there is no rotation, losing it bricks stored AI-agent api keys**).

### 6. First deploy and data load

1. Merge to `main` (or *Actions → deploy → Run workflow*). First run builds the image, syncs files, starts the stack.
2. Move data from the Mac:
   ```bash
   pg_dump -h 127.0.0.1 -U <owner> -d qubar -n domains -Fc --no-owner -f qubar.dump
   # copy to the server (scp through the tunnel, or the provider console upload), then on the server:
   /opt/qubar/scripts/restore.sh /opt/qubar/qubar.dump      # stops the app; refuses on a non-empty DB
   /opt/qubar/scripts/deploy.sh "$(cat /opt/qubar/.deploy/current)"   # start the app again
   ```
3. Check Elasticsearch fills via CDC (see below). Reconcile `compose/connect/connectors/*.json` with your local setup first.
4. **Restore drill** from an R2 backup on a scratch box before you call this production.

## Day-2 commands (on the server, as `deploy`)

```bash
cd /opt/qubar/compose
C="docker compose --env-file ../.env -f compose.prod.yml"
$C ps                               # health of everything
$C logs -f --tail 100 qubar
curl -s localhost:8888/readyz       # {"status":"ok","checks":{...}}
curl -s localhost:8083/connectors?expand=status | jq   # CDC health
cat /opt/qubar/.deploy/current /opt/qubar/.deploy/previous
/opt/qubar/scripts/backup.sh        # backup now (timer: daily 19:30 UTC)
systemctl list-timers qubar-backup.timer
free -h; docker stats --no-stream   # memory pressure → time for a bigger box
```

- **Roll back:** Actions → *deploy* → Run workflow → `tag=<previous sha-…>`; or on the box `scripts/deploy.sh <tag>`.
- **Rebuild Elasticsearch:** stop Connect's sink, delete `pg.domains.*` indices (keep templates), delete the source connector and its slot
  (`select pg_drop_replication_slot('qubar_dbz')`), re-register → snapshot re-indexes.
- **Disk:** `max_slot_wal_keep_size=4GB` caps WAL if Connect dies (slot gets invalidated instead of filling the disk — then re-snapshot).
- **Emergency shell if the tunnel is down:** the provider's web console (root login over SSH is disabled), or temporarily allow your IP: `ufw allow from <ip> to any port 22 proto tcp` from the console.

## Local sanity check of the compose file

```bash
cp deploy/compose/.env.example /tmp/x.env   # fill required values with dummies
docker compose --env-file /tmp/x.env -f deploy/compose/compose.prod.yml config -q
```
Postgres + Redis + Redpanda + app can be brought up locally without the tunnel/ES/Connect
(`docker compose … up -d postgres redis redpanda`, then the app with `QUBAR_IMAGE=qubar QUBAR_TAG=<local build>`).
