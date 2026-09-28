-- =============================================================================
-- #46 点赞/收藏链路修复 · 历史计数对账
--
-- 修复前的 bug 让 post.like_count / comment.like_count / post.collect_count 只增不减地漂移。
-- 新代码只阻止新增漂移；本脚本把存量计数校正为流水表中有效行（deleted = 0）的数量。
--
-- 何时执行：新代码已上线，且 like_events / collect_events 消费组 lag 为 0 之后，业务低峰。
--
-- 用法（DB-owner）：
--   psql "$PG_URI" -f 03_reconcile_counts.sql                          # 干跑：只出报告，不改数据
--   psql "$PG_URI" -v apply=1 -f 03_reconcile_counts.sql               # 执行校正
--   psql "$PG_URI" -v apply=1 -v batch_size=1000 -v sleep_ms=200 -f 03_reconcile_counts.sql
--
-- 执行方式与安全性：
--   - 按主键 id 升序分批（默认 2000 行/批），每批独立提交，批间 sleep（默认 100ms）限流；
--   - 每批先 SELECT … FOR UPDATE 锁住本批目标行，再在新快照中计数并更新：
--     与线上消费者（同样要更新这些行）串行化，不会覆盖掉并发的 +1/−1；
--   - 只更新确实不一致的行（同时 bump update_time → CDC 同步 ES），一致的行不动；
--   - 每一处修改记入 ops.count_reconcile_log(entity, id, old_value, new_value, run_id)，可审计 / 可回滚；
--   - 幂等：中途失败（如锁等待超时）直接重跑，已校正的行不会再变。
-- 完成后执行 04_invalidate_redis_stats.sh 清掉对应 Redis 统计缓存，让读路径从 DB 重新加载。
-- =============================================================================
\set ON_ERROR_STOP on
\if :{?apply}
\else
  \set apply 0
\endif
\if :{?batch_size}
\else
  \set batch_size 2000
\endif
\if :{?sleep_ms}
\else
  \set sleep_ms 100
\endif

\echo '== 漂移报告（执行前）'
WITH pl AS (SELECT post_id, count(*) AS cnt FROM domains.post_like WHERE deleted = 0 GROUP BY post_id),
     cl AS (SELECT comment_id, count(*) AS cnt FROM domains.comment_like WHERE deleted = 0 GROUP BY comment_id),
     pc AS (SELECT post_id, count(*) AS cnt FROM domains.post_collect WHERE deleted = 0 GROUP BY post_id)
SELECT 'post.like_count' AS counter,
       count(*) FILTER (WHERE p.like_count <> COALESCE(pl.cnt, 0)) AS rows_off,
       COALESCE(sum(p.like_count - COALESCE(pl.cnt, 0)), 0) AS net_excess,
       COALESCE(max(abs(p.like_count - COALESCE(pl.cnt, 0))), 0) AS max_abs_diff
FROM domains.post p LEFT JOIN pl ON pl.post_id = p.id WHERE p.deleted = 0
UNION ALL
SELECT 'comment.like_count',
       count(*) FILTER (WHERE c.like_count <> COALESCE(cl.cnt, 0)),
       COALESCE(sum(c.like_count - COALESCE(cl.cnt, 0)), 0),
       COALESCE(max(abs(c.like_count - COALESCE(cl.cnt, 0))), 0)
FROM domains.comment c LEFT JOIN cl ON cl.comment_id = c.id WHERE c.deleted = 0
UNION ALL
SELECT 'post.collect_count',
       count(*) FILTER (WHERE p.collect_count <> COALESCE(pc.cnt, 0)),
       COALESCE(sum(p.collect_count - COALESCE(pc.cnt, 0)), 0),
       COALESCE(max(abs(p.collect_count - COALESCE(pc.cnt, 0))), 0)
FROM domains.post p LEFT JOIN pc ON pc.post_id = p.id WHERE p.deleted = 0;

\echo '== 偏差最大的帖子（like_count，前 20）'
WITH pl AS (SELECT post_id, count(*) AS cnt FROM domains.post_like WHERE deleted = 0 GROUP BY post_id)
SELECT p.id, p.like_count AS current, COALESCE(pl.cnt, 0) AS actual, p.like_count - COALESCE(pl.cnt, 0) AS diff
FROM domains.post p LEFT JOIN pl ON pl.post_id = p.id
WHERE p.deleted = 0 AND p.like_count <> COALESCE(pl.cnt, 0)
ORDER BY abs(p.like_count - COALESCE(pl.cnt, 0)) DESC
LIMIT 20;

\if :apply
\else
  \echo '== 干跑结束（未修改任何数据）。确认后加 -v apply=1 执行。'
  \quit
\endif

