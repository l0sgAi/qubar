-- =============================================================================
-- #46 点赞/收藏链路修复 · 补齐唯一索引（仅当 01_preflight.sql 第 1 段非 OK 时执行）
--
-- 新代码的 INSERT … ON CONFLICT (user_id, post_id|comment_id) 依赖以下唯一索引，
-- 与 docs/pgsql-ddl/ 定义一致；缺失时点赞/收藏落库直接报错：
--   domains.post_like    uk_post_like_user_post        (user_id, post_id)
--   domains.comment_like uk_comment_like_user_comment  (user_id, comment_id)
--   domains.post_collect uk_post_collect_user_post     (user_id, post_id)
--
-- 用法（DB-owner，**不要**包在事务里：CREATE/DROP INDEX CONCURRENTLY 不能在事务块内执行）：
--   psql "$PG_URI" -f 02_fix_unique_indexes.sql
--
-- 每张表：已有有效索引 → 跳过；有失败残留的 INVALID 索引 → DROP CONCURRENTLY；
--        有重复行 → 先整行备份到 ops.dedupe_backup_<table> 再删除多余行 → CREATE UNIQUE INDEX CONCURRENTLY。
-- 去重保留规则：有效行(deleted=0)优先 → update_time 最新 → id 最大。
-- 幂等：可重复执行；若建索引期间有并发写入产生新重复导致失败，直接重跑即可。
-- 应在**部署新代码之前**执行。
-- =============================================================================
\set ON_ERROR_STOP on
CREATE SCHEMA IF NOT EXISTS ops;

-- ---------------------------------------------------------------------------
-- domains.post_like (user_id, post_id)
-- ---------------------------------------------------------------------------
\echo '== domains.post_like'
WITH idx AS (
    SELECT i.indexrelid::regclass::text AS name, i.indisvalid AS valid, i.indpred IS NULL AS full_idx
    FROM pg_index i
    WHERE i.indrelid = 'domains.post_like'::regclass AND i.indisunique
      AND ARRAY(SELECT a.attname::text FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(n, o)
                JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n ORDER BY k.o)
          = ARRAY['user_id', 'post_id']
)
SELECT COALESCE(bool_or(valid AND full_idx), false) AS ok,
       COALESCE(max(name) FILTER (WHERE NOT valid), '') AS invalid_name,
       to_regclass('domains.uk_post_like_user_post') IS NOT NULL
           AND NOT COALESCE(bool_or(name = 'domains.uk_post_like_user_post' AND NOT valid), false) AS name_taken
FROM idx \gset pl_
\if :pl_ok
  \echo 'OK: valid unique index already present, skip'
\else
  \if :pl_name_taken
    \echo 'ABORT: domains.uk_post_like_user_post exists but is not a full unique index on (user_id, post_id); inspect manually'
    \quit
  \endif
  SELECT :'pl_invalid_name' <> '' AS has_invalid \gset pl_
  \if :pl_has_invalid
    \echo 'dropping INVALID index' :pl_invalid_name
    DROP INDEX CONCURRENTLY IF EXISTS :pl_invalid_name;
  \endif
  CREATE TABLE IF NOT EXISTS ops.dedupe_backup_post_like (LIKE domains.post_like);
  WITH ranked AS (
      SELECT id, row_number() OVER (PARTITION BY user_id, post_id
                                    ORDER BY deleted ASC, update_time DESC, id DESC) AS rn
      FROM domains.post_like
  ),
  doomed AS (
      DELETE FROM domains.post_like t USING ranked r
      WHERE t.id = r.id AND r.rn > 1
      RETURNING t.*
  )
  INSERT INTO ops.dedupe_backup_post_like SELECT * FROM doomed;
  CREATE UNIQUE INDEX CONCURRENTLY uk_post_like_user_post ON domains.post_like (user_id, post_id);
  \echo 'CREATED: domains.uk_post_like_user_post'
\endif

