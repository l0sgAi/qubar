-- add_post_report
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建表 + 改注释：旧版本应用不读写 post_report；post_moderation_log.decision / notification.notice_type 无 CHECK，
-- 新取值（review_publish、report_dismiss、report_remove；通知类型 9 / 10 / 11）只由新版本写入。
-- 帖子举报（#153）：先发后审，用户举报 → 圈主 / 管理员复核。只在 PG 内读写，不进 ES。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.post_report (
    id uuid DEFAULT uuidv7() NOT NULL,
    post_id uuid NOT NULL,
    circle_id uuid NOT NULL,
    reporter_id uuid NOT NULL,
    reason_type smallint NOT NULL,
    reason character varying(200) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 0 NOT NULL,
    notified smallint DEFAULT 0 NOT NULL,
    handler_id uuid,
    handle_time timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT post_report_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.post_report IS '帖子举报表(每用户每帖一次;待处理举报达阈值通知圈主/管理员复核)';

COMMENT ON COLUMN domains.post_report.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.post_report.post_id IS '被举报帖子ID(post.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.post_report.circle_id IS '帖子所属圈子ID(冗余自post.circle_id,圈内复核列表用)';

COMMENT ON COLUMN domains.post_report.reporter_id IS '举报人(user.id,无FK);不向帖子作者暴露';

COMMENT ON COLUMN domains.post_report.reason_type IS '举报原因:1=垃圾广告/引流,2=色情低俗,3=辱骂骚扰,4=违法违规,5=暴力恐怖,6=泄露隐私,7=自伤轻生,8=其他';

COMMENT ON COLUMN domains.post_report.reason IS '补充说明(可空串,≤200字符)';

COMMENT ON COLUMN domains.post_report.status IS '处理状态:0=待处理,1=举报不成立(帖子保留),2=帖子已下架';

COMMENT ON COLUMN domains.post_report.notified IS '本轮待处理举报是否已通知圈管:0=否,1=是(达到通知阈值的那条举报置1,同一帖子处理前只通知一次)';

COMMENT ON COLUMN domains.post_report.handler_id IS '复核处理人(user.id,无FK);待处理为NULL';

COMMENT ON COLUMN domains.post_report.handle_time IS '复核处理时间;待处理为NULL';

COMMENT ON COLUMN domains.post_report.create_time IS '举报时间';

COMMENT ON COLUMN domains.post_report.update_time IS '更新时间';

CREATE UNIQUE INDEX uk_post_report_post_reporter ON domains.post_report USING btree (post_id, reporter_id);

CREATE INDEX idx_post_report_reporter ON domains.post_report USING btree (reporter_id, create_time DESC);

CREATE INDEX idx_post_report_circle_pending ON domains.post_report USING btree (circle_id, post_id) WHERE (status = 0);

COMMENT ON COLUMN domains.post_moderation_log.decision IS '分流结果:publish=发布,review=人工待审,reject=驳回,fail_open=审核不可用按fail_open放行,review_publish=AI待审但review_action=publish自动发布;人工审核(source=2):publish=通过,reject=驳回,report_dismiss=举报复核保留,report_remove=举报复核下架';

COMMENT ON COLUMN domains.notification.notice_type IS '通知类型: 1=帖子被赞 2=评论被赞 3=帖子被收藏 4=帖子被评论 5=评论被回复 6=@提及 7=帖子审核通过 8=帖子审核驳回 9=帖子被举报(圈管) 10=帖子疑似自伤(圈管) 11=帖子被举报下架(作者)';
