-- add_private_message
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建三张表：旧版本应用不读写它们。
-- 私信核心（#238，#30 第 1 步）：会话 / 会话成员状态 / 消息。只在 PG 内读写，不进 ES。设计见 docs/message-design.md。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.conversation (
    id uuid DEFAULT uuidv7() NOT NULL,
    type smallint DEFAULT 1 NOT NULL,
    user_low uuid,
    user_high uuid,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    CONSTRAINT conversation_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.conversation IS '私信会话表(单聊每对用户一行,首次发送时建行;群聊 #31 复用本表)';

COMMENT ON COLUMN domains.conversation.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.conversation.type IS '会话类型:1=单聊,2=群聊(#31 预留);取值由应用层校验';

COMMENT ON COLUMN domains.conversation.user_low IS '单聊双方中 UUID 字节序较小的一方(users.id,无FK);群聊为 NULL';

COMMENT ON COLUMN domains.conversation.user_high IS '单聊双方中 UUID 字节序较大的一方(users.id,无FK);群聊为 NULL';

COMMENT ON COLUMN domains.conversation.create_time IS '创建时间(首条消息发送时间)';

COMMENT ON COLUMN domains.conversation.update_time IS '更新时间(每次发送消息时由应用层刷新)';

COMMENT ON COLUMN domains.conversation.deleted IS '删除标记:0=正常,1=已删除(预留,当前不使用;用户删除会话只改自己的成员行)';

-- 单聊每对用户只有一行（应用依赖：首次发送走 INSERT … ON CONFLICT (user_low, user_high)，勿删 / 勿改为部分索引）。
-- 群聊两列为 NULL，NULL 互不相等，不受此约束影响。
CREATE UNIQUE INDEX uk_conversation_pair ON domains.conversation USING btree (user_low, user_high);

CREATE TABLE domains.conversation_member (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    peer_id uuid,
    last_message_id uuid,
    last_read_message_id uuid,
    unread_count integer DEFAULT 0 NOT NULL,
    cleared_message_id uuid,
    sent_count integer DEFAULT 0 NOT NULL,
    first_sent_time timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    CONSTRAINT conversation_member_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.conversation_member IS '私信会话成员状态表(每个会话每个成员一行,与消息同事务维护;会话列表从本表按成员读取)';

COMMENT ON COLUMN domains.conversation_member.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.conversation_member.conversation_id IS '会话ID(conversation.id,无FK)';

COMMENT ON COLUMN domains.conversation_member.user_id IS '成员用户ID(users.id,无FK)';

COMMENT ON COLUMN domains.conversation_member.peer_id IS '单聊对方用户ID(users.id,无FK);群聊为 NULL';

COMMENT ON COLUMN domains.conversation_member.last_message_id IS '会话最新一条消息ID(message.id,UUIDv7 即时间序);会话列表排序键,NULL=还没有消息';

COMMENT ON COLUMN domains.conversation_member.last_read_message_id IS '本成员最后已读到的消息ID(标记已读/删除会话时置为 last_message_id);NULL=从未读过';

COMMENT ON COLUMN domains.conversation_member.unread_count IS '本成员在该会话的未读数(对方每发一条 +1,标记已读/删除会话清零;#241 未读总数使用)';

COMMENT ON COLUMN domains.conversation_member.cleared_message_id IS '隐藏截止位置:删除会话时置为当时的 last_message_id;本成员只能看到 id 大于它的消息,last_message_id 不大于它时会话不在列表里;NULL=从未删除';

COMMENT ON COLUMN domains.conversation_member.sent_count IS '本成员在该会话累计发送的消息条数(#239 未关注发送限制使用)';

COMMENT ON COLUMN domains.conversation_member.first_sent_time IS '本成员在该会话发送第一条消息的时间(#239 判断对方是否回复过使用);NULL=还没发过';

COMMENT ON COLUMN domains.conversation_member.create_time IS '创建时间';

COMMENT ON COLUMN domains.conversation_member.update_time IS '更新时间(应用层维护)';

COMMENT ON COLUMN domains.conversation_member.deleted IS '删除标记:0=正常,1=已退出(群聊 #31 预留;单聊删除会话用 cleared_message_id,不改本列)';

-- 每个会话每个成员只有一行（应用依赖：发送时走 INSERT … ON CONFLICT (conversation_id, user_id)，勿删 / 勿改为部分索引）
CREATE UNIQUE INDEX uk_conversation_member ON domains.conversation_member USING btree (conversation_id, user_id);

-- 会话列表：按最新消息倒序 keyset 翻页
CREATE INDEX idx_conversation_member_list ON domains.conversation_member USING btree (user_id, last_message_id DESC) WHERE (deleted = 0);

CREATE TABLE domains.message (
    id uuid DEFAULT uuidv7() NOT NULL,
    conversation_id uuid NOT NULL,
    sender_id uuid NOT NULL,
    msg_type smallint DEFAULT 1 NOT NULL,
    content character varying(2048) NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted smallint DEFAULT 0 NOT NULL,
    CONSTRAINT message_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.message IS '私信消息表(只追加;UUIDv7 主键即发送时间序)';

COMMENT ON COLUMN domains.message.id IS '主键ID(UUIDv7,应用层在会话行锁内生成,同一会话内单调递增;消息历史按它倒序 keyset 翻页)';

COMMENT ON COLUMN domains.message.conversation_id IS '会话ID(conversation.id,无FK)';

COMMENT ON COLUMN domains.message.sender_id IS '发送者用户ID(users.id,无FK)';

COMMENT ON COLUMN domains.message.msg_type IS '消息类型:1=文本(图片/文件等为后续预留);取值由应用层校验';

COMMENT ON COLUMN domains.message.content IS '消息正文(经 SanitizeForPg;长度 1-2000 字符由应用层校验)';

COMMENT ON COLUMN domains.message.create_time IS '发送时间';

COMMENT ON COLUMN domains.message.update_time IS '更新时间';

COMMENT ON COLUMN domains.message.deleted IS '删除标记:0=正常,1=已删除(撤回等后续功能预留,当前不使用)';

-- 消息历史：按会话向上翻旧消息（id 倒序 keyset）；会话列表按 last_message_id 点查最后一条消息走主键
CREATE INDEX idx_message_conversation ON domains.message USING btree (conversation_id, id DESC) WHERE (deleted = 0);
