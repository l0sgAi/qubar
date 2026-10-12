#!/usr/bin/env bash
# 服务器端：幂等注册 / 更新 Kafka Connect connectors（PUT /connectors/<name>/config）。
#
# 顺序约束：必须在 qubar 首次启动之后执行 —— pg_domains_template（IK 分词 mapping）由应用启动时创建，
# ES sink 若先建出 pg.domains.* 索引，会落成动态 mapping（无 IK、*_id 为 text）。
set -euo pipefail

DIR=/opt/qubar/middleware/connectors
CONNECT=http://127.0.0.1:8083
ES=http://127.0.0.1:9200
log() { echo "[connectors] $*"; }

curl -fs "$ES/_index_template/pg_domains_template" >/dev/null \
  || { log "ES 中没有 pg_domains_template —— 先启动 qubar 再注册 connectors"; exit 1; }

for i in $(seq 1 30); do
  curl -fs "$CONNECT/connectors" >/dev/null && break
  [ "$i" = 30 ] && { log "Kafka Connect REST 未就绪"; exit 1; }
  sleep 2
done

for f in "$DIR"/*.json; do
  name=$(jq -r .name "$f")
  code=$(jq -c .config "$f" | curl -s -o /tmp/connect-resp -w '%{http_code}' -X PUT \
    -H 'Content-Type: application/json' --data-binary @- "$CONNECT/connectors/$name/config")
  case "$code" in
    200 | 201) log "$name ok ($code)" ;;
    *) log "$name 失败 ($code): $(cat /tmp/connect-resp)"; exit 1 ;;
  esac
done
rm -f /tmp/connect-resp

sleep 5
curl -fs "$CONNECT/connectors?expand=status" \
  | jq -r 'to_entries[] | "\(.key)\t\(.value.status.connector.state)\t\([.value.status.tasks[].state] | join(","))"'