\echo '== 执行校正 batch_size =' :batch_size ', sleep_ms =' :sleep_ms
CREATE SCHEMA IF NOT EXISTS ops;
CREATE TABLE IF NOT EXISTS ops.count_reconcile_log (
    run_id    uuid        NOT NULL,
    logged_at timestamptz NOT NULL DEFAULT now(),
    entity    text        NOT NULL,  -- post.like_count | comment.like_count | post.collect_count
    id        uuid        NOT NULL,
    old_value bigint      NOT NULL,
    new_value bigint      NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_count_reconcile_log_run ON ops.count_reconcile_log (run_id, entity);

-- 单个计数器的分批校正（过程内 COMMIT 需 PG 11+，且不能在外层事务块中 CALL）。
CREATE OR REPLACE PROCEDURE ops.reconcile_counter(
    p_run_id     uuid,
    p_entity     text,
    p_batch_size int,
    p_sleep_ms   int
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_target  text;
    v_column  text;
    v_source  text;
    v_fk      text;
    v_last    uuid := '00000000-0000-0000-0000-000000000000';
    v_upper   uuid;
    v_fixed   bigint;
    v_total   bigint := 0;
    v_batches bigint := 0;
BEGIN
    CASE p_entity
        WHEN 'post.like_count'    THEN v_target := 'domains.post';    v_column := 'like_count';    v_source := 'domains.post_like';    v_fk := 'post_id';
        WHEN 'comment.like_count' THEN v_target := 'domains.comment'; v_column := 'like_count';    v_source := 'domains.comment_like'; v_fk := 'comment_id';
        WHEN 'post.collect_count' THEN v_target := 'domains.post';    v_column := 'collect_count'; v_source := 'domains.post_collect'; v_fk := 'post_id';
        ELSE RAISE EXCEPTION 'unknown entity %', p_entity;
    END CASE;

    LOOP
        -- 本批主键上界（keyset，按 id 升序）
        -- 注：uuid 无 max() 聚合，取本批最后一行
        EXECUTE format('SELECT id FROM (SELECT id FROM %s WHERE id > $1 ORDER BY id LIMIT $2) b ORDER BY id DESC LIMIT 1', v_target)
            INTO v_upper USING v_last, p_batch_size;
        EXIT WHEN v_upper IS NULL;

        -- 先锁本批目标行：等待在途消费者事务提交，并阻止新的 ±1 插队，之后的计数读到的是最新已提交状态
        EXECUTE format('SELECT 1 FROM %s WHERE id > $1 AND id <= $2 AND deleted = 0 FOR UPDATE', v_target)
            USING v_last, v_upper;

        EXECUTE format($q$
            WITH s AS (
                SELECT x.id, x.%2$I AS old_value,
                       (SELECT count(*) FROM %3$s l WHERE l.%4$I = x.id AND l.deleted = 0) AS new_value
                FROM %1$s x
                WHERE x.id > $1 AND x.id <= $2 AND x.deleted = 0
            ),
            fixed AS (
                UPDATE %1$s t
                SET %2$I = s.new_value, update_time = CURRENT_TIMESTAMP
                FROM s
                WHERE t.id = s.id AND t.%2$I <> s.new_value
                RETURNING t.id, s.old_value, s.new_value
            )
            INSERT INTO ops.count_reconcile_log (run_id, entity, id, old_value, new_value)
            SELECT $3, $4, id, old_value, new_value FROM fixed
        $q$, v_target, v_column, v_source, v_fk)
            USING v_last, v_upper, p_run_id, p_entity;
        GET DIAGNOSTICS v_fixed = ROW_COUNT;

        COMMIT;
        v_total := v_total + v_fixed;
        v_batches := v_batches + 1;
        v_last := v_upper;
        IF v_batches % 50 = 0 THEN
            RAISE NOTICE '% : % batches, % rows fixed so far (last id %)', p_entity, v_batches, v_total, v_last;
        END IF;
        PERFORM pg_sleep(p_sleep_ms / 1000.0);
    END LOOP;

    RAISE NOTICE '% : done, % batches, % rows fixed', p_entity, v_batches, v_total;
END;
$$;

SELECT gen_random_uuid() AS run_id \gset
\echo '== run_id =' :run_id '（04_invalidate_redis_stats.sh 需要它）'
CALL ops.reconcile_counter(:'run_id', 'post.like_count', :batch_size, :sleep_ms);
CALL ops.reconcile_counter(:'run_id', 'comment.like_count', :batch_size, :sleep_ms);
CALL ops.reconcile_counter(:'run_id', 'post.collect_count', :batch_size, :sleep_ms);

\echo '== 本次校正汇总'
SELECT entity, count(*) AS rows_fixed, sum(old_value - new_value) AS net_removed
FROM ops.count_reconcile_log
WHERE run_id = :'run_id'
GROUP BY entity
ORDER BY entity;

\echo '== 完成。下一步：RUN_ID=' :run_id './04_invalidate_redis_stats.sh'
