terraform {
  required_version = ">= 1.6"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.60"
    }
  }

  # 与 Cloudflare 那份共用同一个 R2 状态 bucket，但 key 不同：
  #   terraform init -backend-config=../backend.hcl -backend-config="key=prod/hetzner.tfstate"
  backend "s3" {}
}

# 令牌走环境变量：HCLOUD_TOKEN
provider "hcloud" {}
