-- post_moderation_log_manual
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只加列：source 带常量默认值（PG11+ 只改元数据、不重写表），operator_id / reason 可空；
-- 旧版本应用写审核日志时不带新列，落默认值 source=1（AI），读写均不受影响。
-- 帖子人工审核（#118）：审核日志区分 AI 与人工，记录处理人与备注；通知类型新增 7 / 8（只改注释，列无 CHECK）。
SET lock_timeout = '5s';

ALTER TABLE domains.post_moderation_log
    ADD COLUMN source smallint DEFAULT 1 NOT NULL,
    ADD COLUMN operator_id uuid,
    ADD COLUMN reason character varying(200);

COMMENT ON COLUMN domains.post_moderation_log.source IS '审核来源:1=AI(审核worker),2=人工(圈主/管理员)';

COMMENT ON COLUMN domains.post_moderation_log.operator_id IS '人工审核处理人(user.id,无FK);AI审核为NULL';

COMMENT ON COLUMN domains.post_moderation_log.reason IS '人工审核备注/驳回理由(可空,≤200字符);AI审核为NULL';

COMMENT ON COLUMN domains.post_moderation_log.decision IS '分流结果:publish=发布,review=人工待审,reject=驳回,fail_open=审核不可用按fail_open放行;人工审核(source=2)只有publish=通过/reject=驳回';

COMMENT ON COLUMN domains.notification.notice_type IS '通知类型: 1=帖子被赞 2=评论被赞 3=帖子被收藏 4=帖子被评论 5=评论被回复 6=@提及 7=帖子审核通过 8=帖子审核驳回';
