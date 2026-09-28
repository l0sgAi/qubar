# 帖子互动域(like/collect/view/interaction)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 帖子点赞表

```sql
-- 帖子点赞表 (post_like)
DROP TABLE IF EXISTS domains.post_like;

CREATE TABLE domains.post_like (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 点赞人
    post_id UUID NOT NULL,           -- 帖子ID (必填)

    -- 点赞状态 (0=有效点赞, 1=取消点赞)
    deleted SMALLINT NOT NULL DEFAULT 0,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.post_like IS '帖子点赞流水表';
COMMENT ON COLUMN domains.post_like.user_id IS '点赞人ID(UUID)';
COMMENT ON COLUMN domains.post_like.post_id IS '帖子ID(UUID)，便于查询';
COMMENT ON COLUMN domains.post_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';

-- --- 索引优化---

-- 1. 【核心】保证每个用户对每个帖子只有一条点赞/取消点赞的记录
-- ⚠️ 应用依赖：like 消费者 INSERT … ON CONFLICT (user_id, post_id)（#46），缺失则点赞无法落库，勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_post_like_user_post ON domains.post_like(user_id, post_id);

-- 2. 【核心】查询"我点赞过的帖子"
-- 当用户在个人中心查看"我赞过的帖子"时使用。
CREATE INDEX idx_user_post_active ON domains.post_like(user_id, create_time DESC) WHERE deleted = 0;

-- 3. 【统计/关联】查询某帖子的点赞者列表
-- 配合 `deleted=0` 查询有效点赞者。
CREATE INDEX idx_clike_post_active ON domains.post_like(post_id, create_time DESC) WHERE deleted = 0;
```

## 帖子收藏表

