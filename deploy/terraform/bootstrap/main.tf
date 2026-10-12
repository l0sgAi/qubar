# 一次性引导：创建存放 prod Terraform state 的 OSS bucket。
#
# 先有 bucket 才能用 OSS backend（鸡生蛋），故本目录用本地 state，只在本机跑一次：
#   export ALICLOUD_ACCESS_KEY=... ALICLOUD_SECRET_KEY=...
#   terraform -chdir=deploy/terraform/bootstrap init
#   terraform -chdir=deploy/terraform/bootstrap apply
# 本地 terraform.tfstate 已 git-ignore；丢了也无妨（bucket 可 `terraform import` 回来）。

terraform {
  required_version = ">= 1.10"
  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "~> 1.294"
    }
  }
}

variable "region" {
  type    = string
  default = "ap-southeast-8"
}

variable "state_bucket" {
  type    = string
  default = "qubar-tfstate-l0sgai" # 全局唯一；改名需同步 ../prod/versions.tf backend
}

provider "alicloud" {
  region = var.region
}

resource "alicloud_oss_bucket" "state" {
  bucket        = var.state_bucket
  storage_class = "Standard"

  versioning {
    status = "Enabled" # state 误写可回滚
  }

  server_side_encryption_rule {
    sse_algorithm = "AES256"
  }

  lifecycle_rule {
    id      = "expire-noncurrent"
    enabled = true
    prefix  = ""
    noncurrent_version_expiration {
      days = 90
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "alicloud_oss_bucket_public_access_block" "state" {
  bucket              = alicloud_oss_bucket.state.bucket
  block_public_access = true
}

output "state_bucket" {
  value = alicloud_oss_bucket.state.bucket
}
