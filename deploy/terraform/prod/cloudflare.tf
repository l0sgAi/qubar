# Cloudflare：生产 tunnel（远程托管配置）+ DNS + Zero Trust Access。
#
# 服务器上 cloudflared 用 host 网络，所有服务只监听 127.0.0.1，公网入站零端口。
# 前置：Cloudflare 控制台已开通 Zero Trust（免费版，设定 team 域名），登录方式保留默认 One-time PIN。

locals {
  # 子域前缀 → 本机服务
  public_services = {
    "api-next" = "http://127.0.0.1:8888" # 切流前冒烟入口；切流后保留作直连备用
  }
  admin_services = {
    "kibana"   = "http://127.0.0.1:5601"
    "redpanda" = "http://127.0.0.1:8090"
  }
  # api 主域名仅在 api_cutover=true 时接管 DNS；ingress 规则始终在，便于切换前后一致
  api_hostname = "api.${var.domain}"
  ssh_hostname = "ssh.${var.domain}"

  dns_hostnames = merge(
    { for k, _ in local.public_services : k => "${k}.${var.domain}" },
    { for k, _ in local.admin_services : k => "${k}.${var.domain}" },
    { ssh = local.ssh_hostname },
    var.api_cutover ? { api = local.api_hostname } : {},
  )

  # SSE（/notice/stream）长连：关闭缓冲相关由 cloudflared 默认处理；keepalive 拉长
  api_origin_request = {
    connect_timeout    = 10
    keep_alive_timeout = 90
    http_host_header   = local.api_hostname
  }
}

resource "cloudflare_zero_trust_tunnel_cloudflared" "prod" {
  account_id = var.cloudflare_account_id
  name       = "qubar-prod"
  config_src = "cloudflare"
}

resource "cloudflare_zero_trust_tunnel_cloudflared_config" "prod" {
  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.prod.id

  config = {
    ingress = concat(
      [{
        hostname       = local.api_hostname
        service        = "http://127.0.0.1:8888"
        origin_request = local.api_origin_request
      }],
      [for k, svc in local.public_services : {
        hostname       = "${k}.${var.domain}"
        service        = svc
        origin_request = local.api_origin_request
      }],
      [for k, svc in local.admin_services : {
        hostname = "${k}.${var.domain}"
        service  = svc
      }],
      [{
        hostname = local.ssh_hostname
        service  = "ssh://127.0.0.1:22"
      }],
      [{ service = "http_status:404" }],
    )
  }
}

data "cloudflare_zero_trust_tunnel_cloudflared_token" "prod" {
  account_id = var.cloudflare_account_id
  tunnel_id  = cloudflare_zero_trust_tunnel_cloudflared.prod.id
}

resource "cloudflare_dns_record" "tunnel" {
  for_each = local.dns_hostnames

  zone_id = var.cloudflare_zone_id
  name    = each.value
  type    = "CNAME"
  content = "${cloudflare_zero_trust_tunnel_cloudflared.prod.id}.cfargotunnel.com"
  proxied = true
  ttl     = 1
  comment = "terraform: qubar-prod tunnel"
}

# ---------- Zero Trust Access ----------

# CI（GitHub Actions）经 cloudflared access ssh 登录服务器用的服务令牌
resource "cloudflare_zero_trust_access_service_token" "ci" {
  account_id = var.cloudflare_account_id
  name       = "qubar-github-actions"
  duration   = "8760h"
}

resource "cloudflare_zero_trust_access_policy" "owner" {
  account_id       = var.cloudflare_account_id
  name             = "qubar-owner"
  decision         = "allow"
  session_duration = "24h"
  include          = [for e in var.owner_emails : { email = { email = e } }]
}

resource "cloudflare_zero_trust_access_policy" "ci" {
  account_id = var.cloudflare_account_id
  name       = "qubar-ci-service-token"
  decision   = "non_identity"
  include = [{
    service_token = { token_id = cloudflare_zero_trust_access_service_token.ci.id }
  }]
}

resource "cloudflare_zero_trust_access_application" "ssh" {
  account_id       = var.cloudflare_account_id
  name             = "qubar-ssh"
  type             = "self_hosted"
  domain           = local.ssh_hostname
  session_duration = "24h"
  policies = [
    { id = cloudflare_zero_trust_access_policy.owner.id, precedence = 1 },
    { id = cloudflare_zero_trust_access_policy.ci.id, precedence = 2 },
  ]
}

resource "cloudflare_zero_trust_access_application" "admin" {
  for_each = local.admin_services

  account_id       = var.cloudflare_account_id
  name             = "qubar-${each.key}"
  type             = "self_hosted"
  domain           = "${each.key}.${var.domain}"
  session_duration = "24h"
  policies = [
    { id = cloudflare_zero_trust_access_policy.owner.id, precedence = 1 },
  ]
}
