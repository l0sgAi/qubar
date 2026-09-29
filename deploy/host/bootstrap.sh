#!/usr/bin/env bash
# shellcheck disable=SC1091
# 新 VPS 一次性初始化（Ubuntu 24.04 / Debian 12，root 执行；可重复执行）。
# Terraform 通过 cloud-init 调用；Contabo / Oracle 等手工开的机器直接 scp 上去跑即可：
#
#   DEPLOY_SSH_PUBKEY="ssh-ed25519 AAAA... ci-deploy" bash bootstrap.sh
#
# 做的事：Docker、2G swap、内核参数、deploy 用户、SSH 加固、防火墙（默认只出不进）、每日备份 timer。
set -euo pipefail

: "${DEPLOY_SSH_PUBKEY:?set DEPLOY_SSH_PUBKEY (CI 用的公钥；私钥放 GitHub secret)}"
ALLOW_SSH_FROM="${ALLOW_SSH_FROM:-}" # 留空 = 22 端口不对公网开放，只经 Cloudflare Tunnel 访问
SWAP_GB="${SWAP_GB:-2}"
ROOT=/opt/qubar

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y --no-install-recommends ca-certificates curl gnupg ufw gettext-base age unattended-upgrades

# ---- Docker（官方 apt 源，含 compose 插件）----
if ! command -v docker >/dev/null; then
  . /etc/os-release
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" \
    >/etc/apt/sources.list.d/docker.list
  apt-get update -y
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
fi
cat >/etc/docker/daemon.json <<'JSON'
{ "log-driver": "json-file", "log-opts": { "max-size": "10m", "max-file": "3" }, "live-restore": true }
JSON
systemctl enable --now docker
systemctl reload docker || systemctl restart docker

# ---- swap + 内核参数（ES 需要 max_map_count；Redis fork 需要 overcommit）----
if ! swapon --show | grep -q .; then
  fallocate -l "${SWAP_GB}G" /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
  grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi
cat >/etc/sysctl.d/90-qubar.conf <<'CONF'
vm.max_map_count = 262144
vm.overcommit_memory = 1
vm.swappiness = 10
CONF
sysctl --system >/dev/null

# ---- deploy 用户（CI 只用它；在 docker 组 ≈ root 权限，所以私钥只放 GitHub Environment secret）----
id deploy >/dev/null 2>&1 || useradd -m -s /bin/bash deploy
usermod -aG docker deploy
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
echo "$DEPLOY_SSH_PUBKEY" >/home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys && chmod 600 /home/deploy/.ssh/authorized_keys
install -d -m 750 -o deploy -g deploy "$ROOT" "$ROOT/compose" "$ROOT/config" "$ROOT/scripts" "$ROOT/.deploy"

# ---- SSH 加固：仅密钥、禁 root ----
cat >/etc/ssh/sshd_config.d/90-qubar.conf <<'CONF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers deploy
CONF
systemctl reload ssh || systemctl reload sshd

# ---- 防火墙：默认拒绝入站。公网入口只有 cloudflared 的出站长连接。----
# Docker 发布的端口会绕过 ufw，所以 compose 里所有端口都只绑 127.0.0.1。
ufw --force reset >/dev/null
ufw default deny incoming
ufw default allow outgoing
[[ -n "$ALLOW_SSH_FROM" ]] && ufw allow from "$ALLOW_SSH_FROM" to any port 22 proto tcp
ufw --force enable

# ---- 每日备份 timer ----
cat >/etc/systemd/system/qubar-backup.service <<'UNIT'
[Unit]
Description=Qubar PostgreSQL backup to R2
After=docker.service
[Service]
Type=oneshot
User=deploy
ExecStart=/opt/qubar/scripts/backup.sh
UNIT
cat >/etc/systemd/system/qubar-backup.timer <<'UNIT'
[Unit]
Description=Daily Qubar backup
[Timer]
OnCalendar=*-*-* 19:30:00
RandomizedDelaySec=900
Persistent=true
[Install]
WantedBy=timers.target
UNIT
cat >/etc/systemd/system/qubar-postboot.service <<'UNIT'
[Unit]
Description=Restart qubar once its dependencies are healthy (after reboot)
After=docker.service network-online.target
Wants=network-online.target
[Service]
Type=oneshot
User=deploy
ExecStart=/opt/qubar/scripts/postboot.sh
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now qubar-backup.timer
systemctl enable qubar-postboot.service

echo "bootstrap done. Next: GitHub Actions deploy (or copy deploy/ to $ROOT and run scripts/deploy.sh <tag>)."
