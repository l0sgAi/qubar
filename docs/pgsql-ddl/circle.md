# 圈子域(circle)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 分类表

> **种子数据 ID 必须固化**(下方 INSERT 显式指定固定 UUIDv7),不可每次随机--否则重建库后 ID 变化,导致前端/配置缓存错乱。

```sql
DROP TABLE IF EXISTS domains.category;

CREATE TABLE domains.category (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心信息
    name VARCHAR(50) NOT NULL, -- 分类名称 (如：科技、生活)
    slug VARCHAR(60), -- SEO友好标识 (如：tech, life)
    icon VARCHAR(500), -- 图标/Icon URL

    -- 层级结构 (支持无限级或二级分类)
    parent_id UUID, -- 父分类ID，NULL表示顶级分类

    -- 排序与统计
    sort INT NOT NULL DEFAULT 0, -- 排序权重 (数值越大越靠前，或越小越靠前，由业务定义)
    circle_count INT NOT NULL DEFAULT 0, -- 该分类下的圈子数量 (缓存字段，用于展示 "科技(102)")

    -- 状态
    status SMALLINT NOT NULL DEFAULT 1, -- 0=禁用/隐藏，1=启用/显示
    deleted SMALLINT DEFAULT 0, -- 逻辑删除

    -- 时间 (用于同步)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 添加注释 ---
COMMENT ON TABLE domains.category IS '圈子分类表';
COMMENT ON COLUMN domains.category.id IS '分类ID(UUIDv7)';
COMMENT ON COLUMN domains.category.name IS '分类名称';
COMMENT ON COLUMN domains.category.slug IS 'URL标识符';
COMMENT ON COLUMN domains.category.icon IS '分类图标';
COMMENT ON COLUMN domains.category.parent_id IS '父分类ID(NULL为顶级)';
COMMENT ON COLUMN domains.category.sort IS '排序权重(人工控制顺序)';
COMMENT ON COLUMN domains.category.circle_count IS '该分类下的圈子数(统计缓存)';
COMMENT ON COLUMN domains.category.status IS '状态:0=隐藏,1=显示';
COMMENT ON COLUMN domains.category.deleted IS '逻辑删除';
COMMENT ON COLUMN domains.category.update_time IS '更新时间(用于ES同步)';

-- --- 索引优化 (Index) ---

-- 1. 【必须】唯一性约束
CREATE UNIQUE INDEX idx_category_name ON domains.category(name) WHERE deleted = 0;
CREATE UNIQUE INDEX idx_category_slug ON domains.category(slug) WHERE deleted = 0;

-- 2. 【核心】列表查询索引
-- 场景：APP首页获取全部分类列表，通常按权重排序
-- SQL: SELECT * FROM category WHERE parent_id IS NULL AND status=1 AND deleted=0 ORDER BY sort DESC
CREATE INDEX idx_category_list ON domains.category(parent_id, sort DESC) WHERE deleted = 0 AND status = 1;

-- 插入顶级分类数据 (id 使用固化 UUIDv7，同一毫秒前缀 + 递增尾数)
INSERT INTO domains.category (id, name, slug, icon, parent_id, sort, circle_count) VALUES
-- 第一梯队：超高热度/刚需
('019058b0-5b40-7000-8000-000000000001', '科技数码', 'tech', NULL, NULL, 100, 0),
('019058b0-5b40-7000-8000-000000000002', '游戏电竞', 'gaming', NULL, NULL, 95, 0),
('019058b0-5b40-7000-8000-000000000003', '生活日常', 'lifestyle', NULL, NULL, 90, 0),
('019058b0-5b40-7000-8000-000000000004', '二次元', 'acg', NULL, NULL, 85, 0),
('019058b0-5b40-7000-8000-000000000005', '影音娱乐', 'entertainment', NULL, NULL, 80, 0), -- 电影、电视剧、综艺、音乐
('019058b0-5b40-7000-8000-000000000006', '流行文化', 'pop-culture', NULL, NULL, 79, 0),

('019058b0-5b40-7000-8000-000000000007', '运动健身', 'sports', NULL, NULL, 75, 0),
('019058b0-5b40-7000-8000-000000000008', '美食寻味', 'food', NULL, NULL, 70, 0),
('019058b0-5b40-7000-8000-000000000009', '萌宠动物', 'pets', NULL, NULL, 65, 0),
('019058b0-5b40-7000-8000-000000000010', '旅行户外', 'travel', NULL, NULL, 60, 0), -- 露营、旅游
('019058b0-5b40-7000-8000-000000000011', '汽车出行', 'cars', NULL, NULL, 55, 0), -- 汽车、摩托车

('019058b0-5b40-7000-8000-000000000012', '财经投资', 'finance', NULL, NULL, 50, 0), -- 股票、基金、理财
('019058b0-5b40-7000-8000-000000000013', '职场发展', 'career', NULL, NULL, 45, 0),
('019058b0-5b40-7000-8000-000000000014', '知识科普', 'knowledge', NULL, NULL, 40, 0), -- 硬核知识、百科
('019058b0-5b40-7000-8000-000000000015', '校园教育', 'education', NULL, NULL, 38, 0), -- 升学、考研、校园生活

('019058b0-5b40-7000-8000-000000000016', '阅读文学', 'reading', NULL, NULL, 35, 0), -- 书籍、网文
('019058b0-5b40-7000-8000-000000000017', '摄影摄像', 'photography', NULL, NULL, 30, 0),
('019058b0-5b40-7000-8000-000000000018', '时尚美妆', 'fashion', NULL, NULL, 25, 0),
('019058b0-5b40-7000-8000-000000000019', '艺术设计', 'art', NULL, NULL, 20, 0), -- 绘画、设计
('019058b0-5b40-7000-8000-000000000020', '家居房产', 'home', NULL, NULL, 15, 0), -- 装修、租房

('019058b0-5b40-7000-8000-000000000021', '亲子育儿', 'parenting', NULL, NULL, 10, 0),
('019058b0-5b40-7000-8000-000000000022', '情感心理', 'emotion', NULL, NULL, 5, 0),
('019058b0-5b40-7000-8000-000000000023', '新闻政治', 'history-politic', NULL, NULL, 4, 0),
('019058b0-5b40-7000-8000-000000000024', '历史人文', 'history-humannity', NULL, NULL, 3, 0),
('019058b0-5b40-7000-8000-000000000025', '其它兴趣', 'others', NULL, NULL, 2, 0),
('019058b0-5b40-7000-8000-000000000026', '小众猎奇', 'niche-exotic', NULL, NULL, 1, 0);
```

