-- add_user_setting
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建一张表：旧版本应用不读写它。
-- 用户设置（#218）：个人中心的开关类设置，每个用户至多一行，没有行时全部取默认值。只在 PG 内读写，不进 ES。
-- 不放在 users 表：users 在 CDC / ES 同步链路里。后续设置项（如 #30 私信权限）在本表加列。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.user_setting (
    user_id uuid NOT NULL,
    follow_list_visibility smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT user_setting_pkey PRIMARY KEY (user_id)
);

COMMENT ON TABLE domains.user_setting IS '用户设置表(每个用户至多一行;首次修改设置时建行,无行视为全部默认值)';

COMMENT ON COLUMN domains.user_setting.user_id IS '用户ID(users.id,无FK;主键,每个用户至多一行)';

COMMENT ON COLUMN domains.user_setting.follow_list_visibility IS '关注列表/粉丝列表可见性(一个开关同时控制两个列表):0=公开(默认),1=私密(仅本人可看名单;关注数/粉丝数仍公开);取值由应用层校验';

COMMENT ON COLUMN domains.user_setting.create_time IS '创建时间(首次修改设置的时间)';

COMMENT ON COLUMN domains.user_setting.update_time IS '更新时间(应用层维护)';
