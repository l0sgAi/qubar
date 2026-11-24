-- add_post_share
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 新建 post_share：旧版本应用不读写它。
-- post 加 share_count：带常量默认值的 NOT NULL 列，PG 11+ 只改元数据、不重写表；旧版本应用不读写这一列
-- （GORM 按结构体字段读写，INSERT 不带该列时取默认值 0）。post 在 CDC 链路里，es_sinks 白名单不含 share_count，不进 ES（#232 决策）。
-- 帖子外部分享（#232）：一人一帖只记一次、不可撤销；分享数 Redis 为读路径真值，post.share_count 由帖子统计消费者按本表批量重算。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.post_share (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    post_id uuid NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT post_share_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.post_share IS '帖子分享记录表(每对 user+post 一行,首次分享时插入;分享不可撤销,行不更新不删除,故无 update_time / deleted)';

COMMENT ON COLUMN domains.post_share.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底);分享事件的 share_id';

COMMENT ON COLUMN domains.post_share.user_id IS '分享人(users.id,无FK)';

COMMENT ON COLUMN domains.post_share.post_id IS '被分享的帖子(post.id,无FK;分享时须已发布、未删除且对分享人可见)';

COMMENT ON COLUMN domains.post_share.create_time IS '首次分享时间';

-- 一人一帖只记一次（应用依赖：分享走 INSERT … ON CONFLICT (user_id, post_id) DO NOTHING，影响行数即「是否首次」，勿删 / 勿改为部分索引）
CREATE UNIQUE INDEX uk_post_share_user_post ON domains.post_share USING btree (user_id, post_id);

-- 按帖子重算分享数（帖子统计消费者 count(*) WHERE post_id = ?）
CREATE INDEX idx_post_share_post ON domains.post_share USING btree (post_id);

ALTER TABLE domains.post ADD COLUMN share_count integer DEFAULT 0 NOT NULL;

COMMENT ON COLUMN domains.post.share_count IS '分享数(去重后的分享人数;Redis post:sharecnt 为读路径真值,本列由帖子统计消费者按 post_share 批量重算,不进 ES)';
