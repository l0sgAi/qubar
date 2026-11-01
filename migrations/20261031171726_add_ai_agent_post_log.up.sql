-- add_ai_agent_post_log
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建一张表（另更新 ai_agent.auto_config 的列注释）：旧版本应用不读写新表。
-- 全局机器人在 hot 圈自动发帖（#101）的发帖日志：话题去重 + interval_min / daily_max 限频 + 审计。
-- 日志表，字段形态与 ai_agent_reply_log 一致（无软删 / 审计人列）；只在 PG 内读写，不进 ES。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.ai_agent_post_log (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    circle_id uuid NOT NULL,
    topic_key character varying(64) NOT NULL,
    post_id uuid,
    status smallint DEFAULT 2 NOT NULL,
    search_query character varying(128) DEFAULT ''::character varying NOT NULL,
    sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    error_msg character varying(2048),
    latency_ms integer DEFAULT 0 NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ai_agent_post_log_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.ai_agent_post_log IS 'AI机器人自动发帖日志(先插status=2占位行再调LLM,终态原地更新;话题去重+限频统计+审计)';

COMMENT ON COLUMN domains.ai_agent_post_log.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.ai_agent_post_log.agent_id IS '机器人ID(ai_agent.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.ai_agent_post_log.circle_id IS '发帖目标圈子ID(circle.id,无FK;全局机器人为配置aiagent.autonomy.hot_circle_id)';

COMMENT ON COLUMN domains.ai_agent_post_log.topic_key IS '话题键:规范化后的首条来源URL的SHA-256(小写hex);同一机器人同一话题只发一次';

COMMENT ON COLUMN domains.ai_agent_post_log.post_id IS '生成的帖子ID(post.id;占位/失败时为NULL)';

COMMENT ON COLUMN domains.ai_agent_post_log.status IS '结果: 0=失败, 1=成功, 2=占位(执行中;进程中断则保持2,视为已触发不重试)';

COMMENT ON COLUMN domains.ai_agent_post_log.search_query IS '本次搜索使用的关键词';

COMMENT ON COLUMN domains.ai_agent_post_log.sources IS '提供给LLM的搜索结果[{title,url,source,published_at}];成功后更新为帖子实际引用的来源';

COMMENT ON COLUMN domains.ai_agent_post_log.error_msg IS '失败原因(成功为NULL)';

COMMENT ON COLUMN domains.ai_agent_post_log.latency_ms IS 'LLM调用到发帖完成的耗时(毫秒)';

COMMENT ON COLUMN domains.ai_agent_post_log.prompt_tokens IS '输入token数(供应商返回,失败为0)';

COMMENT ON COLUMN domains.ai_agent_post_log.completion_tokens IS '输出token数(供应商返回,失败为0)';

COMMENT ON COLUMN domains.ai_agent_post_log.create_time IS '创建时间(占位时间;限频统计口径)';

COMMENT ON COLUMN domains.ai_agent_post_log.update_time IS '更新时间(终态写入时由应用层维护)';

CREATE UNIQUE INDEX uk_ai_agent_post_log_topic ON domains.ai_agent_post_log USING btree (agent_id, topic_key);

CREATE INDEX idx_ai_agent_post_log_agent_time ON domains.ai_agent_post_log USING btree (agent_id, create_time DESC);

COMMENT ON COLUMN domains.ai_agent.auto_config IS '自主行为配置(JSON对象,与trigger_mode互不影响): auto_reply={enabled,targets,post_hot_min,comment_like_min,comment_reply_min,window_hours,max_per_hour}; auto_post={enabled,interval_min,daily_max,active_hours,topics,require_review}(仅全局机器人); 各子对象按顶层key独立写入';
