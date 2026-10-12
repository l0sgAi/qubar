#!/usr/bin/env bash
# 服务器端：PG 逻辑备份 → Cloudflare R2（systemd timer 每日触发，见 Ansible backup 角色）。
# R2 凭证在 /etc/qubar/rclone.conf（0600）；不存在时只保留本地副本。R2 侧过期规则见 terraform/bootstrap。
set -euo pipefail
# shellcheck source=/dev/null
source /etc/qubar/backup.env # BACKUP_BUCKET

DIR=/var/backups/qubar
RCLONE_CONF=/etc/qubar/rclone.conf
ts=$(date +%Y%m%d-%H%M%S)
file="$DIR/qubar-$ts.dump"
mkdir -p "$DIR"

docker exec qubar-postgres pg_dump -U postgres -d qubar -Fc -Z 6 > "$file.part"
mv "$file.part" "$file"

if [ -f "$RCLONE_CONF" ]; then
  rclone --config "$RCLONE_CONF" copy "$file" "r2:$BACKUP_BUCKET/pg/$(date +%Y/%m)/"
  echo "[pg-backup] uploaded to r2:$BACKUP_BUCKET"
else
  echo "[pg-backup] WARN: 无 $RCLONE_CONF，仅本地备份"
fi

# 本地只留 3 天，长期副本在 R2
find "$DIR" -name 'qubar-*.dump' -mtime +3 -delete
echo "[pg-backup] ok: $file ($(du -h "$file" | cut -f1))"
