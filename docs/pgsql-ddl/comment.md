# 评论域(comment)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 评论表

> 二级扁平化结构:`root_id` 为 NULL 表示顶层评论;回复时 `root_id` 指向所属顶层评论。
> keyset 游标分页的 `id` 字典序比较:UUIDv7 下 `id < ?` 等价「更早创建」,与 `ORDER BY id DESC` 配合正确。

```sql
DROP TABLE IF EXISTS domains.comment;

CREATE TABLE domains.comment (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 归属关系
    post_id UUID NOT NULL,                    -- 所属帖子ID
    user_id UUID NOT NULL,                    -- 发布者ID

    -- 2. 结构关系 (二级扁平化)
    root_id UUID,                             -- 根评论ID (NULL=顶层，否则指向所属顶层评论ID)
    reply_to_id UUID,                         -- 被回复评论ID (NULL=非回复特定评论)
    reply_to_user_id UUID,                    -- 被回复用户ID (NULL=非回复特定用户)

    -- 3. 内容
    content TEXT NOT NULL,                       -- 评论文本内容
    extra_data JSONB DEFAULT '{}'::JSONB,        -- 扩展数据：富文本结构/图片/视频JSON (预留)

    -- 4. 统计数据 (高频更新字段)
    like_count INT NOT NULL DEFAULT 0,          -- 点赞数
    reply_count INT NOT NULL DEFAULT 0,         -- 子评论数

    -- 5. 状态与时间
    status SMALLINT NOT NULL DEFAULT 1,         -- 1=正常, 2=审核中, 3=隐藏
    deleted SMALLINT DEFAULT 0,                 -- 0=正常, 1=已删除
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 添加表注释
COMMENT ON TABLE domains.comment IS '评论表：统一存储评论的元数据和内容，PostgreSQL TOAST机制自动处理大字段存储';

-- 添加列注释
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

-- 索引设计 (根据查询模式优化)
-- 场景：查询某个帖子下的顶层评论，按点赞数量倒序 (root_id IS NULL 表示顶层)
CREATE INDEX idx_comment_post_root_like ON domains.comment (post_id, root_id, like_count DESC);
-- 场景：查询某个帖子下的顶层评论，按时间倒序
CREATE INDEX idx_comment_post_root_time ON domains.comment (post_id, root_id, create_time DESC);

-- 场景：查询用户的历史评论
CREATE INDEX idx_comment_user_time ON domains.comment (user_id, create_time DESC);

-- 场景：查询某个根评论下的所有子回复 (root_id 非空)
CREATE INDEX idx_comment_root_id ON domains.comment (root_id) WHERE root_id IS NOT NULL;
```

## 评论点赞表

```sql
-- 评论点赞表 (comment_like)
DROP TABLE IF EXISTS domains.comment_like;

CREATE TABLE domains.comment_like (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 点赞人
    comment_id UUID NOT NULL,         -- 被点赞的评论

    -- 冗余字段 (可选，但推荐)
    post_id UUID,  -- 冗余帖子ID，NULL表示仅评论点赞未冗余，有助于查询

    -- 点赞状态 (0=有效点赞, 1=取消点赞)
    deleted SMALLINT NOT NULL DEFAULT 0,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.comment_like IS '评论点赞流水表';
COMMENT ON COLUMN domains.comment_like.user_id IS '点赞人ID(UUID)';
COMMENT ON COLUMN domains.comment_like.comment_id IS '被点赞评论ID(UUID)';
COMMENT ON COLUMN domains.comment_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';
COMMENT ON COLUMN domains.comment_like.post_id IS '冗余帖子ID(UUID)，便于查询';

-- --- 索引优化 ---

-- 1. 【核心】保证每个用户对每个评论只有一条点赞/取消点赞的记录
-- ⚠️ 应用依赖：like 消费者 INSERT … ON CONFLICT (user_id, comment_id)（#46），缺失则评论点赞无法落库，勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_comment_like_user_comment ON domains.comment_like(user_id, comment_id);

-- 2. 【核心】查询"我点赞过的评论"
-- 当用户在个人中心查看"我赞过的评论"时使用。
CREATE INDEX idx_clike_user_active ON domains.comment_like(user_id, create_time DESC) WHERE deleted = 0;

-- 3. 【统计/关联】查询某评论的点赞者列表 (通常只显示头像，不常翻页)
-- 配合 `deleted=0` 查询有效点赞者。
CREATE INDEX idx_clike_comment_active ON domains.comment_like(comment_id, create_time DESC) WHERE deleted = 0;
```

## 评论提及表

> 发评论/回复时正文 @提及 的最终持久化名单（评论人选择并经后端校验：存在且未注销、
> 去重、剔除评论人本人、上限截断）。append-only：随评论创建写入，不设 deleted（提及行
> 随内容生灭）。消息中心通知只对落库名单发；`GET /comment/list`、`GET /comment/replies`
> 据此按条回传 `mentions` 数组。

```sql
-- 评论提及表 (comment_mention)
DROP TABLE IF EXISTS domains.comment_mention;

CREATE TABLE domains.comment_mention (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    comment_id UUID NOT NULL,        -- 被提及所在的评论
    user_id UUID NOT NULL,           -- 被提及的用户

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.comment_mention IS '评论@提及表：发评论时的最终提及名单（去重/去评论人/上限截断后）';
COMMENT ON COLUMN domains.comment_mention.comment_id IS '提及所在评论ID(UUID)';
COMMENT ON COLUMN domains.comment_mention.user_id IS '被提及用户ID(UUID)';

-- --- 索引优化 ---

-- 1. 【核心】一评论一用户仅一条提及记录；按 comment_id 批量读（列表按条回传 mentions）
-- id 为主键升序读出（UUIDv7 字典序=时间序），近似正文出现顺序
CREATE UNIQUE INDEX uk_comment_mention_comment_user ON domains.comment_mention(comment_id, user_id);

-- 2. 【预留】"提及我的" 反查列表
CREATE INDEX idx_comment_mention_user ON domains.comment_mention(user_id, create_time DESC);
```
