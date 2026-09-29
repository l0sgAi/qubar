#!/usr/bin/env bash
# shellcheck disable=SC1091,SC2016
# PostgreSQL 逻辑备份 → Cloudflare R2（可选 age 加密）。由 systemd timer 每日调用，也可手动执行。
# PG 是唯一真相源：ES 可由 Debezium 重灌，Redis 会话/计数可丢（见部署文档 §备份）。
set -euo pipefail

ROOT="${QUBAR_ROOT:-/opt/qubar}"
set -a; . "$ROOT/.env"; set +a
COMPOSE=(docker compose --env-file "$ROOT/.env" -f "$ROOT/compose/compose.prod.yml")

: "${BACKUP_R2_ENDPOINT:?}" "${BACKUP_R2_BUCKET:?}" "${BACKUP_R2_ACCESS_KEY_ID:?}" "${BACKUP_R2_SECRET_ACCESS_KEY:?}"
RETENTION="${BACKUP_RETENTION_DAYS:-14}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OBJ="pg/qubar-${STAMP}.dump"

RCLONE=(docker run --rm -i
  -e RCLONE_CONFIG_R2_TYPE=s3 -e RCLONE_CONFIG_R2_PROVIDER=Cloudflare
  -e RCLONE_CONFIG_R2_ENDPOINT="$BACKUP_R2_ENDPOINT"
  -e RCLONE_CONFIG_R2_ACCESS_KEY_ID="$BACKUP_R2_ACCESS_KEY_ID"
  -e RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="$BACKUP_R2_SECRET_ACCESS_KEY"
  -e RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true
  rclone/rclone:latest)

dump() { "${COMPOSE[@]}" exec -T postgres pg_dump -U postgres -d qubar -Fc --no-owner; }

if [[ -n "${BACKUP_AGE_RECIPIENT:-}" ]]; then
  command -v age >/dev/null || { echo "age not installed" >&2; exit 1; }
  OBJ="${OBJ}.age"
  dump | age -r "$BACKUP_AGE_RECIPIENT" | "${RCLONE[@]}" rcat "r2:${BACKUP_R2_BUCKET}/${OBJ}"
else
  dump | "${RCLONE[@]}" rcat "r2:${BACKUP_R2_BUCKET}/${OBJ}"
fi

# 校验：对象存在且非空
SIZE="$("${RCLONE[@]}" size --json "r2:${BACKUP_R2_BUCKET}/${OBJ}" | sed -n 's/.*"bytes":\([0-9]*\).*/\1/p')"
[[ "${SIZE:-0}" -gt 1024 ]] || { echo "backup ${OBJ} suspiciously small (${SIZE:-0} bytes)" >&2; exit 1; }
echo "backup ok: ${OBJ} (${SIZE} bytes)"

"${RCLONE[@]}" delete --min-age "${RETENTION}d" "r2:${BACKUP_R2_BUCKET}/pg/" || echo "WARN: retention cleanup failed" >&2
