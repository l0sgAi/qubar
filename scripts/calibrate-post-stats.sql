-- R1 双写版本上线后、R2 切读前执行。运行条件见 docs/count-cdc-decoupling-design.md。
-- 使用 psql -X -v ON_ERROR_STOP=1 -f 单独运行，DO 内每批提交，不要外包事务。
SET lock_timeout = '5s';
DO $$
DECLARE
    last_id uuid;
    upper_id uuid := (SELECT id FROM domains.post ORDER BY id DESC LIMIT 1);
    ids uuid[];
BEGIN
    LOOP
        SELECT array_agg(id ORDER BY id) INTO ids FROM (
            SELECT id FROM domains.post
            WHERE (last_id IS NULL OR id > last_id) AND id <= upper_id
            ORDER BY id LIMIT 1000 FOR NO KEY UPDATE
        ) batch;
        EXIT WHEN ids IS NULL;

        -- 父行锁到手后另起语句读取明细，可见性与评论列表一致。
        WITH counts AS (
            SELECT id,
                (SELECT count(*) FROM domains.comment WHERE post_id = p.id AND deleted = 0 AND status = 1) AS comments,
                (SELECT count(*) FROM domains.post_like WHERE post_id = p.id AND deleted = 0) AS likes,
                (SELECT count(*) FROM domains.post_collect WHERE post_id = p.id AND deleted = 0) AS collects
            FROM domains.post p WHERE id = ANY(ids)
        )
        UPDATE domains.post p SET comment_count = c.comments, like_count = c.likes,
            collect_count = c.collects, update_time = CURRENT_TIMESTAMP
        FROM counts c WHERE p.id = c.id AND (p.comment_count, p.like_count, p.collect_count)
            IS DISTINCT FROM (c.comments, c.likes, c.collects);

        INSERT INTO domains.post_stats AS s (post_id, view_count, comment_count, like_count, collect_count, update_time)
        SELECT id, view_count, comment_count, like_count, collect_count, update_time
        FROM domains.post WHERE id = ANY(ids) ORDER BY id
        ON CONFLICT (post_id) DO UPDATE SET (view_count, comment_count, like_count, collect_count, update_time) =
            (EXCLUDED.view_count, EXCLUDED.comment_count, EXCLUDED.like_count, EXCLUDED.collect_count, EXCLUDED.update_time)
        WHERE (s.view_count, s.comment_count, s.like_count, s.collect_count) IS DISTINCT FROM
            (EXCLUDED.view_count, EXCLUDED.comment_count, EXCLUDED.like_count, EXCLUDED.collect_count);

        last_id := ids[cardinality(ids)];
        COMMIT;
        PERFORM pg_sleep(0.1);
    END LOOP;
END $$;
