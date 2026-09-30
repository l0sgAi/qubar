output "server_ipv4" {
  description = "仅用于排障；公网无法直连（防火墙零入站）"
  value       = hcloud_server.app.ipv4_address
}