-- ---------------------------------------------------------------------------
-- domains.comment_like (user_id, comment_id)
-- ---------------------------------------------------------------------------
\echo '== domains.comment_like'
WITH idx AS (
    SELECT i.indexrelid::regclass::text AS name, i.indisvalid AS valid, i.indpred IS NULL AS full_idx
    FROM pg_index i
    WHERE i.indrelid = 'domains.comment_like'::regclass AND i.indisunique
      AND ARRAY(SELECT a.attname::text FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(n, o)
                JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n ORDER BY k.o)
          = ARRAY['user_id', 'comment_id']
)
SELECT COALESCE(bool_or(valid AND full_idx), false) AS ok,
       COALESCE(max(name) FILTER (WHERE NOT valid), '') AS invalid_name,
       to_regclass('domains.uk_comment_like_user_comment') IS NOT NULL
           AND NOT COALESCE(bool_or(name = 'domains.uk_comment_like_user_comment' AND NOT valid), false) AS name_taken
FROM idx \gset cl_
\if :cl_ok
  \echo 'OK: valid unique index already present, skip'
\else
  \if :cl_name_taken
    \echo 'ABORT: domains.uk_comment_like_user_comment exists but is not a full unique index on (user_id, comment_id); inspect manually'
    \quit
  \endif
  SELECT :'cl_invalid_name' <> '' AS has_invalid \gset cl_
  \if :cl_has_invalid
    \echo 'dropping INVALID index' :cl_invalid_name
    DROP INDEX CONCURRENTLY IF EXISTS :cl_invalid_name;
  \endif
  CREATE TABLE IF NOT EXISTS ops.dedupe_backup_comment_like (LIKE domains.comment_like);
  WITH ranked AS (
      SELECT id, row_number() OVER (PARTITION BY user_id, comment_id
                                    ORDER BY deleted ASC, update_time DESC, id DESC) AS rn
      FROM domains.comment_like
  ),
  doomed AS (
      DELETE FROM domains.comment_like t USING ranked r
      WHERE t.id = r.id AND r.rn > 1
      RETURNING t.*
  )
  INSERT INTO ops.dedupe_backup_comment_like SELECT * FROM doomed;
  CREATE UNIQUE INDEX CONCURRENTLY uk_comment_like_user_comment ON domains.comment_like (user_id, comment_id);
  \echo 'CREATED: domains.uk_comment_like_user_comment'
\endif

-- ---------------------------------------------------------------------------
-- domains.post_collect (user_id, post_id)
-- ---------------------------------------------------------------------------
\echo '== domains.post_collect'
WITH idx AS (
    SELECT i.indexrelid::regclass::text AS name, i.indisvalid AS valid, i.indpred IS NULL AS full_idx
    FROM pg_index i
    WHERE i.indrelid = 'domains.post_collect'::regclass AND i.indisunique
      AND ARRAY(SELECT a.attname::text FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(n, o)
                JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.n ORDER BY k.o)
          = ARRAY['user_id', 'post_id']
)
SELECT COALESCE(bool_or(valid AND full_idx), false) AS ok,
       COALESCE(max(name) FILTER (WHERE NOT valid), '') AS invalid_name,
       to_regclass('domains.uk_post_collect_user_post') IS NOT NULL
           AND NOT COALESCE(bool_or(name = 'domains.uk_post_collect_user_post' AND NOT valid), false) AS name_taken
FROM idx \gset pc_
\if :pc_ok
  \echo 'OK: valid unique index already present, skip'
\else
  \if :pc_name_taken
    \echo 'ABORT: domains.uk_post_collect_user_post exists but is not a full unique index on (user_id, post_id); inspect manually'
    \quit
  \endif
  SELECT :'pc_invalid_name' <> '' AS has_invalid \gset pc_
  \if :pc_has_invalid
    \echo 'dropping INVALID index' :pc_invalid_name
    DROP INDEX CONCURRENTLY IF EXISTS :pc_invalid_name;
  \endif
  CREATE TABLE IF NOT EXISTS ops.dedupe_backup_post_collect (LIKE domains.post_collect);
  WITH ranked AS (
      SELECT id, row_number() OVER (PARTITION BY user_id, post_id
                                    ORDER BY deleted ASC, update_time DESC, id DESC) AS rn
      FROM domains.post_collect
  ),
  doomed AS (
      DELETE FROM domains.post_collect t USING ranked r
      WHERE t.id = r.id AND r.rn > 1
      RETURNING t.*
  )
  INSERT INTO ops.dedupe_backup_post_collect SELECT * FROM doomed;
  CREATE UNIQUE INDEX CONCURRENTLY uk_post_collect_user_post ON domains.post_collect (user_id, post_id);
  \echo 'CREATED: domains.uk_post_collect_user_post'
\endif

\echo '== done. Re-run 01_preflight.sql: section 1 should be all OK.'
