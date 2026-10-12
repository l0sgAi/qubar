# 非密钥参数，值在 terraform.tfvars（入库）。密钥只走环境变量。
# 阿里云侧不归 Terraform 管（无 RAM 权限）：安全组 / 快照在控制台手工维护，见 deploy/README.md。

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
