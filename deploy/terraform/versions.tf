terraform {
  required_version = ">= 1.6"

  required_providers {
    # v4 系列（资源名 cloudflare_tunnel / cloudflare_record …）。升 v5 需要整体改名，见 README。
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.52"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # 状态存 Cloudflare R2（S3 兼容，免费额度内 $0）。参数见 backend.hcl.example：
  #   terraform init -backend-config=backend.hcl
  backend "s3" {}
}

# 令牌走环境变量，不进代码/状态文件：
#   CLOUDFLARE_API_TOKEN
provider "cloudflare" {}
