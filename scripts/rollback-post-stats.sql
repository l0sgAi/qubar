-- 回退 R1 前，排空并停止 R2 的写入，再回写旧表；独立使用 psql -X -v ON_ERROR_STOP=1 -f 执行。
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

        UPDATE domains.post p SET (view_count, comment_count, like_count, collect_count, update_time) =
            (s.view_count, s.comment_count, s.like_count, s.collect_count, CURRENT_TIMESTAMP)
        FROM domains.post_stats s WHERE p.id = ANY(ids) AND s.post_id = p.id
        AND (p.view_count, p.comment_count, p.like_count, p.collect_count) IS DISTINCT FROM
            (s.view_count, s.comment_count, s.like_count, s.collect_count);

        last_id := ids[cardinality(ids)];
        COMMIT;
        PERFORM pg_sleep(0.1);
    END LOOP;
END $$;
