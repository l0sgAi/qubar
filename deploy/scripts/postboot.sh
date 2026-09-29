#!/usr/bin/env bash
# 整机重启后：等依赖全部健康，再重启一次应用。
# 原因：Docker 重启容器时不遵守 depends_on；应用只在启动时连 Redpanda / ES，起得比它们早就会降级到下次重启。
set -euo pipefail

ROOT="${QUBAR_ROOT:-/opt/qubar}"
[[ -f "$ROOT/.env" ]] || exit 0
COMPOSE=(docker compose --env-file "$ROOT/.env" -f "$ROOT/compose/compose.prod.yml")
# 没有已部署版本就什么都不做（首次部署由 CI 完成）
[[ -f "$ROOT/.deploy/current" ]] || exit 0
export QUBAR_TAG
QUBAR_TAG="$(cat "$ROOT/.deploy/current")"

deadline=$((SECONDS + 600))
while ((SECONDS < deadline)); do
  unhealthy="$("${COMPOSE[@]}" ps --format '{{.Service}} {{.Health}}' postgres redis redpanda elasticsearch | grep -vc ' healthy$' || true)"
  if [[ "$unhealthy" == "0" ]]; then
    echo "dependencies healthy; restarting qubar"
    "${COMPOSE[@]}" restart qubar
    exit 0
  fi
  sleep 5
done
echo "dependencies not healthy after 10 min; leaving qubar as is" >&2
exit 1
