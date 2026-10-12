terraform {
  required_version = ">= 1.10"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.27"
    }
  }

  # state 存 Cloudflare R2（S3 兼容；桶由 ../bootstrap 创建）。use_lockfile = S3 条件写原生锁。
  # 凭证：AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY = R2 API token 的 Access Key ID / Secret。
  backend "s3" {
    bucket = "qubar-tfstate"
    key    = "prod/terraform.tfstate"
    region = "auto"
    endpoints = {
      s3 = "https://2328bffc01bc4969ab19339d6dcbf5d9.r2.cloudflarestorage.com"
    }
    use_path_style              = true
    use_lockfile                = true
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }
}

provider "cloudflare" {
  # 凭证：CLOUDFLARE_API_TOKEN（权限见 deploy/README.md）
}
