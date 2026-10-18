-- 基线：生产 domains schema（pg_dump --schema-only 清洗而来，见 deploy/scripts/clean-baseline.sh）。
-- 已有环境用 `migrate force <本文件版本>` 标记为已执行；只有全新空库（CI 回放 / 新环境）会真正执行。
-- 不要修改本文件；表结构变更一律新增迁移文件。

CREATE TABLE domains.ai_agent (
    id uuid DEFAULT uuidv7() NOT NULL,
    name character varying(50) NOT NULL,
    avatar_url character varying(500),
    linked_user_id uuid NOT NULL,
    api_protocol character varying(20) NOT NULL,
    base_url character varying(500) DEFAULT ''::character varying NOT NULL,
    api_key character varying(512),
    model character varying(100) NOT NULL,
    llm_params jsonb DEFAULT '{}'::jsonb NOT NULL,
    system_prompt text DEFAULT ''::text NOT NULL,
    trigger_mode smallint DEFAULT 1 NOT NULL,
    trigger_keywords jsonb DEFAULT '[]'::jsonb NOT NULL,
    max_replies_per_hour integer DEFAULT 30 NOT NULL,
    min_interval_sec integer DEFAULT 60 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    filter_prompt text DEFAULT ''::text NOT NULL,
    circle_id uuid,
    creator_id uuid
);

COMMENT ON TABLE domains.ai_agent IS 'AI回复机器人配置表(管理员维护)';

COMMENT ON COLUMN domains.ai_agent.name IS '机器人名称(展示名,未删除行内唯一)';

COMMENT ON COLUMN domains.ai_agent.linked_user_id IS '关联系统用户ID(机器人发评论的身份,创建agent时同步创建)';

COMMENT ON COLUMN domains.ai_agent.api_protocol IS 'API协议: openai/anthropic/gemini/ollama(白名单应用层校验)';

COMMENT ON COLUMN domains.ai_agent.base_url IS 'API基础地址(兼容OpenAI协议的中转/自托管端点)';

COMMENT ON COLUMN domains.ai_agent.api_key IS 'API密钥(应用层加密存密文,任何接口不得回显明文)';

COMMENT ON COLUMN domains.ai_agent.model IS '模型名';

COMMENT ON COLUMN domains.ai_agent.llm_params IS 'LLM通用参数(temperature/top_p/max_tokens/presence_penalty/frequency_penalty,未配置走应用层默认)';

COMMENT ON COLUMN domains.ai_agent.system_prompt IS '系统提示词(机器人人设/回复风格)';

COMMENT ON COLUMN domains.ai_agent.trigger_mode IS '触发模式: 1=全部新帖, 2=关键词触发, 3=手动';

COMMENT ON COLUMN domains.ai_agent.trigger_keywords IS 'trigger_mode=2时的关键词列表(JSON数组,应用层匹配)';

COMMENT ON COLUMN domains.ai_agent.max_replies_per_hour IS '每小时最大回复数(0=不限,按ai_agent_reply_log统计)';

COMMENT ON COLUMN domains.ai_agent.min_interval_sec IS '两次回复最小间隔秒数(0=不限)';

COMMENT ON COLUMN domains.ai_agent.status IS '状态: 0=停用, 1=启用';

COMMENT ON COLUMN domains.ai_agent.circle_id IS '绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=平台全局机器人(超管维护)';

COMMENT ON COLUMN domains.ai_agent.creator_id IS '创建者用户ID(users.id,审计用)';

