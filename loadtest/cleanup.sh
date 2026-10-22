#!/usr/bin/env bash
# 清理一批压测数据（按 data/users.json 的 RUN_ID）：
#   PG    帖子 / 评论 / 点赞 / 收藏 / 通知 / 圈子 / 账号逻辑删除（deleted=1，经 CDC 同步到 ES 后搜索不可见）；
#         无 deleted 列的派生表（post_interaction / post_view_history）删除本批次账号的行，避免进入 CF 召回
#   Redis 本批次账号的 token、会话、浏览记录、推荐 / 发现池等按用户 ID 命名的 key
# 只影响 lt_<RUN_ID>_* 账号及其内容。默认只打印将影响的行数，APPLY=1 才执行。
set -euo pipefail
cd "$(dirname "$0")"

PSQL="${PSQL:-docker exec -i qubar-dev-postgres-1 psql -U postgres -d qubar}"
REDIS_CLI="${REDIS_CLI:-docker exec -i qubar-dev-redis-1 redis-cli}"
RUN_ID="${RUN_ID:-$(jq -r .run_id data/users.json)}"
APPLY="${APPLY:-0}"

read -r -d '' SCOPE <<SQL || true
create temp table lt_u as select id from domains.users where username like 'lt\_${RUN_ID}\_%';
create temp table lt_p as select id from domains.post where user_id in (select id from lt_u);
SQL

echo "== RUN_ID=$RUN_ID 影响范围"
$PSQL -At <<SQL
$SCOPE
select 'users', count(*) from domains.users where id in (select id from lt_u) and deleted = 0
union all select 'posts', count(*) from domains.post where id in (select id from lt_p) and deleted = 0
union all select 'comments', count(*) from domains.comment where (user_id in (select id from lt_u) or post_id in (select id from lt_p)) and deleted = 0
union all select 'post_like', count(*) from domains.post_like where user_id in (select id from lt_u) and deleted = 0
union all select 'comment_like', count(*) from domains.comment_like where user_id in (select id from lt_u) and deleted = 0
union all select 'post_collect', count(*) from domains.post_collect where user_id in (select id from lt_u) and deleted = 0
union all select 'notification', count(*) from domains.notification where (recipient_id in (select id from lt_u) or actor_id in (select id from lt_u)) and deleted = 0
union all select 'post_interaction', count(*) from domains.post_interaction where user_id in (select id from lt_u) or post_id in (select id from lt_p)
union all select 'post_view_history', count(*) from domains.post_view_history where user_id in (select id from lt_u)
union all select 'circle', count(*) from domains.circle where slug = 'lt-${RUN_ID}' and deleted = 0
-- 越界检查：压测账号对非压测内容的互动（应为 0）
union all select '越界:like非lt帖', count(*) from domains.post_like where user_id in (select id from lt_u) and post_id not in (select id from lt_p)
union all select '越界:评论非lt帖', count(*) from domains.comment where user_id in (select id from lt_u) and post_id not in (select id from lt_p);
SQL

if [ "$APPLY" != 1 ]; then
  echo "预览完成，APPLY=1 执行清理"
  exit 0
fi

echo "== 清理 PG"
$PSQL -v ON_ERROR_STOP=1 <<SQL
begin;
$SCOPE
update domains.comment set deleted = 1, update_time = now() where (user_id in (select id from lt_u) or post_id in (select id from lt_p)) and deleted = 0;
update domains.post set deleted = 1, update_time = now() where id in (select id from lt_p) and deleted = 0;
update domains.post_like set deleted = 1, update_time = now() where user_id in (select id from lt_u) and deleted = 0;
update domains.comment_like set deleted = 1, update_time = now() where user_id in (select id from lt_u) and deleted = 0;
update domains.post_collect set deleted = 1, update_time = now() where user_id in (select id from lt_u) and deleted = 0;
update domains.notification set deleted = 1, update_time = now() where (recipient_id in (select id from lt_u) or actor_id in (select id from lt_u)) and deleted = 0;
delete from domains.post_interaction where user_id in (select id from lt_u) or post_id in (select id from lt_p);
delete from domains.post_view_history where user_id in (select id from lt_u);
update domains.circle set deleted = 1, update_time = now() where slug = 'lt-${RUN_ID}' and deleted = 0;
update domains.users set deleted = 1, update_time = now() where id in (select id from lt_u) and deleted = 0;
commit;
SQL

echo "== 清理 Redis"
n=0
while read -r uid tok; do
  # </dev/null：docker exec -i 会吞掉循环的 stdin
  keys="$($REDIS_CLI --scan --pattern "*${uid}*" </dev/null | tr '\n' ' ')"
  # shellcheck disable=SC2086
  $REDIS_CLI DEL "satoken:token:$tok" $keys </dev/null >/dev/null
  n=$((n + 1))
done < <(jq -r '.users[] | "\(.id) \(.token)"' data/users.json)
echo "已清理 $n 个账号的 Redis key"
