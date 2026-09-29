output "api_url" {
  value = "https://${local.api_fqdn}"
}

output "ssh_hostname" {
  description = "CI 用（GitHub variable SSH_HOSTNAME）"
  value       = local.ssh_fqdn
}

output "server_ipv4" {
  description = "仅用于排障；公网无法直连（防火墙零入站）"
  value       = try(hcloud_server.app[0].ipv4_address, null)
}

output "tunnel_token" {
  description = ".env 的 CLOUDFLARE_TUNNEL_TOKEN"
  value       = cloudflare_tunnel.qubar.tunnel_token
  sensitive   = true
}

output "access_client_id" {
  description = "GitHub secret CF_ACCESS_CLIENT_ID"
  value       = cloudflare_access_service_token.ci.client_id
}

output "access_client_secret" {
  description = "GitHub secret CF_ACCESS_CLIENT_SECRET"
  value       = cloudflare_access_service_token.ci.client_secret
  sensitive   = true
}

output "media_bucket" {
  value = cloudflare_r2_bucket.media.name
}

output "backup_bucket" {
  value = cloudflare_r2_bucket.backup.name
}
