#!/usr/bin/env bash
# 服务器端：PG 逻辑备份 → OSS（systemd timer 每日触发，见 Ansible backup 角色）。
# 凭证：ECS 实例 RAM 角色 STS 临时凭证（主机不落 AK）。OSS 侧生命周期按天数过期。
set -euo pipefail
# shellcheck source=/dev/null
source /etc/qubar/backup.env # BACKUP_BUCKET（凭证/region/endpoint 在 /etc/qubar/ossutil.ini）

DIR=/var/backups/qubar
ts=$(date +%Y%m%d-%H%M%S)
file="$DIR/qubar-$ts.dump"
mkdir -p "$DIR"

docker exec qubar-postgres pg_dump -U postgres -d qubar -Fc -Z 6 > "$file.part"
mv "$file.part" "$file"

ossutil -c /etc/qubar/ossutil.ini cp "$file" "oss://$BACKUP_BUCKET/pg/$(date +%Y/%m)/" -f

# 本地只留 3 天，长期副本在 OSS
find "$DIR" -name 'qubar-*.dump' -mtime +3 -delete
echo "[pg-backup] ok: $file ($(du -h "$file" | cut -f1))"
