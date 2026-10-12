# 非密钥参数，值在 terraform.tfvars（入库）。密钥只走环境变量。

variable "region" {
  type = string
}

variable "instance_id" {
  description = "已手工购买的 ECS 实例 ID（不由 Terraform 创建/销毁，只读引用）"
  type        = string
}

variable "ssh_allowed_cidrs" {
  description = <<-EOT
    额外放行公网 22 的 CIDR（应急直连用）。默认空 = Terraform 不开 22；
    日常 SSH / CI 走 Cloudflare Access（ssh.<domain>）。
    注意：控制台手工建的 22 入站规则不归 Terraform 管，CF SSH 验证通过后到控制台删除。
  EOT
  type        = list(string)
  default     = []
}

variable "backup_bucket" {
  description = "PG 备份 OSS bucket（全局唯一）"
  type        = string
}

variable "backup_retention_days" {
  type    = number
  default = 30
}

variable "snapshot_retention_days" {
  type    = number
  default = 14
}

variable "cloudflare_account_id" {
  type = string
}

variable "cloudflare_zone_id" {
  type = string
}

variable "domain" {
  type = string
}

variable "owner_emails" {
  description = "可通过 Cloudflare Access（邮箱 OTP）登录 ssh / kibana / redpanda 控制台的邮箱"
  type        = list(string)
}

variable "api_cutover" {
  description = <<-EOT
    false：只建 api-next.<domain> 供冒烟，api.<domain> 仍指向本机 dev tunnel。
    true ：接管 api.<domain>。切换前须先在 CF 控制台删掉旧的 api CNAME（属于 dev-debug tunnel），否则创建冲突。
  EOT
  type        = bool
  default     = false
}
