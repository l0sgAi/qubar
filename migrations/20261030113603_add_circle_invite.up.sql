-- add_circle_invite
-- 兼容性：本迁移在新版本应用发布之前执行，必须兼容正在运行的旧版本（expand / contract，见 migrations/README.md）。
-- 只新建两张表：旧版本应用不读写它们。
-- 私密圈邀请制（#169）：邀请链接 + 指定 email 邀请。只在 PG 内读写，不进 ES。
-- cdc: none
SET lock_timeout = '5s';

CREATE TABLE domains.circle_invite_link (
    id uuid DEFAULT uuidv7() NOT NULL,
    circle_id uuid NOT NULL,
    creator_id uuid NOT NULL,
    token_hash character varying(64) NOT NULL,
    expire_time timestamp with time zone NOT NULL,
    max_uses integer,
    used_count integer DEFAULT 0 NOT NULL,
    revoked smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_by uuid,
    CONSTRAINT circle_invite_link_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.circle_invite_link IS '私密圈邀请链接(圈主/管理员生成,持链接的登录用户可加入;明文token只在创建响应中出现一次)';

COMMENT ON COLUMN domains.circle_invite_link.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.circle_invite_link.circle_id IS '圈子ID(circle.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.circle_invite_link.creator_id IS '创建人(user.id,无FK;创建时须为该圈圈主/管理员)';

COMMENT ON COLUMN domains.circle_invite_link.token_hash IS 'token的SHA-256(小写hex,64字符);token为32字节随机数的base64url编码,库内不存明文';

COMMENT ON COLUMN domains.circle_invite_link.expire_time IS '过期时间(创建时间+有效期,默认7天,最长30天)';

COMMENT ON COLUMN domains.circle_invite_link.max_uses IS '最大使用次数;NULL=不限次数';

COMMENT ON COLUMN domains.circle_invite_link.used_count IS '已使用次数(接受邀请时条件UPDATE原子递增,不超过max_uses;已是成员不消耗)';

COMMENT ON COLUMN domains.circle_invite_link.revoked IS '是否已撤销:0=否,1=是(手动撤销或圈子改为公开时批量撤销)';

COMMENT ON COLUMN domains.circle_invite_link.create_time IS '创建时间';

COMMENT ON COLUMN domains.circle_invite_link.update_time IS '更新时间(应用层维护)';

COMMENT ON COLUMN domains.circle_invite_link.update_by IS '最后修改人(user.id,无FK;撤销操作人);未修改为NULL';

CREATE UNIQUE INDEX uk_circle_invite_link_token_hash ON domains.circle_invite_link USING btree (token_hash);

CREATE INDEX idx_circle_invite_link_circle ON domains.circle_invite_link USING btree (circle_id, create_time DESC);

CREATE TABLE domains.circle_invite_email (
    id uuid DEFAULT uuidv7() NOT NULL,
    circle_id uuid NOT NULL,
    inviter_id uuid NOT NULL,
    email character varying(100) NOT NULL,
    status smallint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_by uuid,
    CONSTRAINT circle_invite_email_pkey PRIMARY KEY (id)
);

COMMENT ON TABLE domains.circle_invite_email IS '私密圈指定email邀请(圈主/管理员录入邮箱,对应users.email的用户登录后确认加入)';

COMMENT ON COLUMN domains.circle_invite_email.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';

COMMENT ON COLUMN domains.circle_invite_email.circle_id IS '圈子ID(circle.id,无FK,应用层保证)';

COMMENT ON COLUMN domains.circle_invite_email.inviter_id IS '邀请人(user.id,无FK;最近一次添加/重新邀请的圈主或管理员)';

COMMENT ON COLUMN domains.circle_invite_email.email IS '被邀请邮箱(存储前trim+小写,与users.email等值匹配;可尚无对应注册用户)';

COMMENT ON COLUMN domains.circle_invite_email.status IS '状态:0=待接受,1=已接受,2=已撤销,3=已拒绝;已撤销/已拒绝再次添加时重置为0';

COMMENT ON COLUMN domains.circle_invite_email.create_time IS '创建时间';

COMMENT ON COLUMN domains.circle_invite_email.update_time IS '更新时间(应用层维护)';

COMMENT ON COLUMN domains.circle_invite_email.update_by IS '最后修改人(user.id,无FK;撤销/重新邀请的管理者或接受/拒绝的被邀请人);未修改为NULL';

CREATE UNIQUE INDEX uk_circle_invite_email_circle_email ON domains.circle_invite_email USING btree (circle_id, email);

CREATE INDEX idx_circle_invite_email_pending ON domains.circle_invite_email USING btree (email, create_time DESC) WHERE (status = 0);
