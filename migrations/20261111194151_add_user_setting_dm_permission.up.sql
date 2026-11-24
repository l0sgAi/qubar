-- add_user_setting_dm_permission
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只给 domains.user_setting 加一个带常量默认值的 NOT NULL 列（PG11+ 只改目录，不重写表）：
-- 旧版本应用按列 upsert 其他设置项、不写本列，新行取默认值 0（所有人），行为与加列前一致。
-- 私信权限设置（#240，私信总览 #30 第 3 步）：谁可以给我发私信。
SET lock_timeout = '5s';

ALTER TABLE domains.user_setting
    ADD COLUMN dm_permission smallint DEFAULT 0 NOT NULL;

COMMENT ON COLUMN domains.user_setting.dm_permission IS '谁可以给我发私信:0=所有人(默认,受未关注 5 条限制),1=仅我关注的人,2=仅互相关注,3=关闭;已有会话同样受约束;取值由应用层校验';