> 设计与 [`post_like`](#帖子点赞表) 完全对齐:同属「用户对帖子的二元状态」流水表,仅目标维度不同(收藏 vs 点赞)。
> 统计字段 `post.collect_count` 已在帖子表预留,Redis Hash `post:stats:{post_id}` 的 `collect_count` 字段、
> Redpanda `post_collect_count` 消费链路均已就绪--本表补齐「收藏流水 + 收藏者关系」最后一环。

```sql
-- 帖子收藏表 (post_collect)
DROP TABLE IF EXISTS domains.post_collect;

CREATE TABLE domains.post_collect (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 收藏人
    post_id UUID NOT NULL,           -- 被收藏的帖子ID (必填)

    -- 收藏状态 (0=有效收藏, 1=取消收藏)
    deleted SMALLINT NOT NULL DEFAULT 0,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.post_collect IS '帖子收藏流水表';
COMMENT ON COLUMN domains.post_collect.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';
COMMENT ON COLUMN domains.post_collect.user_id IS '收藏人ID(UUID)';
COMMENT ON COLUMN domains.post_collect.post_id IS '被收藏的帖子ID(UUID)';
COMMENT ON COLUMN domains.post_collect.deleted IS '收藏状态: 0=有效收藏, 1=取消收藏';
COMMENT ON COLUMN domains.post_collect.create_time IS '收藏时间(用于"我的收藏"按收藏时间倒序)';
COMMENT ON COLUMN domains.post_collect.update_time IS '更新时间(收藏/取消收藏切换时更新)';

-- --- 索引优化 ---

-- 1. 【核心】保证每个用户对每个帖子只有一条收藏/取消收藏的记录
-- ⚠️ 应用依赖：collect SetCollected 的 INSERT … ON CONFLICT (user_id, post_id)（#46），勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_post_collect_user_post ON domains.post_collect(user_id, post_id);

-- 2. 【核心】查询"我收藏过的帖子"
-- 场景：个人中心"我的收藏"页，按收藏时间倒序翻页。
-- (create_time, id) 组合可做 keyset 游标分页，避免深翻页 OFFSET 性能退化。
CREATE INDEX idx_pcollect_user_active ON domains.post_collect(user_id, create_time DESC, id DESC) WHERE deleted = 0;

-- 3. 【统计/关联】查询某帖子的收藏者列表 (通常只展示头像，不常翻页)
-- 配合 `deleted=0` 查询有效收藏者。
CREATE INDEX idx_pcollect_post_active ON domains.post_collect(post_id, create_time DESC) WHERE deleted = 0;
```

## 帖子浏览历史表

> 用户「最近浏览」历史。去重模型:每对 (user_id, post_id) 仅一行,再看时 bump `update_time` + `view_count`。
> 与 [`post_collect`](#帖子收藏表) 同构,差异:无 `deleted` 列(浏览历史无 toggle 语义,「清空历史」= 硬 DELETE);
> 多 `view_count`(浏览次数);排序键 `update_time`(= 最近浏览时间),列表按其倒序。
>
> 读写策略:Redis ZSET 即时读(key `user:view:posts:{user_id}`,score=访问时间,cap 500)
> + Redpanda MQ 异步聚合落库(复用 like/collect 事件链路,consumer 批量 `ON CONFLICT` upsert 本表)
> + DB 冷启动回源(ZSET 过期后按 `update_time` 倒序取 top 500 回填 ZSET)。

```sql
-- 帖子浏览历史表 (post_view_history)
DROP TABLE IF EXISTS domains.post_view_history;

CREATE TABLE domains.post_view_history (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 浏览人ID
    post_id UUID NOT NULL,           -- 被浏览的帖子ID (必填)

    -- 浏览统计 (展示"看过 N 次"用)
    view_count INT NOT NULL DEFAULT 1, -- 该帖被该用户浏览次数(MQ 聚合 +1)

    -- 时间字段
    -- update_time 兼作「最近浏览时间」,是「最近浏览」列表的排序键 + 冷启动回源排序键
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, -- 首次浏览时间
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP  -- 最近浏览时间(再看时 bump)
);

-- --- 注释 ---
COMMENT ON TABLE domains.post_view_history IS '帖子浏览历史表(去重模型:每对 user+post 一行,再看时 bump update_time+view_count)';
COMMENT ON COLUMN domains.post_view_history.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';
COMMENT ON COLUMN domains.post_view_history.user_id IS '浏览人ID(UUID)';
COMMENT ON COLUMN domains.post_view_history.post_id IS '被浏览的帖子ID(UUID)';
COMMENT ON COLUMN domains.post_view_history.view_count IS '该帖被该用户浏览次数(MQ 聚合 +1)';
COMMENT ON COLUMN domains.post_view_history.create_time IS '首次浏览时间';
COMMENT ON COLUMN domains.post_view_history.update_time IS '最近浏览时间(再看时更新,「最近浏览」列表排序键 + 冷启动回源排序键)';

-- --- 索引优化 ---

-- 1. 【核心】去重约束:每对 (user_id, post_id) 仅一行,支撑 MQ consumer 的 ON CONFLICT upsert
CREATE UNIQUE INDEX uk_pviewhist_user_post ON domains.post_view_history(user_id, post_id);

-- 2. 【核心】冷启动回源:ZSET 过期后按 update_time 倒序取 top 500 回填 ZSET
CREATE INDEX idx_pviewhist_user_time ON domains.post_view_history(user_id, update_time DESC, id DESC);
```

## 帖子互动表（CF 协同过滤物化矩阵）

> 用户×帖子交互矩阵，item-based 协同过滤的隐反馈评分表。设计见 docs/design/cf-item-based-design.md。
> 5 张交互事实表（post_like/post_collect/comment/comment_like/post_view_history）的事件驱动双写聚合而来：
> 各 event publisher 额外发 `post_interaction` topic -> InteractionConsumer 批量 `ON CONFLICT` upsert 本表。
>
> - PK (user_id, post_id)：一对一行，weight = 历史最强信号（max-ever），ts = 最近互动时间。
> - 不带 deleted/active：取消赞/收藏**不删行**（隐反馈哲学：瞬时点赞仍是兴趣信号）。
> - 失效帖（删/封）不主动清理：CF 读路径用 SearchPostsByIDs 过滤，自然不展示。
> - 时间窗：CF 共现计算回溯 `interaction_window_days`（默认 90 天）；超过 `cleanup_days`（默认 120 天）的行定期 DELETE。

```sql
-- 帖子互动表 (post_interaction) -- CF 协同过滤交互矩阵
DROP TABLE IF EXISTS domains.post_interaction;

CREATE TABLE domains.post_interaction (
    user_id UUID        NOT NULL,                     -- 互动用户ID
    post_id UUID        NOT NULL,                     -- 被互动帖子ID
    weight  SMALLINT    NOT NULL,                     -- 该 user-post 对最强信号强度（1..5：view1/comment_like2/like3/comment4/collect5）
    ts      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, -- 最近一次互动时间（CF 共现回溯窗口 + 清理锚点）
    PRIMARY KEY (user_id, post_id)
);

COMMENT ON TABLE domains.post_interaction IS '用户×帖子交互矩阵（item-based CF 隐反馈评分表，事件驱动双写聚合）';
COMMENT ON COLUMN domains.post_interaction.user_id IS '互动用户ID(UUID)';
COMMENT ON COLUMN domains.post_interaction.post_id IS '被互动帖子ID(UUID)';
COMMENT ON COLUMN domains.post_interaction.weight IS '该 user-post 对最强信号强度（1..5：view1/comment_like2/like3/comment4/collect5）';
COMMENT ON COLUMN domains.post_interaction.ts IS '最近一次互动时间（CF 共现回溯窗口 + 清理锚点）';

-- --- 索引优化 ---

-- 1. CF 共现自连接：按 post_id 取该帖所有互动者
CREATE INDEX idx_post_interaction_post    ON domains.post_interaction (post_id);

-- 2. 候选筛选 / 时间窗扫描
CREATE INDEX idx_post_interaction_post_ts ON domains.post_interaction (post_id, ts DESC);

-- 3. 清理：按 ts 删除过期行
CREATE INDEX idx_post_interaction_ts      ON domains.post_interaction (ts);
```
