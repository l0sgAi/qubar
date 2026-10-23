# 消息中心域（notice）

> 全局约定（UUIDv7 主键、无 FK/CHECK、逻辑删除）见 [README.md](README.md)。
> 设计文档：[docs/notice-design.md](../notice-design.md)。

## 站内通知表

> 用户对用户行为（赞/收藏/评论/回复/@提及）产生的站内通知。
> 写路径：4 触发域（like/collect/comment/post）发 `notification_events` Redpanda 事件
> → NotificationEventConsumer 批量反查接收人 + 规则过滤 + `ON CONFLICT` upsert 本表。
> 读路径：notice 域 keyset 翻页（`ORDER BY id DESC`，UUIDv7 字典序==时间序）。
>
> - 去重模型：每对 (recipient, actor, type, target) 仅一行，重复触发（re-like）upsert 复用行 + 重置未读。
> - 取消赞/收藏**不回收**通知（发出即留；负向事件触发端不发布）。
> - `snippet` 快照入库，读侧不反查 live 内容；目标删除后通知保留（R7）。

```sql
-- 站内通知表 (notification)
DROP TABLE IF EXISTS domains.notification;

CREATE TABLE domains.notification (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    recipient_id UUID NOT NULL,           -- 接收人(内容作者/被提及人)
    actor_id UUID NOT NULL,               -- 触发人
    notice_type SMALLINT NOT NULL,        -- 通知类型(枚举见注释)
    post_id UUID,                         -- 跳转帖子ID (可空)
    comment_id UUID,                      -- 跳转评论ID (可空)

    -- 展示
    snippet VARCHAR(200) NOT NULL DEFAULT '',  -- 内容快照
    is_read SMALLINT NOT NULL DEFAULT 0,       -- 0=未读, 1=已读

    deleted SMALLINT NOT NULL DEFAULT 0,
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.notification IS '站内通知表(消息中心, Redpanda notification_events 事件驱动写入)';
COMMENT ON COLUMN domains.notification.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底); 字典序=时间序, keyset 翻页排序键';
COMMENT ON COLUMN domains.notification.recipient_id IS '接收人ID(UUID): 内容作者/被提及人';
COMMENT ON COLUMN domains.notification.actor_id IS '触发人ID(UUID)';
COMMENT ON COLUMN domains.notification.notice_type IS '通知类型: 1=帖子被赞 2=评论被赞 3=帖子被收藏 4=帖子被评论 5=评论被回复 6=@提及 7=帖子审核通过 8=帖子审核驳回';
COMMENT ON COLUMN domains.notification.post_id IS '跳转帖子ID(UUID,可空)';
COMMENT ON COLUMN domains.notification.comment_id IS '跳转评论ID(UUID,可空)';
COMMENT ON COLUMN domains.notification.snippet IS '内容快照(评论正文前100字符/帖子标题), 目标删除后通知仍可读';
COMMENT ON COLUMN domains.notification.is_read IS '已读状态: 0=未读, 1=已读; 重复触发(重赞)时重置为0';
COMMENT ON COLUMN domains.notification.create_time IS '首次通知时间';
COMMENT ON COLUMN domains.notification.update_time IS '最近触发/已读操作时间';

-- --- 索引优化 ---

-- 1. 【核心】通知列表: 按接收人 + id 倒序 keyset 翻页 (notice_type 过滤走索引内 filter)
CREATE INDEX idx_notice_recipient_id ON domains.notification(recipient_id, id DESC) WHERE deleted = 0;

-- 2. 【核心】未读计数回源: 缓存 miss 时 COUNT 未读
CREATE INDEX idx_notice_recipient_unread ON domains.notification(recipient_id, is_read) WHERE deleted = 0;

-- 3. 【核心】幂等去重锚点: consumer ON CONFLICT upsert
-- 可空列 COALESCE 零值 UUID 纳入唯一键(NULL 不参与唯一性比较, 需表达式);
-- 重复投递/重赞命中同一行 → DO UPDATE 重置未读, 不产生第二行。
CREATE UNIQUE INDEX uk_notice_dedup ON domains.notification(
    recipient_id, actor_id, notice_type,
    COALESCE(post_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid)
) WHERE deleted = 0;
```

## 应用义务（DB 不强制，应用层保证）

- `notice_type` 枚举合法性（1-8；7 / 8 为人工审核结果，#118）：consumer 按事件 type 映射，非法丢弃。
- `recipient_id != actor_id`：consumer R1 过滤。
- mention 上限截断（`notice.mention_max`，默认 10）：post/comment application 层。
- 软删过滤 `deleted = 0`：所有查询手动带。
- 建表后授权：`GRANT SELECT, INSERT, UPDATE, DELETE ON domains.notification TO qubar_web_app;`（默认权限已配可省，见 README 附录）。
