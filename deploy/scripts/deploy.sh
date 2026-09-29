#!/usr/bin/env bash
# 服务器上的部署入口（由 GitHub Actions 经 SSH 调用，也可手动执行）。
#
#   /opt/qubar/scripts/deploy.sh <image-tag>          # 例：sha-1a2b3c4
#
# 流程：拉新镜像 → compose up（只重建有变化的服务）→ 轮询 /readyz → 失败自动回滚到上一个 tag。
# 单副本部署有几秒到十几秒的中断（旧进程停 → 新进程起），消费者组会重平衡，SSE 客户端会自动重连。
set -euo pipefail

TAG="${1:?usage: deploy.sh <image-tag>}"
ROOT="${QUBAR_ROOT:-/opt/qubar}"
COMPOSE=(docker compose --env-file "$ROOT/.env" -f "$ROOT/compose/compose.prod.yml")
STATE="$ROOT/.deploy"
READY_URL="http://127.0.0.1:8888/readyz"
READY_TIMEOUT="${READY_TIMEOUT:-120}"

mkdir -p "$STATE"
[[ -f "$ROOT/.env" ]] || { echo "missing $ROOT/.env" >&2; exit 1; }
PREV="$(cat "$STATE/current" 2>/dev/null || true)"

wait_ready() {
  local deadline=$((SECONDS + READY_TIMEOUT))
  while ((SECONDS < deadline)); do
    if curl -fsS -m 3 "$READY_URL" >/dev/null 2>&1; then return 0; fi
    sleep 3
  done
  return 1
}

rollout() {
  export QUBAR_TAG="$1"
  "${COMPOSE[@]}" pull qubar
  # ES / Connect 是本地 build 的镜像（含 IK 插件 / sink 插件），无变化时命中缓存
  "${COMPOSE[@]}" build --pull=false elasticsearch connect
  "${COMPOSE[@]}" up -d --remove-orphans
}

echo "==> deploy $TAG (previous: ${PREV:-none})"
if rollout "$TAG" && wait_ready; then
  echo "$TAG" >"$STATE/current"
  [[ -n "$PREV" && "$PREV" != "$TAG" ]] && echo "$PREV" >"$STATE/previous"
  "$ROOT/scripts/register-connectors.sh" || echo "WARN: connector registration failed (see above)" >&2
  docker image prune -f >/dev/null
  echo "==> ok: $TAG is live"
  exit 0
fi

echo "!! $TAG failed readiness; recent logs:" >&2
"${COMPOSE[@]}" logs --no-color --tail 80 qubar >&2 || true
if [[ -n "$PREV" && "$PREV" != "$TAG" ]]; then
  echo "==> rolling back to $PREV" >&2
  if rollout "$PREV" && wait_ready; then
    echo "==> rollback ok ($PREV live); deploy of $TAG FAILED" >&2
  else
    echo "!! rollback to $PREV also failed — manual intervention needed" >&2
  fi
else
  echo "!! no previous version to roll back to" >&2
fi
exit 1
