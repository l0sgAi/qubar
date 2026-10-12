# 一次性引导：创建 Cloudflare R2 桶 —— Terraform state 桶 + PG 备份桶（含过期规则）。
#
# 先有桶才能用 R2 做 state backend、才能签发限定这两个桶的 R2 API token（鸡生蛋），
# 故本目录用本地 state，只在本机跑一次：
#   export CLOUDFLARE_API_TOKEN=...
#   terraform -chdir=deploy/terraform/bootstrap init
#   terraform -chdir=deploy/terraform/bootstrap apply
# 本地 terraform.tfstate 已 git-ignore；丢了也无妨（桶可 `terraform import` 回来）。

terraform {
  required_version = ">= 1.10"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.27"
    }
  }
}

variable "cloudflare_account_id" {
  type    = string
  default = "2328bffc01bc4969ab19339d6dcbf5d9"
}

variable "backup_retention_days" {
  type    = number
  default = 30
}

provider "cloudflare" {
  # 凭证：CLOUDFLARE_API_TOKEN
}

resource "cloudflare_r2_bucket" "state" {
  account_id = var.cloudflare_account_id
  name       = "qubar-tfstate" # 改名需同步 ../prod/versions.tf backend
  location   = "apac"

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_r2_bucket" "backup" {
  account_id = var.cloudflare_account_id
  name       = "qubar-backup" # 改名需同步 ../../ansible/group_vars/all.yml backup_bucket
  location   = "apac"

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_r2_bucket_lifecycle" "backup" {
  account_id  = var.cloudflare_account_id
  bucket_name = cloudflare_r2_bucket.backup.name
  rules = [{
    id      = "expire-pg-dumps"
    enabled = true
    conditions = {
      prefix = "pg/"
    }
    delete_objects_transition = {
      condition = {
        type    = "Age"
        max_age = var.backup_retention_days * 86400
      }
    }
  }]
}

output "r2_endpoint" {
  value = "https://${var.cloudflare_account_id}.r2.cloudflarestorage.com"
}

output "buckets" {
  value = [cloudflare_r2_bucket.state.name, cloudflare_r2_bucket.backup.name]
}
