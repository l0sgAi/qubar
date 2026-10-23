# 帖子域(post)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 帖子表

```sql
DROP TABLE IF EXISTS domains.post;

CREATE TABLE domains.post (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 归属关系 (核心外键)
    circle_id UUID NOT NULL,          -- 所属圈子ID
    user_id UUID NOT NULL,            -- 发帖人ID

    -- 2. 内容主体
    type SMALLINT NOT NULL DEFAULT 1,   -- 帖子类型：1=图文, 2=纯视频, 3=投票/链接
    title VARCHAR(200) NOT NULL DEFAULT '', -- 标题 (允许为空，适配微博/朋友圈式的短内容)
    summary VARCHAR(2048) NOT NULL DEFAULT '', -- 摘要 (纯文本，用于推送/列表预览，去除了HTML标签)
    content TEXT NOT NULL DEFAULT '',   -- 正文 (富文本/Mardown/HTML)

    -- 3. 多媒体与扩展
    -- 存储图片URL列表、视频封面/地址、链接卡片信息等
    -- 结构示例: ["url1", "url2", "url3"]
    media_extra JSONB NOT NULL DEFAULT '[]'::JSONB,

    -- 4. 统计数据
    view_count INT NOT NULL DEFAULT 0,    -- 浏览量
    comment_count INT NOT NULL DEFAULT 0, -- 评论数
    like_count INT NOT NULL DEFAULT 0,    -- 点赞数
    collect_count INT NOT NULL DEFAULT 0, -- 收藏数
    hot INT NOT NULL DEFAULT 0,   -- 热度分

    -- 5. 运营与状态标记
    is_pinned SMALLINT NOT NULL DEFAULT 0,  -- 是否置顶：0=否, 1=是 (圈内置顶)
    is_essence SMALLINT NOT NULL DEFAULT 0, -- 是否加精：0=否, 1=是
    is_lock SMALLINT NOT NULL DEFAULT 0,    -- 是否锁定：0=否, 1=是 (禁止评论)

    -- 6. 审核与生命周期
    -- 状态：0=草稿, 1=发布(正常), 2=审核中(若开启先审后发), 3=审核失败, 4=被屏蔽(软删/违规)
    status SMALLINT NOT NULL DEFAULT 1,
    deleted SMALLINT DEFAULT 0, -- 用户自行删除：0=未删, 1=已删

    -- 7. 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_reply_time TIMESTAMPTZ -- 最后回复时间
);

-- --- 注释 ---
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

-- --- 索引优化 ---

-- 1. 【核心】外键关联索引
-- 场景：用户进入圈子详情页，PG 校验圈子是否存在
CREATE INDEX idx_post_circle ON domains.post(circle_id);

-- 2. 【用户】"我的帖子" 列表
-- 场景：用户中心查看发布历史。这个通常直接走数据库，因为不仅要看正常的，还要看草稿/审核中的
CREATE INDEX idx_post_user ON domains.post(user_id, status, create_time DESC);

-- 3. 【管理】后台审核/管理索引
-- 场景：后台管理系统按状态筛选帖子进行人工审核
CREATE INDEX idx_post_status ON domains.post(status, create_time DESC);

-- 4. 【置顶】获取圈子置顶帖
-- 场景：置顶帖数量很少(3-5条)，通常不走ES，直接查DB置顶然后插在列表最前面
CREATE INDEX idx_post_pinned ON domains.post(circle_id) WHERE is_pinned = 1 AND deleted = 0;
```

## 帖子提及表

> 发帖时正文 @提及 的最终持久化名单（发帖人选择并经后端校验：存在且未注销、去重、
> 剔除作者本人、上限截断）。append-only：随帖子创建写入，不设 deleted（提及行随内容生灭）。
> 消息中心通知只对落库名单发；详情接口 `GET /post/detail` 据此回传 `mentions` 数组。

```sql
-- 帖子提及表 (post_mention)
DROP TABLE IF EXISTS domains.post_mention;

CREATE TABLE domains.post_mention (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    post_id UUID NOT NULL,           -- 被提及所在的帖子
    user_id UUID NOT NULL,           -- 被提及的用户

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.post_mention IS '帖子@提及表：发帖时的最终提及名单（去重/去作者/上限截断后）';
COMMENT ON COLUMN domains.post_mention.post_id IS '提及所在帖子ID(UUID)';
COMMENT ON COLUMN domains.post_mention.user_id IS '被提及用户ID(UUID)';

-- --- 索引优化 ---

-- 1. 【核心】一帖一用户仅一条提及记录；按 post_id 批量读（详情回传 mentions）
-- id 为主键升序读出（UUIDv7 字典序=时间序），近似正文出现顺序
CREATE UNIQUE INDEX uk_post_mention_post_user ON domains.post_mention(post_id, user_id);

-- 2. 【预留】"提及我的" 反查列表
CREATE INDEX idx_post_mention_user ON domains.post_mention(user_id, create_time DESC);
```

## 帖子 AI 审核日志表

> 权威 DDL：`migrations/20261023164208_add_post_moderation_log.up.sql`（#117）。append-only，不进 CDC / ES（`-- cdc: none`）。

| 字段 | 类型 | 说明 |
|---|---|---|
| id | uuid | 主键（UUIDv7，应用层 `sharedomain.NewID()` 生成） |
| post_id | uuid | 被审核帖子 ID（无 FK，应用层保证） |
| decision | varchar(16) | `publish` 发布 / `review` 人工待审 / `reject` 驳回 / `fail_open` 审核不可用按 fail_open 放行 |
| hazards | jsonb | 各风险概率（0–1），key 为 `domain.Hazard`；审核调用失败时为 `{}` |
| severity | double precision | 危害程度期望分（0–3）；失败时 NULL |
| severity_confidence | double precision | severity 置信度（0–1）；失败时 NULL |
| model | varchar(64) | 实际作答的模型版本号（如 `jev-1.13.0`） |
| input_tokens | integer | 计费输入 token 数 |
| latency_ms | integer | 审核调用总耗时（含重试等待） |
| error_msg | varchar(2048) | 审核调用 / 事件投递错误（成功为 NULL；不含 api_key） |
| create_time | timestamptz | 审核时间 |

- 索引：`idx_post_moderation_log_post (post_id, create_time DESC)`：worker 幂等检查（已有审计行即视为已审核）与按帖子查审核记录。
- 一帖通常只有一行；状态变更由 `post.status` 条件更新（`WHERE status = 2`）保证只发生一次。