CREATE TABLE domains.ai_agent_reply_log (
    id uuid DEFAULT uuidv7() NOT NULL,
    agent_id uuid NOT NULL,
    post_id uuid NOT NULL,
    comment_id uuid,
    user_id uuid NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    error_msg character varying(2048),
    latency_ms integer DEFAULT 0 NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.ai_agent_reply_log IS 'AI回复日志表(append-only,频率限制统计+审计)';

COMMENT ON COLUMN domains.ai_agent_reply_log.agent_id IS '机器人ID(ai_agent.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.ai_agent_reply_log.post_id IS '被回复帖子ID(post.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.ai_agent_reply_log.comment_id IS '生成的评论ID(comment.id,调用失败时为NULL)';

COMMENT ON COLUMN domains.ai_agent_reply_log.user_id IS '帖子作者ID(冗余,风控分析用)';

COMMENT ON COLUMN domains.ai_agent_reply_log.status IS '结果: 0=失败, 1=成功';

COMMENT ON COLUMN domains.ai_agent_reply_log.latency_ms IS 'LLM调用耗时(毫秒)';

COMMENT ON COLUMN domains.ai_agent_reply_log.prompt_tokens IS '输入token数(供应商返回,失败为0)';

COMMENT ON COLUMN domains.ai_agent_reply_log.completion_tokens IS '输出token数(供应商返回,失败为0)';

CREATE TABLE domains.category (
    id uuid DEFAULT uuidv7() NOT NULL,
    name character varying(50) NOT NULL,
    slug character varying(60),
    icon character varying(500),
    parent_id uuid,
    sort integer DEFAULT 0 NOT NULL,
    circle_count integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.category IS '圈子分类表';

COMMENT ON COLUMN domains.category.id IS '分类ID(UUIDv7)';

COMMENT ON COLUMN domains.category.name IS '分类名称';

COMMENT ON COLUMN domains.category.slug IS 'URL标识符';

COMMENT ON COLUMN domains.category.icon IS '分类图标';

COMMENT ON COLUMN domains.category.parent_id IS '父分类ID(NULL为顶级)';

COMMENT ON COLUMN domains.category.sort IS '排序权重(人工控制顺序)';

COMMENT ON COLUMN domains.category.circle_count IS '该分类下圈子数(统计缓存)';

COMMENT ON COLUMN domains.category.status IS '状态:0=隐藏,1=显示';

COMMENT ON COLUMN domains.category.deleted IS '逻辑删除';

COMMENT ON COLUMN domains.category.update_time IS '更新时间(用于ES同步)';

CREATE TABLE domains.circle (
    id uuid DEFAULT uuidv7() NOT NULL,
    name character varying(50) NOT NULL,
    slug character varying(60),
    avatar_url character varying(500),
    cover_url character varying(500),
    description text NOT NULL,
    rule text NOT NULL,
    creator_id uuid NOT NULL,
    category_id uuid,
    hot integer DEFAULT 0 NOT NULL,
    member_count integer DEFAULT 0 NOT NULL,
    post_count integer DEFAULT 0 NOT NULL,
    join_type smallint DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE ONLY domains.circle REPLICA IDENTITY FULL;

COMMENT ON TABLE domains.circle IS '兴趣圈/社区表';

COMMENT ON COLUMN domains.circle.id IS '主键ID(UUIDv7)';

COMMENT ON COLUMN domains.circle.name IS '圈子名称';

COMMENT ON COLUMN domains.circle.slug IS 'URL友好的唯一标识符';

COMMENT ON COLUMN domains.circle.avatar_url IS '圈子头像URL';

COMMENT ON COLUMN domains.circle.cover_url IS '圈子封面背景图URL';

COMMENT ON COLUMN domains.circle.description IS '圈子描述';

COMMENT ON COLUMN domains.circle.rule IS '圈子规则';

COMMENT ON COLUMN domains.circle.creator_id IS '创建者用户ID(UUID)';

COMMENT ON COLUMN domains.circle.category_id IS '所属分类ID(UUID)，NULL=未分类';

COMMENT ON COLUMN domains.circle.hot IS '综合热度值';

COMMENT ON COLUMN domains.circle.member_count IS '成员总数(缓存字段)';

COMMENT ON COLUMN domains.circle.post_count IS '帖子总数(缓存字段)';

COMMENT ON COLUMN domains.circle.join_type IS '加入限制：0=公开，1=审核，2=私密';

COMMENT ON COLUMN domains.circle.status IS '圈子状态：0=待审，1=正常，2=封禁';

COMMENT ON COLUMN domains.circle.deleted IS '逻辑删除：0=未删，1=已删';

COMMENT ON COLUMN domains.circle.create_time IS '创建时间';

COMMENT ON COLUMN domains.circle.update_time IS '更新时间';

CREATE TABLE domains.circle_member (
    id uuid DEFAULT uuidv7() NOT NULL,
    circle_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role smallint DEFAULT 10 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    reputation integer DEFAULT 0 NOT NULL,
    mute_end_time timestamp with time zone,
    is_top smallint DEFAULT 0,
    is_disturb smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.circle_member IS '圈子成员关系与权限表';

COMMENT ON COLUMN domains.circle_member.circle_id IS '圈子ID(UUID)';

COMMENT ON COLUMN domains.circle_member.user_id IS '用户ID(UUID)';

COMMENT ON COLUMN domains.circle_member.role IS '角色: 10=成员, 20=管理员, 30=圈主';

COMMENT ON COLUMN domains.circle_member.status IS '状态: 0=申请中, 1=正常, 2=禁言, 3=拉黑';

COMMENT ON COLUMN domains.circle_member.mute_end_time IS '禁言截止时间';

CREATE TABLE domains.comment (
    id uuid DEFAULT uuidv7() NOT NULL,
    post_id uuid NOT NULL,
    user_id uuid NOT NULL,
    root_id uuid,
    reply_to_id uuid,
    reply_to_user_id uuid,
    content text NOT NULL,
    extra_data jsonb DEFAULT '{}'::jsonb,
    like_count integer DEFAULT 0 NOT NULL,
    reply_count integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE ONLY domains.comment REPLICA IDENTITY FULL;

COMMENT ON TABLE domains.comment IS '评论表：统一存储评论的元数据和内容，PostgreSQL TOAST机制自动处理大字段存储';

COMMENT ON COLUMN domains.comment.id IS '主键ID(UUIDv7)';

COMMENT ON COLUMN domains.comment.post_id IS '所属帖子ID(UUID)';

COMMENT ON COLUMN domains.comment.user_id IS '评论发布者ID(UUID)';

COMMENT ON COLUMN domains.comment.root_id IS '根评论ID：NULL表示顶层评论，否则存放所属的顶层评论ID(UUID)';

COMMENT ON COLUMN domains.comment.reply_to_id IS '被回复的评论ID：NULL表示非回复特定人，否则存放被回复的那条评论ID(UUID)';

COMMENT ON COLUMN domains.comment.reply_to_user_id IS '被回复的用户id(UUID)';

COMMENT ON COLUMN domains.comment.content IS '评论文本内容';

COMMENT ON COLUMN domains.comment.extra_data IS '扩展数据：JSON格式，用于存储富文本结构、图片列表、视频链接等附加信息';

COMMENT ON COLUMN domains.comment.like_count IS '点赞数：高频更新字段，建议配合 Redis Write-Behind 策略';

COMMENT ON COLUMN domains.comment.reply_count IS '子回复数：该评论下的回复数量';

COMMENT ON COLUMN domains.comment.status IS '状态：1=正常, 2=审核中, 3=审核不通过/折叠';

COMMENT ON COLUMN domains.comment.deleted IS '逻辑删除：0=正常, 1=已删除';

COMMENT ON COLUMN domains.comment.create_time IS '创建时间';

COMMENT ON COLUMN domains.comment.update_time IS '更新时间 (如点赞、状态变更时更新)';

CREATE TABLE domains.comment_like (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    comment_id uuid NOT NULL,
    post_id uuid,
    deleted smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE ONLY domains.comment_like REPLICA IDENTITY FULL;

COMMENT ON TABLE domains.comment_like IS '评论点赞流水表';

COMMENT ON COLUMN domains.comment_like.user_id IS '点赞人ID(UUID)';

COMMENT ON COLUMN domains.comment_like.comment_id IS '被点赞评论ID(UUID)';

COMMENT ON COLUMN domains.comment_like.post_id IS '冗余帖子ID(UUID)，便于查询';

COMMENT ON COLUMN domains.comment_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';

CREATE TABLE domains.comment_mention (
    id uuid DEFAULT uuidv7() NOT NULL,
    comment_id uuid NOT NULL,
    user_id uuid NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE domains.log_payment_webhook_event (
    id uuid DEFAULT uuidv7() NOT NULL,
    event_id character varying(128) NOT NULL,
    event_type character varying(64) NOT NULL,
    provider_payment_intent_id character varying(128),
    payload jsonb,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.log_payment_webhook_event IS 'Received provider webhook events; insert-before-process makes replay idempotent.';

COMMENT ON COLUMN domains.log_payment_webhook_event.event_id IS 'Unique provider event id; duplicate inserts are rejected and treated as already processed.';

COMMENT ON COLUMN domains.log_payment_webhook_event.payload IS 'Raw event body for reconciliation; never returned to clients.';

CREATE TABLE domains.notification (
    id uuid DEFAULT uuidv7() NOT NULL,
    recipient_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    notice_type smallint NOT NULL,
    post_id uuid,
    comment_id uuid,
    snippet character varying(200) DEFAULT ''::character varying NOT NULL,
    is_read smallint DEFAULT 0 NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.notification IS '站内通知表(消息中心, Redpanda notification_events 事件驱动写入)';

COMMENT ON COLUMN domains.notification.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底); 字典序=时间序, keyset 翻页排序键';

COMMENT ON COLUMN domains.notification.recipient_id IS '接收人ID(UUID): 内容作者/被提及人';

COMMENT ON COLUMN domains.notification.actor_id IS '触发人ID(UUID)';

COMMENT ON COLUMN domains.notification.notice_type IS '通知类型: 1=帖子被赞 2=评论被赞 3=帖子被收藏 4=帖子被评论 5=评论被回复 6=@提及';

COMMENT ON COLUMN domains.notification.post_id IS '跳转帖子ID(UUID,可空)';

COMMENT ON COLUMN domains.notification.comment_id IS '跳转评论ID(UUID,可空)';

COMMENT ON COLUMN domains.notification.snippet IS '内容快照(评论正文前100字符/帖子标题), 目标删除后通知仍可读';

COMMENT ON COLUMN domains.notification.is_read IS '已读状态: 0=未读, 1=已读; 重复触发(重赞)时重置为0';

COMMENT ON COLUMN domains.notification.create_time IS '首次通知时间';

COMMENT ON COLUMN domains.notification.update_time IS '最近触发/已读操作时间';

CREATE TABLE domains.post (
    id uuid DEFAULT uuidv7() NOT NULL,
    circle_id uuid NOT NULL,
    user_id uuid NOT NULL,
    type smallint DEFAULT 1 NOT NULL,
    title character varying(200) DEFAULT ''::character varying NOT NULL,
    summary character varying(2048) DEFAULT ''::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    media_extra jsonb DEFAULT '[]'::jsonb NOT NULL,
    view_count integer DEFAULT 0 NOT NULL,
    comment_count integer DEFAULT 0 NOT NULL,
    like_count integer DEFAULT 0 NOT NULL,
    collect_count integer DEFAULT 0 NOT NULL,
    hot integer DEFAULT 0 NOT NULL,
    is_pinned smallint DEFAULT 0 NOT NULL,
    is_essence smallint DEFAULT 0 NOT NULL,
    is_lock smallint DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_reply_time timestamp with time zone
);

ALTER TABLE ONLY domains.post REPLICA IDENTITY FULL;

COMMENT ON TABLE domains.post IS '帖子主表';

COMMENT ON COLUMN domains.post.id IS '主键ID(UUIDv7)';

COMMENT ON COLUMN domains.post.circle_id IS '所属圈子ID(UUID)';

COMMENT ON COLUMN domains.post.user_id IS '作者用户ID(UUID)';

COMMENT ON COLUMN domains.post.type IS '类型:1=图文,2=视频,3=投票';

COMMENT ON COLUMN domains.post.title IS '标题';

COMMENT ON COLUMN domains.post.summary IS '纯文本摘要';

COMMENT ON COLUMN domains.post.content IS '正文内容(Text)';

COMMENT ON COLUMN domains.post.media_extra IS '媒体扩展信息(JSONB存储图片/视频)';

COMMENT ON COLUMN domains.post.view_count IS '浏览数';

COMMENT ON COLUMN domains.post.is_pinned IS '是否置顶';

COMMENT ON COLUMN domains.post.is_essence IS '是否加精';

COMMENT ON COLUMN domains.post.status IS '状态:1=正常,2=审核中,3=驳回,4=屏蔽';

COMMENT ON COLUMN domains.post.update_time IS '更新时间(ES同步锚点)';

CREATE TABLE domains.post_collect (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    post_id uuid NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.post_collect IS '帖子收藏流水表';

COMMENT ON COLUMN domains.post_collect.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.post_collect.user_id IS '收藏人ID(UUID)';

COMMENT ON COLUMN domains.post_collect.post_id IS '被收藏的帖子ID(UUID)';

COMMENT ON COLUMN domains.post_collect.deleted IS '收藏状态: 0=有效收藏, 1=取消收藏';

COMMENT ON COLUMN domains.post_collect.create_time IS '收藏时间(用于"我的收藏"按收藏时间倒序)';

COMMENT ON COLUMN domains.post_collect.update_time IS '更新时间(收藏/取消收藏切换时更新)';

CREATE TABLE domains.post_interaction (
    user_id uuid NOT NULL,
    post_id uuid NOT NULL,
    weight smallint NOT NULL,
    ts timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.post_interaction IS '用户×帖子交互矩阵（item-based CF 隐反馈评分表，事件驱动双写聚合）';

COMMENT ON COLUMN domains.post_interaction.user_id IS '互动用户ID(UUID)';

COMMENT ON COLUMN domains.post_interaction.post_id IS '被互动帖子ID(UUID)';

COMMENT ON COLUMN domains.post_interaction.weight IS '该 user-post 对最强信号强度（1..5：view1/comment_like2/like3/comment4/collect5）';

COMMENT ON COLUMN domains.post_interaction.ts IS '最近一次互动时间（CF 共现回溯窗口 + 清理锚点）';

CREATE TABLE domains.post_like (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    post_id uuid NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

ALTER TABLE ONLY domains.post_like REPLICA IDENTITY FULL;

COMMENT ON TABLE domains.post_like IS '帖子点赞流水表';

COMMENT ON COLUMN domains.post_like.user_id IS '点赞人ID(UUID)';

COMMENT ON COLUMN domains.post_like.post_id IS '帖子ID(UUID)，便于查询';

COMMENT ON COLUMN domains.post_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';

CREATE TABLE domains.post_mention (
    id uuid DEFAULT uuidv7() NOT NULL,
    post_id uuid NOT NULL,
    user_id uuid NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE domains.post_view_history (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    post_id uuid NOT NULL,
    view_count integer DEFAULT 1 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE domains.post_view_history IS '帖子浏览历史表(去重模型:每对 user+post 一行,再看时 bump update_time+view_count)';

COMMENT ON COLUMN domains.post_view_history.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.post_view_history.user_id IS '浏览人ID(UUID)';

COMMENT ON COLUMN domains.post_view_history.post_id IS '被浏览的帖子ID(UUID)';

COMMENT ON COLUMN domains.post_view_history.view_count IS '该帖被该用户浏览次数(MQ 聚合 +1)';

COMMENT ON COLUMN domains.post_view_history.create_time IS '首次浏览时间';

COMMENT ON COLUMN domains.post_view_history.update_time IS '最近浏览时间(再看时更新,「最近浏览」列表排序键 + 冷启动回源排序键)';

CREATE TABLE domains.users (
    id uuid DEFAULT uuidv7() NOT NULL,
    username character varying(50) NOT NULL,
    email character varying(100) NOT NULL,
    phone character varying(20),
    pwd character varying(512),
    motto character varying(2048),
    google_id character varying(100),
    x_id character varying(100),
    github_id character varying(100),
    microsoft_id character varying(100),
    avatar_url character varying(500),
    gender smallint DEFAULT 0,
    birthdate date,
    status smallint DEFAULT 1,
    role smallint DEFAULT 0,
    deleted smallint DEFAULT 0,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    agent_circle_id uuid
);

ALTER TABLE ONLY domains.users REPLICA IDENTITY FULL;

COMMENT ON COLUMN domains.users.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.users.username IS '用户名/昵称';

COMMENT ON COLUMN domains.users.email IS '邮箱(唯一凭证)';

COMMENT ON COLUMN domains.users.google_id IS 'Google平台唯一ID';

COMMENT ON COLUMN domains.users.x_id IS 'X/Twitter平台唯一ID';

COMMENT ON COLUMN domains.users.github_id IS 'Github平台唯一ID';

COMMENT ON COLUMN domains.users.status IS '状态：0=禁用，1=启用';

COMMENT ON COLUMN domains.users.deleted IS '逻辑删除：0=正常，1=已删除';

COMMENT ON COLUMN domains.users.agent_circle_id IS '机器人绑定圈子ID(domains.circle.id);NULL=普通用户或全局机器人;仅 role=2 有意义';

ALTER TABLE ONLY domains.ai_agent
    ADD CONSTRAINT ai_agent_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.ai_agent_reply_log
    ADD CONSTRAINT ai_agent_reply_log_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.category
    ADD CONSTRAINT category_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.circle_member
    ADD CONSTRAINT circle_member_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.circle
    ADD CONSTRAINT circle_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.comment_like
    ADD CONSTRAINT comment_like_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.comment_mention
    ADD CONSTRAINT comment_mention_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.comment
    ADD CONSTRAINT comment_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.notification
    ADD CONSTRAINT notification_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.log_payment_webhook_event
    ADD CONSTRAINT pk_log_payment_webhook_event PRIMARY KEY (id);

ALTER TABLE ONLY domains.post_collect
    ADD CONSTRAINT post_collect_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.post_interaction
    ADD CONSTRAINT post_interaction_pkey PRIMARY KEY (user_id, post_id);

ALTER TABLE ONLY domains.post_like
    ADD CONSTRAINT post_like_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.post_mention
    ADD CONSTRAINT post_mention_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.post
    ADD CONSTRAINT post_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.post_view_history
    ADD CONSTRAINT post_view_history_pkey PRIMARY KEY (id);

ALTER TABLE ONLY domains.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

CREATE INDEX idx_ai_agent_active ON domains.ai_agent USING btree (id) WHERE ((deleted = 0) AND (status = 1));

CREATE INDEX idx_ai_agent_circle ON domains.ai_agent USING btree (circle_id) WHERE (deleted = 0);

CREATE INDEX idx_ai_agent_linked_user ON domains.ai_agent USING btree (linked_user_id) WHERE (deleted = 0);

CREATE UNIQUE INDEX idx_ai_agent_name ON domains.ai_agent USING btree (COALESCE(circle_id, '00000000-0000-0000-0000-000000000000'::uuid), name) WHERE (deleted = 0);

CREATE INDEX idx_ai_reply_agent_time ON domains.ai_agent_reply_log USING btree (agent_id, create_time DESC);

CREATE INDEX idx_category_list ON domains.category USING btree (parent_id, sort DESC) WHERE ((deleted = 0) AND (status = 1));

CREATE UNIQUE INDEX idx_category_name ON domains.category USING btree (name) WHERE (deleted = 0);

CREATE UNIQUE INDEX idx_category_slug ON domains.category USING btree (slug) WHERE (deleted = 0);

CREATE UNIQUE INDEX idx_circle_name ON domains.circle USING btree (name) WHERE (deleted = 0);

CREATE UNIQUE INDEX idx_circle_slug ON domains.circle USING btree (slug) WHERE (deleted = 0);

CREATE INDEX idx_clike_comment_active ON domains.comment_like USING btree (comment_id, create_time DESC) WHERE (deleted = 0);

CREATE INDEX idx_clike_post_active ON domains.post_like USING btree (post_id, create_time DESC) WHERE (deleted = 0);

CREATE INDEX idx_clike_user_active ON domains.comment_like USING btree (user_id, create_time DESC) WHERE (deleted = 0);

CREATE INDEX idx_comment_mention_user ON domains.comment_mention USING btree (user_id, create_time DESC);

CREATE INDEX idx_comment_post_root_like ON domains.comment USING btree (post_id, root_id, like_count DESC);

CREATE INDEX idx_comment_post_root_time ON domains.comment USING btree (post_id, root_id, create_time DESC);

CREATE INDEX idx_comment_root_id ON domains.comment USING btree (root_id) WHERE (root_id IS NOT NULL);

CREATE INDEX idx_comment_user_time ON domains.comment USING btree (user_id, create_time DESC);

CREATE INDEX idx_log_payment_webhook_event_intent ON domains.log_payment_webhook_event USING btree (provider_payment_intent_id);

CREATE INDEX idx_member_circle_role ON domains.circle_member USING btree (circle_id, role DESC, create_time DESC);

CREATE UNIQUE INDEX idx_member_unique ON domains.circle_member USING btree (circle_id, user_id);

CREATE INDEX idx_member_user ON domains.circle_member USING btree (user_id, role, status);

CREATE INDEX idx_notice_recipient_id ON domains.notification USING btree (recipient_id, id DESC) WHERE (deleted = 0);

CREATE INDEX idx_notice_recipient_unread ON domains.notification USING btree (recipient_id, is_read) WHERE (deleted = 0);

CREATE INDEX idx_pcollect_post_active ON domains.post_collect USING btree (post_id, create_time DESC) WHERE (deleted = 0);

CREATE INDEX idx_pcollect_user_active ON domains.post_collect USING btree (user_id, create_time DESC, id DESC) WHERE (deleted = 0);

CREATE INDEX idx_post_circle ON domains.post USING btree (circle_id);

CREATE INDEX idx_post_interaction_post ON domains.post_interaction USING btree (post_id);

CREATE INDEX idx_post_interaction_post_ts ON domains.post_interaction USING btree (post_id, ts DESC);

CREATE INDEX idx_post_interaction_ts ON domains.post_interaction USING btree (ts);

CREATE INDEX idx_post_mention_user ON domains.post_mention USING btree (user_id, create_time DESC);

CREATE INDEX idx_post_pinned ON domains.post USING btree (circle_id) WHERE ((is_pinned = 1) AND (deleted = 0));

CREATE INDEX idx_post_status ON domains.post USING btree (status, create_time DESC);

CREATE INDEX idx_post_user ON domains.post USING btree (user_id, status, create_time DESC);

CREATE INDEX idx_pviewhist_user_time ON domains.post_view_history USING btree (user_id, update_time DESC, id DESC);

CREATE INDEX idx_user_post_active ON domains.post_like USING btree (user_id, create_time DESC) WHERE (deleted = 0);

CREATE UNIQUE INDEX idx_users_email ON domains.users USING btree (email);

CREATE INDEX idx_users_github_id ON domains.users USING btree (github_id);

CREATE INDEX idx_users_google_id ON domains.users USING btree (google_id);

CREATE INDEX idx_users_phone ON domains.users USING btree (phone);

CREATE INDEX idx_users_x_id ON domains.users USING btree (x_id);

CREATE UNIQUE INDEX uk_comment_like_user_comment ON domains.comment_like USING btree (user_id, comment_id);

CREATE UNIQUE INDEX uk_comment_mention_comment_user ON domains.comment_mention USING btree (comment_id, user_id);

CREATE UNIQUE INDEX uk_notice_dedup ON domains.notification USING btree (recipient_id, actor_id, notice_type, COALESCE(post_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE (deleted = 0);

CREATE UNIQUE INDEX uk_post_collect_user_post ON domains.post_collect USING btree (user_id, post_id);

CREATE UNIQUE INDEX uk_post_like_user_post ON domains.post_like USING btree (user_id, post_id);

CREATE UNIQUE INDEX uk_post_mention_post_user ON domains.post_mention USING btree (post_id, user_id);

CREATE UNIQUE INDEX uk_pviewhist_user_post ON domains.post_view_history USING btree (user_id, post_id);

CREATE UNIQUE INDEX uq_log_payment_webhook_event_event_id ON domains.log_payment_webhook_event USING btree (event_id);

