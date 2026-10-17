#!/usr/bin/env bash
# 本机执行（一次性）：把本机 Homebrew PG 的 qubar.domains 迁到生产容器 PG。
#
#   SSH_TARGET=root@43.107.44.74 SSH_OPTS="-i ~/deployements/aliyun-deployment/ALIYUN.pem" \
#     deploy/scripts/migrate-pg.sh
#   （CF Access 就绪后：SSH_TARGET=deploy@ssh.qubar.site，~/.ssh/config 配好 ProxyCommand）
#
# 行为：pg_dump(-n domains, 不带 owner/ACL) → 上传 → 以 qubar_owner 身份 pg_restore → 重跑 grants.sql
# （补 grants / default privileges / dbz_pub publication）→ 对比行数。
# 安全阀：生产 domains 已有表时拒绝执行，除非 FORCE=1（会 DROP SCHEMA domains 重建！）。
# 必须在注册 CDC connectors 之前执行（Debezium 初始快照会读到完整数据）。
# 之后还要标记迁移基线（migrate force <baseline>），见 deploy/README.md「首次引导」4.1。
set -euo pipefail

SSH_TARGET="${SSH_TARGET:?设置 SSH_TARGET，如 root@43.107.44.74}"
SSH_OPTS="${SSH_OPTS:-}"
LOCAL_DB="${LOCAL_DB:-qubar}"
PG_BIN="${PG_BIN:-/opt/homebrew/opt/postgresql@18/bin}"
dump=$(mktemp -t qubar-domains).dump
remote=/tmp/qubar-domains.dump

# shellcheck disable=SC2086,SC2029  # SSH_OPTS 需分词；远端命令有意在本机展开
rsh() { ssh $SSH_OPTS "$SSH_TARGET" "$@"; }
log() { echo "[migrate-pg] $*"; }

existing=$(rsh "docker exec qubar-postgres psql -U postgres -d qubar -tAc \"select count(*) from pg_tables where schemaname='domains'\"")
if [ "$existing" != "0" ] && [ "${FORCE:-0}" != "1" ]; then
  log "生产 domains 已有 $existing 张表，拒绝覆盖（确需覆盖：FORCE=1）"
  exit 1
fi

log "dump 本机 $LOCAL_DB.domains → $dump"
"$PG_BIN/pg_dump" -d "$LOCAL_DB" -n domains -Fc --no-owner --no-privileges -f "$dump"

log "上传"
# shellcheck disable=SC2086
scp $SSH_OPTS -q "$dump" "$SSH_TARGET:$remote"

log "restore（role=qubar_owner）"
rsh "docker exec -i qubar-postgres pg_restore -U postgres -d qubar --role=qubar_owner \
       --no-owner --no-privileges --clean --if-exists --exit-on-error < $remote && rm -f $remote"

log "重跑 grants.sql（grants / publication）"
rsh "sudo sh -c 'docker exec -i qubar-postgres psql -U postgres -d qubar -v ON_ERROR_STOP=1 -q < /opt/qubar/middleware/pg/grants.sql'"

count_sql="select string_agg(t || '=' || n, ' ' order by t) from (select relname t, n_live_tup n from pg_stat_user_tables where schemaname='domains') s"
rsh "docker exec qubar-postgres psql -U postgres -d qubar -c 'ANALYZE' >/dev/null"
"$PG_BIN/psql" -d "$LOCAL_DB" -c 'ANALYZE' >/dev/null
log "本机: $("$PG_BIN/psql" -d "$LOCAL_DB" -tAc "$count_sql")"
log "生产: $(rsh "docker exec qubar-postgres psql -U postgres -d qubar -tAc \"$count_sql\"")"
rm -f "$dump"
