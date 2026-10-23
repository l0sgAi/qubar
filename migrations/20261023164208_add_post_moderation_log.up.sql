-- add_post_moderation_log
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建表，旧版本应用不读写它，直接兼容。
-- 帖子 AI 审核审计日志（#117）：append-only，只写不改，不进 ES。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.post_moderation_log (
    id uuid DEFAULT uuidv7() NOT NULL,
    post_id uuid NOT NULL,
    decision character varying(16) NOT NULL,
    hazards jsonb DEFAULT '{}'::jsonb NOT NULL,
    severity double precision,
    severity_confidence double precision,
    model character varying(64) DEFAULT ''::character varying NOT NULL,
    input_tokens integer DEFAULT 0 NOT NULL,
    latency_ms integer DEFAULT 0 NOT NULL,
    error_msg character varying(2048),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT post_moderation_log_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.post_moderation_log IS '帖子AI审核日志表(append-only,每次审核一行,审计+阈值调优)';

COMMENT ON COLUMN domains.post_moderation_log.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.post_moderation_log.post_id IS '被审核帖子ID(post.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.post_moderation_log.decision IS '分流结果:publish=发布,review=人工待审,reject=驳回,fail_open=审核不可用按fail_open放行';

COMMENT ON COLUMN domains.post_moderation_log.hazards IS '各风险概率(0-1),如{"sexual":0.01,...};审核调用失败时为{}';

COMMENT ON COLUMN domains.post_moderation_log.severity IS '危害程度期望分(0-3);审核调用失败时为NULL';

COMMENT ON COLUMN domains.post_moderation_log.severity_confidence IS 'severity置信度(0-1);审核调用失败时为NULL';

COMMENT ON COLUMN domains.post_moderation_log.model IS '实际作答的模型版本号(如jev-1.13.0);失败时为空';

COMMENT ON COLUMN domains.post_moderation_log.input_tokens IS '计费输入token数(失败为0)';

COMMENT ON COLUMN domains.post_moderation_log.latency_ms IS '审核调用总耗时(毫秒,含重试等待)';

COMMENT ON COLUMN domains.post_moderation_log.error_msg IS '审核调用错误信息(成功为NULL;不含api_key)';

COMMENT ON COLUMN domains.post_moderation_log.create_time IS '审核时间';

CREATE INDEX idx_post_moderation_log_post ON domains.post_moderation_log USING btree (post_id, create_time DESC);
