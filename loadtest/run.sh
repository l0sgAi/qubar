#!/usr/bin/env bash
# 在服务器上跑一轮压测：k6 容器加入 qubar-net 直打 qubar:8888（源站层，不经 Cloudflare），
# --cpus 限制压测机自身占用；同时后台跑 monitor.sh 采样，结果落 results/<场景>-<profile>-<时间>/。
# 用法：SCENARIO=read PROFILE=stress RATE=400 ./run.sh
# 其余 k6 参数（STEPS / STEP_DURATION / DURATION / MAX_VUS / SSE_VUS / HOLD ...）同名环境变量透传。
set -euo pipefail
cd "$(dirname "$0")"

SCENARIO="${SCENARIO:-read}"
PROFILE="${PROFILE:-smoke}"
K6_IMAGE="${K6_IMAGE:-grafana/k6:latest}"
K6_CPUS="${K6_CPUS:-1}"
TAG="${SCENARIO}-${PROFILE}-$(date +%H%M%S)"
OUT="results/$TAG"
mkdir -p "$OUT"

OUT="$OUT" ./monitor.sh >/dev/null 2>&1 &
mon=$!
trap 'kill $mon 2>/dev/null || true' EXIT

envs=()
for v in ENDPOINT ANON SCENARIO PROFILE RATE STEPS STEP_DURATION DURATION MAX_VUS SSE_VUS RAMP HOLD; do
  [ -n "${!v:-}" ] && envs+=(-e "$v=${!v}")
done

# 表级访问快照（无 pg_stat_statements 时定位热点表）：前后两份 diff 即本轮的扫描 / 写入量
pgsnap() {
  docker exec -i "${PG_CONTAINER:-qubar-postgres}" psql -U postgres -d qubar -AtF, -c \
    "select relname, seq_scan, seq_tup_read, coalesce(idx_scan, 0), n_tup_ins, n_tup_upd, n_tup_del from pg_stat_user_tables" >"$1" || true
}
pgsnap "$OUT/pg_tables_before.csv"

echo "== $TAG 开始 $(date '+%F %T')"
docker run --rm --name "qubar-k6-$TAG" --network qubar-net --cpus="$K6_CPUS" \
  -u "$(id -u):$(id -g)" -v "$PWD:/lt" -w /lt "$K6_IMAGE" run \
  --summary-export "$OUT/summary.json" --out "csv=$OUT/k6.csv.gz" \
  -e BASE_URL="${BASE_URL:-http://qubar:8888}" "${envs[@]}" scenarios.js | tee "$OUT/k6.log" || true
pgsnap "$OUT/pg_tables_after.csv"
echo "== $TAG 结束 $(date '+%F %T')，结果 $OUT"
