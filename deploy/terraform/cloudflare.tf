locals {
  api_fqdn = "${var.api_subdomain}.${var.domain}"
  ssh_fqdn = "${var.ssh_subdomain}.${var.domain}"
}

# ---------- Tunnel：服务器主动外连，公网入口不需要任何入站端口 ----------

resource "random_id" "tunnel_secret" {
  byte_length = 35 # ≥32 字节
}

resource "cloudflare_tunnel" "qubar" {
  account_id = var.cloudflare_account_id
  name       = "qubar-prod"
  secret     = random_id.tunnel_secret.b64_std
  config_src = "cloudflare" # 路由在这里（Terraform）管理，服务器上的 cloudflared 只需要 token
}

resource "cloudflare_tunnel_config" "qubar" {
  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_tunnel.qubar.id

  config {
    # cloudflared 容器用 host 网络，127.0.0.1 就是服务器本机
    ingress_rule {
      hostname = local.api_fqdn
      service  = "http://127.0.0.1:8888"
    }
    ingress_rule {
      hostname = local.ssh_fqdn
      service  = "ssh://127.0.0.1:22"
    }
    ingress_rule {
      service = "http_status:404"
    }
  }
}

resource "cloudflare_record" "api" {
  zone_id = var.cloudflare_zone_id
  name    = var.api_subdomain
  type    = "CNAME"
  content = cloudflare_tunnel.qubar.cname
  proxied = true
  ttl     = 1
  comment = "qubar API via Cloudflare Tunnel (managed by terraform)"
}

resource "cloudflare_record" "ssh" {
  zone_id = var.cloudflare_zone_id
  name    = var.ssh_subdomain
  type    = "CNAME"
  content = cloudflare_tunnel.qubar.cname
  proxied = true
  ttl     = 1
  comment = "CI ssh via Cloudflare Tunnel + Access (managed by terraform)"
}

# ---------- Access：只有持服务令牌的 CI 能打到 ssh 子域 ----------

resource "cloudflare_access_service_token" "ci" {
  account_id           = var.cloudflare_account_id
  name                 = "qubar-github-actions"
  duration             = "8760h" # 1 年；到期前 30 天内 apply 会自动续（需同步更新 GitHub secret）
  min_days_for_renewal = 30
}

resource "cloudflare_access_application" "ssh" {
  account_id       = var.cloudflare_account_id
  name             = "qubar-ssh"
  domain           = local.ssh_fqdn
  type             = "ssh"
  session_duration = "1h"
}

resource "cloudflare_access_policy" "ssh_ci" {
  account_id     = var.cloudflare_account_id
  application_id = cloudflare_access_application.ssh.id
  name           = "github-actions-service-token"
  precedence     = 1
  decision       = "non_identity"

  include {
    service_token = [cloudflare_access_service_token.ci.id]
  }
}

# ---------- R2 ----------
# 自定义域名（media.<domain>）在 v4 provider 里没有资源，需在控制台一次性绑定：
#   R2 → qubar-media → Settings → Custom Domains → Add → media.<domain>
# S3 兼容访问密钥（两把，分别限定到各自 bucket 的读写）同样在控制台创建：R2 → Manage API tokens。

resource "cloudflare_r2_bucket" "media" {
  account_id = var.cloudflare_account_id
  name       = var.media_bucket_name
  location   = var.r2_location
}

resource "cloudflare_r2_bucket" "backup" {
  account_id = var.cloudflare_account_id
  name       = var.backup_bucket_name
  location   = var.r2_location
}
