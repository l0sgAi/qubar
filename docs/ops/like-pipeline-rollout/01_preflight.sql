-- =============================================================================
-- #46 点赞/收藏链路修复 · 上线前检查（只读，可随时重复执行）
--
-- 用法（任意有读权限的角色；建议 DB-owner）：
--   psql "$PG_URI" -v app_role=qubar_web_app -f 01_preflight.sql
--
-- 检查项：
--   1. ON CONFLICT 依赖的唯一索引：存在 + 有效(indisvalid) + 非部分索引 + 列完全匹配
--   2. (user, target) 重复行（若唯一索引缺失，建索引前必须先去重）
--   3. 运行时角色权限（新 SQL 需要的最小集合）
--   4. 计数漂移预估（仅统计，不修改；详细报告见 03_reconcile_counts.sql 干跑）
--
-- 结论看每段的 status 列：全部 OK 即可上线；否则按 README 处理。
-- =============================================================================
\set ON_ERROR_STOP on
\if :{?app_role}
\else
  \set app_role qubar_web_app
\endif

\echo '== 1. 唯一索引（应用 ON CONFLICT 依赖，缺失则 flush 报错、点赞无法落库）'
WITH required(tbl, cols) AS (
    VALUES ('domains.post_like',    ARRAY['user_id', 'post_id']),
           ('domains.comment_like', ARRAY['user_id', 'comment_id']),
           ('domains.post_collect', ARRAY['user_id', 'post_id'])
),
idx AS (
    SELECT i.indrelid::regclass::text AS tbl,
           i.indexrelid::regclass::text AS index_name,
           i.indisvalid,
           i.indpred IS NULL AS is_full,
           ARRAY(SELECT a.attname::text
                 FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(attnum, ord)
                 JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
                 ORDER BY k.ord) AS cols
    FROM pg_index i
    WHERE i.indisunique
)
SELECT r.tbl,
       array_to_string(r.cols, ', ') AS required_columns,
       COALESCE(string_agg(idx.index_name, ', '), '-') AS found_index,
       CASE
           WHEN bool_or(idx.indisvalid AND idx.is_full) THEN 'OK'
           WHEN bool_or(idx.is_full AND NOT idx.indisvalid) THEN 'INVALID -> run 02_fix_unique_indexes.sql'
           WHEN bool_or(NOT idx.is_full) THEN 'PARTIAL ONLY -> run 02_fix_unique_indexes.sql'
           ELSE 'MISSING -> run 02_fix_unique_indexes.sql'
       END AS status
FROM required r
LEFT JOIN idx ON idx.tbl = r.tbl AND idx.cols = r.cols
GROUP BY r.tbl, r.cols
ORDER BY r.tbl;

\echo '== 2. 重复 (user, target) 组（应为 0；非 0 且索引缺失时 02 会先备份再去重）'
SELECT 'domains.post_like' AS tbl, count(*) AS duplicate_groups,
       CASE WHEN count(*) = 0 THEN 'OK' ELSE 'DUPLICATES' END AS status
FROM (SELECT 1 FROM domains.post_like GROUP BY user_id, post_id HAVING count(*) > 1) d
UNION ALL
SELECT 'domains.comment_like', count(*),
       CASE WHEN count(*) = 0 THEN 'OK' ELSE 'DUPLICATES' END
FROM (SELECT 1 FROM domains.comment_like GROUP BY user_id, comment_id HAVING count(*) > 1) d
UNION ALL
SELECT 'domains.post_collect', count(*),
       CASE WHEN count(*) = 0 THEN 'OK' ELSE 'DUPLICATES' END
FROM (SELECT 1 FROM domains.post_collect GROUP BY user_id, post_id HAVING count(*) > 1) d;

\echo '== 3. 运行时角色权限，role =' :app_role
SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app_role') AS app_role_exists \gset
\if :app_role_exists
WITH required(tbl, priv) AS (
    VALUES ('domains.post_like', 'SELECT'), ('domains.post_like', 'INSERT'), ('domains.post_like', 'UPDATE'),
           ('domains.comment_like', 'SELECT'), ('domains.comment_like', 'INSERT'), ('domains.comment_like', 'UPDATE'),
           ('domains.post_collect', 'SELECT'), ('domains.post_collect', 'INSERT'), ('domains.post_collect', 'UPDATE'),
           ('domains.post', 'SELECT'), ('domains.post', 'UPDATE'),
           ('domains.comment', 'SELECT'), ('domains.comment', 'UPDATE')
)
SELECT tbl, priv,
       CASE WHEN has_table_privilege(:'app_role', tbl, priv) THEN 'OK'
            ELSE 'MISSING -> GRANT ' || priv || ' ON ' || tbl || ' TO ' || :'app_role' END AS status
FROM required
ORDER BY has_table_privilege(:'app_role', tbl, priv), tbl, priv;  -- MISSING 在前
\else
\echo 'WARN: role' :app_role 'not found; pass -v app_role=<runtime role> (configs pgsql.username)'
\endif

\echo '== 4. 计数漂移预估（仅统计；修正见 03_reconcile_counts.sql）'
WITH pl AS (SELECT post_id, count(*) AS cnt FROM domains.post_like WHERE deleted = 0 GROUP BY post_id),
     cl AS (SELECT comment_id, count(*) AS cnt FROM domains.comment_like WHERE deleted = 0 GROUP BY comment_id),
     pc AS (SELECT post_id, count(*) AS cnt FROM domains.post_collect WHERE deleted = 0 GROUP BY post_id)
SELECT 'post.like_count' AS counter,
       count(*) FILTER (WHERE p.like_count <> COALESCE(pl.cnt, 0)) AS rows_off,
       COALESCE(sum(p.like_count - COALESCE(pl.cnt, 0)), 0) AS net_excess
FROM domains.post p LEFT JOIN pl ON pl.post_id = p.id WHERE p.deleted = 0
UNION ALL
SELECT 'comment.like_count',
       count(*) FILTER (WHERE c.like_count <> COALESCE(cl.cnt, 0)),
       COALESCE(sum(c.like_count - COALESCE(cl.cnt, 0)), 0)
FROM domains.comment c LEFT JOIN cl ON cl.comment_id = c.id WHERE c.deleted = 0
UNION ALL
SELECT 'post.collect_count',
       count(*) FILTER (WHERE p.collect_count <> COALESCE(pc.cnt, 0)),
       COALESCE(sum(p.collect_count - COALESCE(pc.cnt, 0)), 0)
FROM domains.post p LEFT JOIN pc ON pc.post_id = p.id WHERE p.deleted = 0;
