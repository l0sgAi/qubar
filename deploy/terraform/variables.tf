variable "cloudflare_account_id" {
  description = "Cloudflare 账号 ID（Dashboard 右侧栏 / 域名概览页）"
  type        = string
}

variable "cloudflare_zone_id" {
  description = "域名所在 zone 的 ID（域名概览页右侧栏）"
  type        = string
}

variable "domain" {
  description = "根域名，如 example.com（必须已托管在 Cloudflare）"
  type        = string
}

variable "api_subdomain" {
  description = "API 子域（→ 服务器 :8888，经 Tunnel）"
  type        = string
  default     = "api"
}

variable "ssh_subdomain" {
  description = "CI 用 SSH 子域（→ 服务器 :22，经 Tunnel + Access 服务令牌）"
  type        = string
  default     = "ssh"
}

variable "r2_location" {
  description = "R2 位置提示：APAC / WEUR / EEUR / ENAM / WNAM / OC。选离用户近的。"
  type        = string
  default     = "APAC"
}

variable "media_bucket_name" {
  description = "用户上传的媒体 bucket（通过自定义域名公开读）"
  type        = string
  default     = "qubar-media"
}

variable "backup_bucket_name" {
  description = "备份 bucket（私有）"
  type        = string
  default     = "qubar-backup"
}
