resource "hcloud_ssh_key" "deploy" {
  name       = "${var.server_name}-deploy"
  public_key = var.deploy_ssh_public_key
}

# 默认零入站规则：公网无法直连服务器任何端口。流量只走 cloudflared 的出站长连接。
resource "hcloud_firewall" "locked_down" {
  name = "${var.server_name}-locked-down"

  dynamic "rule" {
    for_each = var.break_glass_ssh_cidrs
    content {
      description = "break-glass ssh"
      direction   = "in"
      protocol    = "tcp"
      port        = "22"
      source_ips  = [rule.value]
    }
  }
}

resource "hcloud_server" "app" {
  name         = var.server_name
  server_type  = var.server_type
  image        = var.server_image
  location     = var.server_location
  ssh_keys     = [hcloud_ssh_key.deploy.id]
  firewall_ids = [hcloud_firewall.locked_down.id]
  backups      = var.server_backups

  public_net {
    ipv4_enabled = true # ghcr.io / Docker Hub 拉镜像需要 IPv4
    ipv6_enabled = true
  }

  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    bootstrap_b64  = base64encode(file("${path.module}/../../host/bootstrap.sh"))
    deploy_pubkey  = trimspace(var.deploy_ssh_public_key)
    allow_ssh_from = join(",", var.break_glass_ssh_cidrs)
  })

  labels = {
    app = "qubar"
    env = "prod"
  }

  # 改 bootstrap 脚本 / 镜像版本不该重建带数据的机器；需要时手动 taint。
  lifecycle {
    ignore_changes = [user_data, image, ssh_keys]
  }
}
