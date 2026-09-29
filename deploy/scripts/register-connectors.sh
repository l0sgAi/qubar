#!/usr/bin/env bash
# shellcheck disable=SC1091,SC2016
# 幂等地把 deploy/compose/connect/connectors/*.json 注册到 Kafka Connect（PUT /connectors/<name>/config）。
# 模板里的 ${VAR} 用 .env 里的值替换。Connect 没起来 / 没有 json 时静默跳过。
set -euo pipefail

ROOT="${QUBAR_ROOT:-/opt/qubar}"
DIR="$ROOT/compose/connect/connectors"
CONNECT="${CONNECT_URL:-http://127.0.0.1:8083}"

shopt -s nullglob
files=("$DIR"/*.json)
((${#files[@]})) || { echo "no connector definitions in $DIR, skip"; exit 0; }
curl -fsS -m 5 "$CONNECT/connectors" >/dev/null 2>&1 || { echo "kafka connect not reachable at $CONNECT, skip"; exit 0; }

set -a; . "$ROOT/.env"; set +a
command -v envsubst >/dev/null || { echo "envsubst missing (apt install gettext-base)" >&2; exit 1; }

for f in "${files[@]}"; do
  name="$(basename "$f" .json)"
  body="$(envsubst '${PG_DEBEZIUM_PASSWORD}' <"$f")"
  # 文件内容是「config 对象」本身，name 取文件名
  code=$(curl -sS -m 30 -o /tmp/connector-resp.txt -w '%{http_code}' -X PUT \
    -H 'Content-Type: application/json' --data "$body" "$CONNECT/connectors/$name/config")
  if [[ "$code" == 200 || "$code" == 201 ]]; then
    echo "connector $name: ok ($code)"
  else
    echo "connector $name: HTTP $code: $(cat /tmp/connector-resp.txt)" >&2
    exit 1
  fi
done
