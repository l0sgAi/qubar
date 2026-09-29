#!/usr/bin/env bash
# 把一份 pg_dump -Fc 文件恢复进 compose 里的 PostgreSQL（首次迁移 / 灾备演练）。
#
#   # 首次迁移：在旧机器（Mac）上
#   pg_dump -h 127.0.0.1 -U <owner> -d qubar -n domains -Fc --no-owner -f qubar.dump
#   scp qubar.dump deploy@<host>:/opt/qubar/     # 经 Cloudflare Access 的 ssh
#   # 服务器上
#   /opt/qubar/scripts/restore.sh /opt/qubar/qubar.dump
#
# 只恢复 domains schema；--no-owner + --role=qubar_owner 让表归 owner，默认权限（见 initdb）自动给 app/debezium 授权。
# 目标库里已有 domains 表时拒绝执行（DDL 文档里有 DROP TABLE，别在有数据的库上误跑）。
set -euo pipefail

DUMP="${1:?usage: restore.sh <file.dump>}"
ROOT="${QUBAR_ROOT:-/opt/qubar}"
COMPOSE=(docker compose --env-file "$ROOT/.env" -f "$ROOT/compose/compose.prod.yml")

[[ -s "$DUMP" ]] || { echo "no such dump: $DUMP" >&2; exit 1; }
EXIST="$("${COMPOSE[@]}" exec -T postgres psql -U postgres -d qubar -Atc \
  "select count(*) from information_schema.tables where table_schema='domains'")"
if [[ "$EXIST" != "0" && "${FORCE:-}" != "1" ]]; then
  echo "domains schema already has $EXIST tables; refusing (set FORCE=1 to override)" >&2
  exit 1
fi

echo "==> stopping app so nothing writes during restore"
"${COMPOSE[@]}" stop qubar || true
"${COMPOSE[@]}" exec -T postgres pg_restore -U postgres -d qubar \
  --no-owner --role=qubar_owner --schema=domains --exit-on-error <"$DUMP"
echo "==> restore done. Start the app:  scripts/deploy.sh \$(cat $ROOT/.deploy/current)"
echo "    Then (re)register connectors so Debezium snapshots into Elasticsearch: scripts/register-connectors.sh"