## 兴趣圈表

```sql
DROP TABLE IF EXISTS domains.circle;

CREATE TABLE domains.circle (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    name VARCHAR(50) NOT NULL, -- 兴趣圈名称
    slug VARCHAR(60), -- 唯一标识符(用于URL SEO，如 /circle/coding-life)，允许为空
    avatar_url VARCHAR(500), -- 兴趣圈头像url
    cover_url VARCHAR(500), -- 背景图url
    description TEXT NOT NULL, -- 描述信息
    rule TEXT NOT NULL, -- 圈内规则

    -- 3. 归属与分类
    creator_id UUID NOT NULL, -- 创建人ID (关联用户表)
    category_id UUID, -- 分类ID (关联分类表)，NULL表示未分类

    -- 4. 统计数据 (反范式设计，避免频繁 Count 查询)
    hot INT NOT NULL DEFAULT 0, -- 热度值 (计算所得)
    member_count INT NOT NULL DEFAULT 0, -- 成员数量
    post_count INT NOT NULL DEFAULT 0, -- 帖子数量

    -- 5. 状态与权限
    join_type SMALLINT NOT NULL DEFAULT 0, -- 加入方式：0=公开(直接加入)，2=私密(邀请制)；1=需审核已下线(#168，存量迁移为 2，不再写入)
    status SMALLINT NOT NULL DEFAULT 1, -- 状态：0=审核中，1=正常，2=被封禁/冻结

    deleted SMALLINT DEFAULT 0, -- 0=正常，1=已删除

    -- 时间字段 (去掉了 ON UPDATE，由代码控制)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- --- 添加注释 (PostgreSQL 标准写法) ---
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
COMMENT ON COLUMN domains.circle.join_type IS '加入限制：0=公开，2=私密(邀请制)；1=审核已下线';
COMMENT ON COLUMN domains.circle.status IS '圈子状态：0=待审，1=正常，2=封禁';
COMMENT ON COLUMN domains.circle.deleted IS '逻辑删除：0=未删，1=已删';
COMMENT ON COLUMN domains.circle.create_time IS '创建时间';
COMMENT ON COLUMN domains.circle.update_time IS '更新时间';

-- --- 创建索引 (Index) ---

-- 1. 唯一性约束
-- 圈子名称不允许重复，且只检测未删除的记录
CREATE UNIQUE INDEX idx_circle_name ON domains.circle(name) WHERE deleted = 0;
-- 如果使用了 slug 用于 URL 访问
CREATE UNIQUE INDEX idx_circle_slug ON domains.circle(slug) WHERE deleted = 0;
```

## 成员权限表

