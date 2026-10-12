#!/usr/bin/env bash
# 服务器端：发布 / 回滚 qubar 镜像。由 GitHub Actions 经 SSH 调用，也可手工执行。
#   deploy-app.sh ghcr.io/l0sgai/qubar:<sha>     # 发布指定镜像（失败自动回滚到上一个）
#   deploy-app.sh --rollback                     # 回滚到上一个成功版本
# 单副本：替换期间有数秒不可用；SSE 客户端按 retry_ms 自动重连。
set -euo pipefail

APP_DIR=/opt/qubar/app
cd "$APP_DIR"

compose() { docker compose -f compose.yml --env-file .env --env-file image.env "$@"; }
log() { echo "[deploy-app] $*"; }

[ -f config.yaml ] || { log "缺少 $APP_DIR/config.yaml（Ansible 未下发 QUBAR_CONFIG_YAML）"; exit 1; }

if [ "${1:-}" = "--rollback" ]; then
  target=$(tail -n 2 releases.log 2>/dev/null | head -n 1 || true)
  [ -n "$target" ] || { log "releases.log 没有可回滚的版本"; exit 1; }
else
  target="${1:?用法: deploy-app.sh <image> | --rollback}"
fi

prev=$(sed -n 's/^QUBAR_IMAGE=//p' image.env 2>/dev/null || true)
log "current=${prev:-<none>} target=$target"

# 本地已有（如手工 docker load 的镜像）则拉取失败也继续
docker pull "$target" || docker image inspect "$target" >/dev/null
echo "QUBAR_IMAGE=$target" > image.env

if compose up -d --wait --wait-timeout 180 qubar; then
  [ "$(tail -n 1 releases.log 2>/dev/null || true)" = "$target" ] || echo "$target" >> releases.log
  log "healthy: $target"
  docker image prune -f --filter "until=168h" >/dev/null || true
  exit 0
fi

log "发布失败，最近日志："
docker logs --tail 80 qubar 2>&1 || true
if [ -n "$prev" ] && [ "$prev" != "$target" ]; then
  log "回滚到 $prev"
  echo "QUBAR_IMAGE=$prev" > image.env
  compose up -d --wait --wait-timeout 180 qubar || log "回滚也失败，需人工介入"
fi
exit 1
