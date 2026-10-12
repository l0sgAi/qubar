# CI 在同一 job 里读取这些输出喂给 Ansible / SSH，不需要手工搬运到 GitHub Secrets。

output "ssh_hostname" {
  value = local.ssh_hostname
}

output "tunnel_token" {
  value     = data.cloudflare_zero_trust_tunnel_cloudflared_token.prod.token
  sensitive = true
}

output "ci_access_client_id" {
  value     = cloudflare_zero_trust_access_service_token.ci.client_id
  sensitive = true
}

output "ci_access_client_secret" {
  value     = cloudflare_zero_trust_access_service_token.ci.client_secret
  sensitive = true
}
