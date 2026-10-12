terraform {
  required_version = ">= 1.10"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "~> 1.294"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.27"
    }
  }

  # state 存 OSS（bucket 由 ../bootstrap 创建，开了版本控制）。
  # 未配 TableStore 锁：并发由 GitHub Actions concurrency group 串行化，本机勿与 CI 同时 apply。
  # 凭证读 ALICLOUD_ACCESS_KEY / ALICLOUD_SECRET_KEY 环境变量。
  backend "oss" {
    bucket  = "qubar-tfstate-l0sgai"
    prefix  = "prod"
    key     = "terraform.tfstate"
    region  = "ap-southeast-8"
    encrypt = true
  }
}

provider "alicloud" {
  region = var.region # 凭证：ALICLOUD_ACCESS_KEY / ALICLOUD_SECRET_KEY
}

provider "cloudflare" {
  # 凭证：CLOUDFLARE_API_TOKEN（权限见 deploy/README.md）
}
