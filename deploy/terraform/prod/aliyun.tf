# 阿里云：ECS 只读引用（手工购买，不归 Terraform 生命周期管理）+ 周边资源。

data "alicloud_instances" "this" {
  ids = [var.instance_id]
}

data "alicloud_ecs_disks" "system" {
  instance_id = var.instance_id
  disk_type   = "system"
}

locals {
  instance       = data.alicloud_instances.this.instances[0]
  security_group = local.instance.security_groups[0]
  system_disk_id = data.alicloud_ecs_disks.system.disks[0].id
}

# ---------- 安全组：默认不开任何入站（cloudflared 出站建隧道），仅应急放行 ----------
resource "alicloud_security_group_rule" "ssh_emergency" {
  for_each = toset(var.ssh_allowed_cidrs)

  security_group_id = local.security_group
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "22/22"
  cidr_ip           = each.value
  policy            = "accept"
  priority          = 1
  nic_type          = "intranet" # VPC 网络固定为 intranet
  description       = "terraform: emergency ssh"
}

# ---------- 系统盘自动快照（整机兜底，含 docker 卷） ----------
resource "alicloud_ecs_auto_snapshot_policy" "daily" {
  auto_snapshot_policy_name = "qubar-daily"
  repeat_weekdays           = ["1", "2", "3", "4", "5", "6", "7"]
  time_points               = ["4"] # 04:00（PG 逻辑备份 03:30 之后）
  retention_days            = var.snapshot_retention_days
}

resource "alicloud_ecs_auto_snapshot_policy_attachment" "system" {
  auto_snapshot_policy_id = alicloud_ecs_auto_snapshot_policy.daily.id
  disk_id                 = local.system_disk_id
}

# ---------- PG 逻辑备份 bucket ----------
resource "alicloud_oss_bucket" "backup" {
  bucket        = var.backup_bucket
  storage_class = "Standard"

  server_side_encryption_rule {
    sse_algorithm = "AES256"
  }

  lifecycle_rule {
    id      = "expire-pg-dumps"
    enabled = true
    prefix  = "pg/"
    expiration {
      days = var.backup_retention_days
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "alicloud_oss_bucket_public_access_block" "backup" {
  bucket              = alicloud_oss_bucket.backup.bucket
  block_public_access = true
}

# ---------- ECS 实例 RAM 角色：机器内 ossutil 走 STS 临时凭证，主机上不落 AK ----------
resource "alicloud_ram_role" "ecs" {
  role_name   = "qubar-ecs"
  description = "qubar ECS instance role (pg backup to OSS)"
  assume_role_policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = ["ecs.aliyuncs.com"] }
    }]
  })
}

resource "alicloud_ram_policy" "backup_write" {
  policy_name = "qubar-backup-write"
  description = "qubar ECS: put/list pg dumps in backup bucket (no delete; expiry via lifecycle)"
  policy_document = jsonencode({
    Version = "1"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["oss:PutObject", "oss:GetObject", "oss:AbortMultipartUpload", "oss:ListParts"]
        Resource = ["acs:oss:*:*:${var.backup_bucket}/pg/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["oss:ListObjects", "oss:GetBucketInfo"]
        Resource = ["acs:oss:*:*:${var.backup_bucket}"]
      },
    ]
  })
}

resource "alicloud_ram_role_policy_attachment" "backup_write" {
  role_name   = alicloud_ram_role.ecs.role_name
  policy_name = alicloud_ram_policy.backup_write.policy_name
  policy_type = "Custom"
}

resource "alicloud_ecs_ram_role_attachment" "ecs" {
  ram_role_name = alicloud_ram_role.ecs.role_name
  instance_id   = var.instance_id
}
