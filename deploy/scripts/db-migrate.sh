#!/usr/bin/env bash
# 服务器端：执行 schema 迁移（golang-migrate）。由 GitHub Actions release.yml 的 migrate job 经 SSH 调用
# （之前已把仓库 migrations/ 同步到 /opt/qubar/migrate/migrations/），也可手工执行。
#   db-migrate.sh [<git sha>]
#
# 步骤：护栏检查 → migrate up（qubar_owner，PG 不出网，容器走 qubar-net）→ 重跑 grants.sql（新表补授权、
#       新 CDC 表进 dbz_pub）→ 记录到 migrations.log。任一步失败非 0 退出，流水线随之中止、不发布应用。
# 注意：与 migrate-pg.sh（一次性数据搬迁）无关。规则见仓库 migrations/README.md。
set -euo pipefail

DIR=/opt/qubar/migrate
GRANTS=/opt/qubar/middleware/pg/grants.sql
LOG="$DIR/migrations.log"
APPLIED="$DIR/applied.txt" # 已执行（版本 <= 当前版本）的迁移版本清单，用于发现"版本号落后、会被静默跳过"的新文件
URL='postgres://qubar_owner@postgres:5432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1'
SHA="${1:-manual}"

cd "$DIR"
log() { echo "[db-migrate] $*"; }
record() { echo "$(date '+%Y-%m-%dT%H:%M:%S%z') sha=$SHA $*" >> "$LOG"; }
psql_q() { docker exec -i qubar-postgres psql -U postgres -d qubar -v ON_ERROR_STOP=1 -tAq "$@"; }
migrate() {
  docker run --rm --network qubar-net --env-file .env -v "$DIR/migrations:/migrations:ro" \
    "$MIGRATE_IMAGE" -path /migrations -database "$URL" "$@"
}

MIGRATE_IMAGE=$(sed -n 's/^MIGRATE_IMAGE=//p' .env)
[ -n "$MIGRATE_IMAGE" ] || { log "$DIR/.env 缺少 MIGRATE_IMAGE（Ansible 未运行？）"; exit 1; }

# ---------- 护栏 ----------
has_table=$(psql_q -c "select to_regclass('public.schema_migrations') is not null")
n_tables=$(psql_q -c "select count(*) from pg_tables where schemaname = 'domains'")
if [ "$has_table" != t ] && [ "$n_tables" != 0 ]; then
  log "public.schema_migrations 不存在，但 domains 已有 $n_tables 张表：存量库须先标记基线，拒绝执行（否则会回放基线）"
  log "处理：migrate ... force <baseline 版本>，见 migrations/README.md「基线」"
  record "refused: no schema_migrations, domains has $n_tables tables"
  exit 1
fi

current=0
if [ "$has_table" = t ]; then
  row=$(psql_q -c "select version || ' ' || dirty from public.schema_migrations limit 1")
  if [ -n "$row" ]; then
    current=${row% *}
    if [ "${row#* }" = true ]; then
      log "当前版本 $current 为 dirty（上次迁移失败），需人工修复后再执行，见 migrations/README.md「失败与 dirty 修复」"
      record "refused: dirty at $current"
      exit 1
    fi
  fi
fi

versions() { for f in migrations/*.up.sql; do [ -e "$f" ] || continue; f=${f##*/}; echo "${f%%_*}"; done | sort; }
upto() { versions | awk -v v="$1" '$1 <= v'; }

if [ -f "$APPLIED" ]; then
  stale=$(comm -23 <(upto "$current") <(sort "$APPLIED"))
  if [ -n "$stale" ]; then
    log "以下迁移的版本号 <= 当前版本 $current 但从未执行过，golang-migrate 会静默跳过它们："
    while read -r v; do echo "  $v"; done <<< "$stale"
    log "处理：把这些文件重命名为更大的版本号（make migrate-new）后重新发布"
    record "refused: stale versions $(echo "$stale" | tr '\n' ' ')"
    exit 1
  fi
else
  upto "$current" > "$APPLIED" # 首次运行：信任当前状态
fi

# ---------- 执行 ----------
log "current=$current pending=$(versions | awk -v v="$current" '$1 > v' | awk 'END { print NR }')"
if ! migrate up; then
  after=$(psql_q -c "select version || ' dirty=' || dirty from public.schema_migrations limit 1" || true)
  record "FAILED from=$current now=${after:-?}"
  log "迁移失败（now=${after:-?}），应用不会发布"
  exit 1
fi

psql_q < "$GRANTS"

after=$(psql_q -c "select version from public.schema_migrations where not dirty limit 1")
upto "$after" > "$APPLIED"
record "ok $current -> $after"
log "ok: $current -> $after"
