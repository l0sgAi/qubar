-- add_user_follow
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建两张表：旧版本应用不读写它们。
-- 用户关注（#217）：关注关系 + 关注数 / 粉丝数。只在 PG 内读写，不进 ES。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.user_follow (
    id uuid DEFAULT uuidv7() NOT NULL,
    follower_id uuid NOT NULL,
    followee_id uuid NOT NULL,
    follow_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    CONSTRAINT user_follow_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.user_follow IS '用户关注关系表(每对 follower+followee 一行;取关置 deleted=1,再次关注复用该行)';

COMMENT ON COLUMN domains.user_follow.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底);行会被复用,不代表关注时间';

COMMENT ON COLUMN domains.user_follow.follower_id IS '关注者(users.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.user_follow.followee_id IS '被关注者(users.id,无FK;关注时须为正常状态用户,可以是AI机器人账号)';

COMMENT ON COLUMN domains.user_follow.follow_time IS '最近一次关注的时间(取关后再关注时更新;关注/粉丝列表的排序键)';

COMMENT ON COLUMN domains.user_follow.create_time IS '首次关注时间(行创建时间)';

COMMENT ON COLUMN domains.user_follow.update_time IS '更新时间(关注/取关切换时由应用层更新)';

COMMENT ON COLUMN domains.user_follow.deleted IS '关注状态:0=关注中,1=已取关';

-- 每对用户只有一行（应用依赖：关注走 INSERT … ON CONFLICT (follower_id, followee_id)，勿删 / 勿改为部分索引）
CREATE UNIQUE INDEX uk_user_follow_pair ON domains.user_follow USING btree (follower_id, followee_id);

-- 「我关注的」：按关注时间倒序 keyset 翻页
CREATE INDEX idx_user_follow_following ON domains.user_follow USING btree (follower_id, follow_time DESC, id DESC) WHERE (deleted = 0);

-- 「关注我的」：按关注时间倒序 keyset 翻页
CREATE INDEX idx_user_follow_follower ON domains.user_follow USING btree (followee_id, follow_time DESC, id DESC) WHERE (deleted = 0);

CREATE TABLE domains.user_follow_stat (
    user_id uuid NOT NULL,
    following_count integer DEFAULT 0 NOT NULL,
    follower_count integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT user_follow_stat_pkey PRIMARY KEY (user_id)
);

COMMENT ON TABLE domains.user_follow_stat IS '用户关注计数表(与 user_follow 同事务更新;首次关注/被关注时建行,无行视为 0)';

COMMENT ON COLUMN domains.user_follow_stat.user_id IS '用户ID(users.id,无FK;主键,每个用户至多一行)';

COMMENT ON COLUMN domains.user_follow_stat.following_count IS '关注数(该用户关注了多少人;上限 follow.max_following 由应用层条件自增保证)';

COMMENT ON COLUMN domains.user_follow_stat.follower_count IS '粉丝数(多少人关注了该用户;不设上限)';

COMMENT ON COLUMN domains.user_follow_stat.create_time IS '创建时间';

COMMENT ON COLUMN domains.user_follow_stat.update_time IS '更新时间(应用层维护)';
