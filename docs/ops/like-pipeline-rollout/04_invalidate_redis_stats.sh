#!/usr/bin/env bash
# =============================================================================
# #46 点赞/收藏链路修复 · 对账后清理 Redis 统计缓存
#
# 03_reconcile_counts.sql 校正了 DB 计数，但 Redis 的 post:stats:{id} / comment:stats:{id}
# 仍是旧（漂移后的）值，且热门帖每次点赞都会续期 TTL，可能长期不过期。
# 本脚本按对账日志删除被校正对象的统计 Hash；读/写路径下次访问时 RestoreStats 从 DB 重新加载。
#
# 代价：被删 Hash 中尚未落库的 view_count 增量（≤ 一个 post_statistics flush 周期）只在展示上短暂偏低，
#       DB 最终值不受影响。
#
# 用法：
#   RUN_ID=<03 输出的 run_id> \
#   PG_URI="postgres://owner@host:5432/qubar" \
#   REDIS_CLI_ARGS="-h redis-host -p 6379 -n 0 --user default --pass xxx" \
#   ./04_invalidate_redis_stats.sh
#
#   DRY_RUN=1 ...  只打印将删除的 key 数量与样例，不执行删除。
# 依赖：psql、redis-cli、xargs。
# =============================================================================
set -euo pipefail

: "${RUN_ID:?set RUN_ID to the run_id printed by 03_reconcile_counts.sql}"
DRY_RUN="${DRY_RUN:-0}"
BATCH="${BATCH:-500}"

read -r -a REDIS_ARGS <<< "${REDIS_CLI_ARGS:-}"
PSQL=(psql -X -At -v ON_ERROR_STOP=1 -v run_id="$RUN_ID")
if [[ -n "${PG_URI:-}" ]]; then
  PSQL+=("$PG_URI")
fi

keys_file="$(mktemp)"
trap 'rm -f "$keys_file"' EXIT

"${PSQL[@]}" > "$keys_file" <<'SQL'
SELECT DISTINCT
       CASE WHEN entity LIKE 'comment.%' THEN 'comment:stats:' ELSE 'post:stats:' END || id::text
FROM ops.count_reconcile_log
WHERE run_id = :'run_id'::uuid
ORDER BY 1;
SQL

total="$(wc -l < "$keys_file" | tr -d ' ')"
echo "run_id=$RUN_ID keys_to_invalidate=$total"
if [[ "$total" -eq 0 ]]; then
  echo "nothing to do"
  exit 0
fi

if [[ "$DRY_RUN" == "1" ]]; then
  echo "DRY_RUN=1, sample:"
  head -n 5 "$keys_file"
  exit 0
fi

# 每批 BATCH 个 key 一次 DEL；redis-cli 输出每批实际删除数（不存在的 key 计 0）。
deleted="$(xargs -n "$BATCH" redis-cli "${REDIS_ARGS[@]}" DEL < "$keys_file" | awk '{s += $1} END {print s + 0}')"
echo "deleted=$deleted (keys that were cached; the rest were already expired)"
