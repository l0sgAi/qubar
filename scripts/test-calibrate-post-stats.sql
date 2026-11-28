-- 在已回放迁移的一次性空库中运行。校准脚本会提交事务，因此测试使用同一会话的临时表保存期望值。
\set ON_ERROR_STOP on
DO $$
BEGIN
    IF EXISTS (SELECT FROM domains.post) THEN
        RAISE EXCEPTION '校准回归测试需要空帖子表';
    END IF;
END $$;

-- 1001 帖跨越 1000 行批次。相邻帖子交替有明细、无明细，覆盖缺行与已有错误镜像。
CREATE TEMP TABLE cases AS
SELECT uuidv7() AS id, n AS views, (n % 2) AS active
FROM generate_series(1, 1001) AS g(n);

INSERT INTO domains.post (id, circle_id, user_id, view_count, comment_count, like_count, collect_count)
SELECT id, uuidv7(), uuidv7(), views, 9, 8, 7 FROM cases;
INSERT INTO domains.post_stats (post_id, view_count)
SELECT id, -1 FROM cases WHERE active = 1;

-- 有明细的帖子期望 1 条可见评论、2 条有效赞、3 条有效收藏。
INSERT INTO domains.comment (id, post_id, user_id, content, status, deleted)
SELECT uuidv7(), id, uuidv7(), '校准测试', status, deleted
FROM cases CROSS JOIN (VALUES (1, 0), (2, 0), (1, 1)) AS d(status, deleted) WHERE active = 1;
INSERT INTO domains.post_like (id, post_id, user_id, deleted)
SELECT uuidv7(), id, uuidv7(), deleted
FROM cases CROSS JOIN (VALUES (0), (0), (1)) AS d(deleted) WHERE active = 1;
INSERT INTO domains.post_collect (id, post_id, user_id, deleted)
SELECT uuidv7(), id, uuidv7(), deleted
FROM cases CROSS JOIN (VALUES (0), (0), (0), (1)) AS d(deleted) WHERE active = 1;

\ir calibrate-post-stats.sql

DO $$
BEGIN
    IF EXISTS (
        SELECT FROM cases c
        LEFT JOIN domains.post p ON p.id = c.id LEFT JOIN domains.post_stats s ON s.post_id = c.id
        WHERE (p.view_count, p.comment_count, p.like_count, p.collect_count)
              IS DISTINCT FROM (c.views, c.active, 2*c.active, 3*c.active)
           OR (s.view_count, s.comment_count, s.like_count, s.collect_count)
              IS DISTINCT FROM (c.views, c.active, 2*c.active, 3*c.active)
    ) THEN
        RAISE EXCEPTION '首次校准出现缺行或计数不符';
    END IF;
    IF (SELECT count(DISTINCT s.xmin::text) FROM domains.post_stats s) < 2 THEN
        RAISE EXCEPTION '1001 行校准未分批提交';
    END IF;
END $$;

CREATE TEMP TABLE versions AS
SELECT p.id, p.xmin AS post_version, s.xmin AS stats_version
FROM domains.post p JOIN domains.post_stats s ON s.post_id = p.id;

\ir calibrate-post-stats.sql

DO $$
BEGIN
    IF EXISTS (
        SELECT FROM versions v
        LEFT JOIN domains.post p ON p.id = v.id LEFT JOIN domains.post_stats s ON s.post_id = v.id
        WHERE p.xmin IS DISTINCT FROM v.post_version OR s.xmin IS DISTINCT FROM v.stats_version
    ) THEN
        RAISE EXCEPTION '二次校准改写或删除了已对齐的行';
    END IF;
END $$;
