output "api_url" {
  value = "https://${local.api_fqdn}"
}

output "ssh_hostname" {
  description = "CI 用（GitHub variable SSH_HOSTNAME）"
  value       = local.ssh_fqdn
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
