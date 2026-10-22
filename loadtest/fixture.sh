#!/usr/bin/env bash
# 从 PG 取本批次（RUN_ID）压测内容的 ID，写 data/content.json 供压测场景使用。
# 压测只读写本批次的帖子 / 评论，不触碰真实用户数据。
set -euo pipefail
cd "$(dirname "$0")"

PSQL="${PSQL:-docker exec -i qubar-dev-postgres-1 psql -U postgres -d qubar}"
RUN_ID="$(jq -r .run_id data/users.json)"

$PSQL -At <<SQL >data/content.json
with lt_users as (select id from domains.users where username like 'lt\_${RUN_ID}\_%' and deleted = 0),
p as (select id from domains.post where user_id in (select id from lt_users) and deleted = 0),
c as (select id, root_id from domains.comment where post_id in (select id from p) and deleted = 0)
select json_build_object(
  'post_ids', (select coalesce(json_agg(id), '[]') from p),
  'root_comment_ids', (select coalesce(json_agg(id), '[]') from c where root_id is null),
  'comment_ids', (select coalesce(json_agg(id), '[]') from c)
);
SQL
jq -c '{posts: (.post_ids|length), roots: (.root_comment_ids|length), comments: (.comment_ids|length)}' data/content.json
