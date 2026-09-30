# 可选：只有用 Hetzner 时才需要这一份。选别家 VPS（腾讯云 / Contabo / AWS …）时跳过，直接跑 host/bootstrap.sh。
# 注意 Hetzner 的 CAX/CX 只有欧洲机房，亚洲用户延迟高（见 docs/deploy/low-cost-deployment-plan.md §3）。

variable "server_name" {
  type    = string
  default = "qubar-prod"
}

variable "server_type" {
  description = "cax21=4 ARM vCPU/8GB（仅欧洲机房）；新加坡只有 cpx 系列。"
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
  description = "Hetzner 整机快照备份（+20% 机器价）。数据库备份走 R2（scripts/backup.sh），这是第二道保险，建议开。"
  type        = bool
  default     = true
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