```sql
DROP TABLE IF EXISTS domains.circle_member;

CREATE TABLE domains.circle_member (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    circle_id UUID NOT NULL, -- 圈子ID
    user_id UUID NOT NULL, -- 用户ID

    -- 角色与权限 (RBAC核心)
    -- 10=普通成员, 20=管理员, 30=圈主 (使用间隔数字，方便未来插入"副圈主"或"嘉宾")
    role SMALLINT NOT NULL DEFAULT 10,

    -- 成员状态 (控制访问权)
    -- 1=正常, 2=禁言(Muted), 3=拉黑(Banned), 4=已退出(主动退圈 / 被移出 / 解除拉黑)
    -- 0=待审核(申请中)已下线(#168，存量迁移为 4，不再写入)
    status SMALLINT NOT NULL DEFAULT 1,
    -- 成员声望 帖子/评论被点赞+1 帖子被收藏+5 帖子/评论被别人回复+10
    reputation INT NOT NULL DEFAULT 0,

    -- 禁言控制 (结合 status=2 使用)
    mute_end_time TIMESTAMPTZ, -- 禁言结束时间，NULL或过去时间代表无禁言

    -- 个性化设置 (用户对该圈子的设置)
    is_top SMALLINT DEFAULT 0, -- 是否置顶显示 (0=否, 1=是)
    is_disturb SMALLINT DEFAULT 0, -- 消息免打扰 (0=否, 1=是)

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, -- 加入时间
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP  -- 角色/状态变更时间
);

-- --- 注释 ---
COMMENT ON TABLE domains.circle_member IS '圈子成员关系与权限表';
COMMENT ON COLUMN domains.circle_member.circle_id IS '圈子ID(UUID)';
COMMENT ON COLUMN domains.circle_member.user_id IS '用户ID(UUID)';
COMMENT ON COLUMN domains.circle_member.role IS '角色: 10=成员, 20=管理员, 30=圈主';
COMMENT ON COLUMN domains.circle_member.status IS '状态: 1=正常, 2=禁言, 3=拉黑, 4=已退出/被移出；0=申请中已下线';
COMMENT ON COLUMN domains.circle_member.mute_end_time IS '禁言截止时间';

-- --- 索引优化---

-- 1. 【必须】唯一性约束 (防止重复加入)
-- 每个用户在同一个圈子只能有一条记录
CREATE UNIQUE INDEX idx_member_unique ON domains.circle_member(circle_id, user_id);

-- 2. 【核心】查询 "我加入的圈子"
-- 场景：APP "我的" 页面，或校验用户发帖权限
-- 包含 role 和 status 是为了覆盖索引(Covering Index)，查权限时不回表
CREATE INDEX idx_member_user ON domains.circle_member(user_id, role, status);

-- 3. 【管理】查询 "圈子的管理员列表" 或 "圈子成员列表"
-- 场景：圈主管理成员，或者展示成员列表
CREATE INDEX idx_member_circle_role ON domains.circle_member(circle_id, role DESC, create_time DESC);
```

## 邀请表（#169）

> 权威 DDL 见 `migrations/20261030113603_add_circle_invite.up.sql`，这里只说明字段语义。两张表都只在 PG 内读写，不进 CDC / ES。

### `domains.circle_invite_link` 邀请链接

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uuid | UUIDv7，应用层 `sharedomain.NewID()` 生成 |
| `circle_id` | uuid | 圈子 ID（仅私密圈可创建） |
| `creator_id` | uuid | 创建人（创建时须为圈主 / 管理员） |
| `token_hash` | varchar(64) | token 的 SHA-256 小写 hex；token = 32 字节随机数的 base64url（无填充），明文只在创建响应中出现一次 |
| `expire_time` | timestamptz | 过期时间，默认创建后 7 天，最长 30 天 |
| `max_uses` | integer | 最大使用次数，NULL = 不限 |
| `used_count` | integer | 已用次数；接受时 `UPDATE ... WHERE max_uses IS NULL OR used_count < max_uses` 原子递增，已是成员不消耗 |
| `revoked` | smallint | 0 = 有效，1 = 已撤销（手动撤销，或私密圈改公开时批量撤销） |
| `create_time` / `update_time` / `update_by` | | 审计字段；`update_by` 为撤销人，`creator_id` 即创建人 |

索引：`uk_circle_invite_link_token_hash (token_hash)`（token 等值校验）、`idx_circle_invite_link_circle (circle_id, create_time DESC)`（管理端列表）。

### `domains.circle_invite_email` 指定 email 邀请

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uuid | UUIDv7，应用层生成 |
| `circle_id` | uuid | 圈子 ID（仅私密圈可添加） |
| `inviter_id` | uuid | 最近一次添加 / 重新邀请的圈主或管理员 |
| `email` | varchar(100) | 被邀请邮箱，存储前 trim + 小写，与 `users.email` 等值匹配；可以还没有对应注册用户 |
| `status` | smallint | 0 = 待接受，1 = 已接受，2 = 已撤销，3 = 已拒绝；已撤销 / 已拒绝再次添加时重置为 0 |
| `create_time` / `update_time` / `update_by` | | 审计字段；`update_by` 为最后操作人（管理者或被邀请人） |

索引：`uk_circle_invite_email_circle_email (circle_id, email)`（唯一 + 管理端列表）、`idx_circle_invite_email_pending (email, create_time DESC) WHERE status = 0`（`GET /circle/invite/my`）。

两张表都没有 `deleted` 列：生命周期由 `revoked` / `status` 表达，行不做删除。
