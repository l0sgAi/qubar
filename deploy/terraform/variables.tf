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

# ---- 服务器（Hetzner）。用别家 VPS（Contabo / Oracle …）时设 manage_server=false，手工开机后跑 host/bootstrap.sh ----

variable "manage_server" {
  description = "是否用 Terraform 创建 Hetzner 服务器"
  type        = bool
  default     = true
}

variable "server_name" {
  type    = string
  default = "qubar-prod"
}

variable "server_type" {
  description = "cax21=4 ARM vCPU/8GB（仅欧洲机房）；ES+PG+Redpanda+Connect 全塞一台，8GB 是下限，16GB(cax31) 更稳。新加坡只有 cpx 系列。"
  type        = string
  default     = "cax21"
}

variable "server_location" {
  description = "fsn1 / nbg1 / hel1（cax 系列）；sin（仅 cpx）"
  type        = string
  default     = "fsn1"
}

variable "server_image" {
  type    = string
  default = "ubuntu-24.04"
}

variable "server_backups" {
  description = "Hetzner 整机快照备份（+20% 机器价）。默认关：数据库备份走 R2（scripts/backup.sh）。"
  type        = bool
  default     = false
}

variable "deploy_ssh_public_key" {
  description = "CI 部署用的 SSH 公钥（ssh-keygen -t ed25519 -f deploy_key；私钥放 GitHub secret DEPLOY_SSH_KEY）"
  type        = string
}

variable "break_glass_ssh_cidrs" {
  description = "应急：允许直连 22 端口的 CIDR（如 [\"203.0.113.7/32\"]）。默认空 = 公网入站全封，只能经 Tunnel 或 Hetzner 控制台。"
  type        = list(string)
  default     = []
}
