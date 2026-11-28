SET lock_timeout = '5s';

-- cdc: none
CREATE TABLE domains.post_stats (
    post_id uuid PRIMARY KEY,
    view_count integer NOT NULL DEFAULT 0,
    comment_count integer NOT NULL DEFAULT 0,
    like_count integer NOT NULL DEFAULT 0,
    collect_count integer NOT NULL DEFAULT 0,
    update_time timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE domains.post_stats IS '帖子展示计数，不进入 CDC';
COMMENT ON COLUMN domains.post_stats.post_id IS '对应帖子的 UUIDv7';
COMMENT ON COLUMN domains.post_stats.view_count IS '浏览量';
COMMENT ON COLUMN domains.post_stats.comment_count IS '可见评论数';
COMMENT ON COLUMN domains.post_stats.like_count IS '有效点赞数';
COMMENT ON COLUMN domains.post_stats.collect_count IS '有效收藏数';
COMMENT ON COLUMN domains.post_stats.update_time IS '计数更新时间，初始回填沿用帖子更新时间';

-- 保留软删除帖。此快照之后旧版本产生的计数和新帖由双写阶段的校准补齐。
INSERT INTO domains.post_stats (post_id, view_count, comment_count, like_count, collect_count, update_time)
SELECT id, view_count, comment_count, like_count, collect_count, update_time
FROM domains.post
ORDER BY id;
