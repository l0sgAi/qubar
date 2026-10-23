# 消息中心（notice）设计

> 目标：用户对用户行为（赞/收藏/评论/回复/@提及）产生站内通知，内容作者即接收人。
> 技术选型（已拍板）：**Redpanda 承载通知事件流**，与统计/热度/CF 链路一致；
> 读侧为 notice 新域（列表/未读数/已读）。触发侧对业务动作零阻塞、best-effort。
> 基线：qubar DDD 模块化单体，现有 7 条 Redpanda topic 全部为统计/行为流。

## 一、现状盘点

### 1.1 触发点（全部现成，仅需加 publish 调用）

| 事件 | 触发点 | 触发时已知数据 |
|---|---|---|
| 帖子被赞 | `like/application/service.go:115` togglePostLike | actor、post_id、方向 ±1 |
| 评论被赞 | `like/application/service.go:153` toggleCommentLike | actor、comment_id、post_id、方向 |
| 帖子被收藏 | `collect/application/service.go:106` Toggle | actor、post_id、方向 |
| 帖子被评论 | `comment/application/service.go:190` CreateComment（RootID=nil） | actor、post_id、正文 |
| 评论被回复 | 同上（`replyToUserID` 已解析，`service.go:243`） | actor、comment_id、被回复人、正文 |
| @提及 | 无（需扩展 CreatePostInput/CreateCommentInput） | — |

### 1.2 接收人解析能力缺口

- `PostMeta`（`post/application/service.go:252`）**只有 ID/Status/IsLock，无作者**；like/collect 域的 `PostTarget` 端口也只有 `Exists/RestoreStats`。
- `GetCommentMeta`（`comment/application/service.go:134`）只回 postID，无评论作者。
- 结论：**触发端解析接收人需动 4 域端口**；消费端反查只需 consumer 直读 DB（现有 consumer 本来就这么干，见 `like_consumer.go:8-9` import commentdomain/postdomain 直写 DB）。

### 1.3 Redpanda 链路范式（新链路照抄）

- 每 topic 一套：专用 `kafka.Writer`（Async + Snappy + RequireOne，`producer.go`）+ `StartXxxConsumerWithRetry` + 聚合器 `{mu, deltas, ticker, stopChan, stopped}` 定时 flush + `jsonb_to_recordset` 批量落库（`like_consumer.go:142`）。
- 配置三件套：`xxx_topic` / `xxx_consumer_group` / `xxx_flush_interval`（`conf.go:137` Redpanda 结构体）。
- 启停挂在 `cmd/apps/server.go:70-210`（Init producer → go consumer → Close 成对）。
- publish 失败仅 log 不传播（`like/application/service.go:142`）——通知链路沿用此语义。

### 1.4 通知相关资产

无。无表、无 topic、无域。全新建。

## 二、技术选型评估（已定：Redpanda）

| 维度 | Redpanda（选定） | 进程内异步（已否） |
|---|---|---|
| 与现有架构一致性 | ✅ 第 8 条 topic，范式照搬 | 新增第 2 种回调模式（现有仅 AgentReplyTrigger） |
| 触发域改动 | 加 publish 调用 + publisher 端口 | 加 NoticeTrigger 端口 + composition 桥接 |
| 崩溃窗口 | publish 后的事件不丢；publish 失败窗口与进程内方案相同（都 best-effort） | 在途通知全丢 |
| 延迟 | flush 间隔秒级（用户已接受） | ms 级 |
| 吞吐 | 批量 upsert，触发侧 Async writer 零阻塞 | 每事件一次 DB 写 |

已接受权衡：通知延迟 = flush_interval（默认 5s）；不做 outbox（publish 失败即丢，与统计链路同哲学）。

## 三、最终方案

### 3.1 拓扑

- 新域 `pkg/domains/notice/`：**只负责读侧**（列表/未读数/已读）+ 实体定义。
- 新 topic `notification_events`：单一事件 schema 承载全部 6 类。
- 触发 4 域（like/collect/comment/post）：各自 domain 层声明 `NoticeEventPublisher` 小端口，infra 加 `notice_event_publisher.go` 薄适配器（委托 `redpanda.PublishNotificationEvent`），构造器注入（同域依赖，非跨域）。
- 消费端 `redpanda/notification_consumer.go`：**胖 consumer** —— 批量反查接收人/标题、应用业务规则、批量 upsert `domains.notification` + 累加未读计数。
- 接收人解析放消费端（确认项 #1）：触发域零端口改动，consumer 批量 IN 查询天然聚合，与现有 consumer 直读直写 DB 风格一致。

### 3.2 事件 schema

```go
// NotificationEventMessage 通知事件（topic: notification_events）。
// 负向动作（取消赞/收藏）触发端不发布（确认项 #2）。
type NotificationEventMessage struct {
    Type           string      `json:"type"` // like_post/like_comment/collect_post/comment_post/reply_comment/mention
    ActorID        uuid.UUID   `json:"actor_id"`
    PostID         *uuid.UUID  `json:"post_id,omitempty"`    // 跳转用；like_post/collect_post/comment_post/mention(post) 必填
    CommentID      *uuid.UUID  `json:"comment_id,omitempty"` // like_comment/comment_post/reply_comment/mention(comment) 必填
    MentionUserIDs []uuid.UUID `json:"mention_user_ids,omitempty"` // type=mention 专用（触发端已校验+截断）
    Snippet        string      `json:"snippet,omitempty"`    // comment 类：正文快照（已 SanitizeForPg）
    Ts             int64       `json:"ts"`
}
```

Kafka key = 目标 ID（post_id 或 comment_id），同目标事件保序。

### 3.3 消费端接收人解析与规则

flush 一批事件后：

1. **过滤**：非法 schema 丢弃（log）。
2. **批量反查**（各一次 IN 查询）：
   - `like_post` / `collect_post` / `comment_post` → `SELECT id, user_id, title FROM domains.post WHERE id IN (...)`（title 作 snippet，确认项 #4）
   - `like_comment` → `SELECT id, user_id FROM domains.comment WHERE id IN (...)`
   - `reply_comment` → `SELECT id, reply_to_user_id FROM domains.comment WHERE id IN (...)`（新评论的 reply_to_user_id 即接收人）
   - `mention` → 事件自带 mention_user_ids，不反查
3. **R1 自动作过滤**：recipient == actor 丢弃。
4. **R4 同人同事去重**：批内按 (recipient, comment_id) 分组，mention 优先于 reply_comment/comment_post，只留一条。
5. **R3 幂等 upsert**：唯一表达式索引锚点 `ON CONFLICT DO UPDATE SET is_read=0, snippet, update_time`（重赞复用行、重置未读；列表按 id 序位置不变，确认项 #7）。
6. **未读计数**：upsert 成功后按 recipient 聚合 `INCRBY notice:unread:{uid}`（确认项 #5）。
7. 目标已删（反查 miss）→ 丢弃该事件（内容都没了，通知无落点）。

### 3.4 业务规则汇总

| 规则 | 内容 | 落点 |
|---|---|---|
| R1 | 自动作不通知 | consumer |
| R2 | ~~取消回收~~（已否决：不回收，发出即留；负向事件触发端不发） | 触发端 |
| R3 | 同 (recipient, actor, type, target) upsert 复用行，重置未读 | consumer + 唯一索引 |
| R4 | 同评论既回复又 @ 同一人 → 只发 mention | consumer 批内去重 |
| R5 | 不聚合（一人一行） | — |
| R6 | snippet 快照入库，读侧不反查 live 内容 | consumer |
| R7 | 目标删除后通知保留（snippet 兜底可读，前端跳转 404 自理） | — |
| R8 | @ = 前端传 `mention_user_ids: []uuid`；后端 `UserFacade.GetBriefs` 校验存在、滤自己、截断上限（确认项 #3） | post/comment application |
| R9 | mention 载体：发帖 + 发评论/回复 均生效 | post/comment |

### 3.5 @提及契约（触发端）

- `CreatePostInput` / `CreateCommentInput` 加 `MentionUserIDs []uuid.UUID`（可选）。
- handler Request DTO 同步加 `mention_user_ids []string`，bind 后逐条 `uuid.Parse`，解析失败 → 400。
- application 层：去重 → 滤 actor 自己 → `UserFacade.GetBriefs` 校验存在（comment 域已有该 Facade，`service.go:34`；post 域已有）→ 截断到上限 → 随业务动作成功后 publish mention 事件。
- 正文内 `@username` 渲染串不解析、不校验，纯前端展示。

### 3.6 读侧 API（notice 域）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/notice/list?type=&cursor=&size=` | keyset 游标（base64 JSON `{id}`，`ORDER BY id DESC`，UUIDv7 序，仿 comment cursor）；type 空/0=全部，支持单值 `1`-`8`（7/8 为帖子人工审核结果，#118）或逗号分隔多值 `1,2`（分类 tab 聚合，`notice_type IN (...)`）；size `normalizeSize` 回落 20 |
| GET | `/notice/unread-count` | Redis 计数器，miss → DB COUNT 回填 |
| POST | `/notice/read` | `{ids: []string}` 批量已读（仅本人），DECR 计数器（floor 0） |
| POST | `/notice/read-all` | 全部已读，计数器 SET 0 |

列表 VO：`{id, type, actor{username, avatar_url}, post_id, comment_id, snippet, is_read, create_time}`。actor 展示用 `UserFacade.GetBriefs` 批量回填（notice 域声明端口，composition 桥接）。

### 3.7 未读计数（确认项 #5）

- key：`notice:unread:{uid}`（String 计数器，无 TTL），常量 + helper 加 `redis/constants.go`。
- 写：consumer upsert 后 `INCRBY`（每 recipient 聚合一次）。
- 读：GET → miss 则 `COUNT(*) WHERE recipient_id=? AND is_read=0 AND deleted=0` 回填。
- 已读：`/notice/read` 按实际更新行数 `DECRBY`（Lua floor 0）；`/notice/read-all` SET 0。
- 漂移接受：计数器是软信号，miss 即回源校正，无锁无单飞（同 stats 哲学）。

## 四、数据流

```
用户动作                     触发域 application                Redpanda                consumer                     DB/Redis
─────────  ────────────────────────────────────  ───────────────────  ─────────────────────────────  ─────────────────────
赞/收藏    like/collect.Toggle ──正向──┐
                                       ├─ publish(notification_events) ──► topic ──► 聚合器 ticker(5s) flush:
评论/回复  comment.CreateComment ──────┤   (Async writer, 失败仅 log)                  1. 批量反查 post/comment
                                       │                                            2. R1/R4 过滤去重
发帖@/评论@ post/comment.Create ───────┘                                            3. ON CONFLICT 批量 upsert ──► domains.notification
                                                                                    4. INCRBY 未读 ──────────► notice:unread:{uid}

读侧（同步，不过 MQ）:
GET /notice/list ──► notice repo keyset 翻页 ──► UserFacade.GetBriefs 回填 actor
GET /notice/unread-count ──► Redis(miss→DB 回填)     POST /notice/read[-all] ──► DB update + 计数器校正
```

## 五、Schema / 配置变更

### 5.1 新表（落地 `docs/pgsql-ddl/notice.md`，README 目录登记）

```sql
-- 通知表 (notification)
DROP TABLE IF EXISTS domains.notification;

CREATE TABLE domains.notification (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    recipient_id UUID NOT NULL,        -- 接收人(内容作者/被提及人)
    actor_id UUID NOT NULL,            -- 触发人
    notice_type SMALLINT NOT NULL,     -- 1=like_post 2=like_comment 3=collect_post 4=comment_post 5=reply_comment 6=mention
    post_id UUID,                      -- 跳转用帖子ID (可空)
    comment_id UUID,                   -- 跳转用评论ID (可空)

    -- 展示
    snippet VARCHAR(200) NOT NULL DEFAULT '',  -- 内容快照(评论正文前缀/帖子标题)
    is_read SMALLINT NOT NULL DEFAULT 0,       -- 0=未读 1=已读

    deleted SMALLINT NOT NULL DEFAULT 0,
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE domains.notification IS '站内通知表(消息中心, Redpanda 事件驱动写入)';
COMMENT ON COLUMN domains.notification.id IS '主键ID(UUIDv7, 应用层生成, DB默认值仅兜底); 字典序=时间序, keyset 翻页排序键';
COMMENT ON COLUMN domains.notification.recipient_id IS '接收人ID(UUID)';
COMMENT ON COLUMN domains.notification.actor_id IS '触发人ID(UUID)';
COMMENT ON COLUMN domains.notification.notice_type IS '通知类型: 1=帖子被赞 2=评论被赞 3=帖子被收藏 4=帖子被评论 5=评论被回复 6=@提及';
COMMENT ON COLUMN domains.notification.post_id IS '跳转帖子ID(UUID, 可空)';
COMMENT ON COLUMN domains.notification.comment_id IS '跳转评论ID(UUID, 可空)';
COMMENT ON COLUMN domains.notification.snippet IS '内容快照(评论正文前100字符/帖子标题), 目标删除后通知仍可读';
COMMENT ON COLUMN domains.notification.is_read IS '已读状态: 0=未读, 1=已读; 重复触发(re-like)时重置为0';

-- --- 索引 ---

-- 1. 【核心】通知列表: 按接收人 + id 倒序 keyset 翻页 (type 过滤走索引内 filter)
CREATE INDEX idx_notice_recipient_id ON domains.notification(recipient_id, id DESC) WHERE deleted = 0;

-- 2. 【核心】未读计数回源: COUNT 未读
CREATE INDEX idx_notice_recipient_unread ON domains.notification(recipient_id, is_read) WHERE deleted = 0;

-- 3. 【核心】幂等去重锚点: consumer ON CONFLICT upsert (可空列 COALESCE 零值 UUID 纳入唯一键)
CREATE UNIQUE INDEX uk_notice_dedup ON domains.notification(
    recipient_id, actor_id, notice_type,
    COALESCE(post_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid)
) WHERE deleted = 0;
```

授权：DB-owner 建表后 `GRANT SELECT, INSERT, UPDATE, DELETE ON domains.notification TO qubar_web_app;`（默认权限已配可省，见 README 附录）。

应用义务（DB 不强制，consumer/service 保证）：notice_type 枚举合法性、recipient≠actor、mention 上限截断、软删过滤。

### 5.2 配置（三处同步）

`configs/config.yaml` redpanda 节 + `conf.go` Redpanda 结构体：

```yaml
notice_event_topic: "notification_events"                    # 通知事件topic
notice_event_consumer_group: "notification_events_consumer_group"
notice_event_flush_interval: 5                               # 通知落库间隔(秒, 确认项 #8)
```

新 `Notice` 节（`conf.go` 加结构体 + AppConfig 字段）：

```yaml
notice:
  mention_max: 10    # 单条内容 @ 上限, <=0 兜底 10 (确认项 #3)
```

### 5.3 Redis key（`redis/constants.go`）

`notice:unread:{uid}` — String 计数器，无 TTL；consumer INCRBY / read 端点 DECRBY(floor 0) / 读 miss DB 回填。前缀 const + `GetNoticeUnreadKey` helper。

### 5.4 代码改动清单

| 位置 | 改动 |
|---|---|
| `redpanda/producer.go` | +notificationEventWriter: Init/PublishNotificationEvent/Close |
| `redpanda/notification_consumer.go` | 新建：aggregator + flush（反查/规则/upsert/INCRBY）+ WithRetry |
| `like/domain` + `like/infrastructure` | NoticeEventPublisher 端口 + 适配器；service 正向分支 publish（2 处） |
| `collect/domain` + `collect/infrastructure` | 同上（1 处） |
| `comment/` | input+port+publish（comment_post/reply_comment/mention 3 处）；handler DTO +mention_user_ids |
| `post/` | CreatePostInput+MentionUserIDs；port+publish（mention 1 处）；handler DTO |
| `notice/` 新域 | domain(实体+Repository+Cache) / application(Service+UserFacade 端口+errors) / infrastructure(repo_pg+cache_redis) / interfaces http |
| `pkg/composition` | newNoticeService + userFacade 桥接 + registerNotice |
| `cmd/apps/server.go` | Init producer + go consumer WithRetry + Close 成对 |
| `docs/pgsql-ddl/notice.md` + README 目录 | DDL 权威来源 |

## 六、一致性 / 边界 / 风险

| 项 | 风险 | 对策 |
|---|---|---|
| publish 失败 | 通知丢失（与统计链路同哲学） | log；接受。P2 可上 outbox |
| consumer 崩溃 | 事件在 topic，重启续消费；offset 已提交+flush 未完成 → 重复投递 | R3 唯一索引 upsert 天然幂等 |
| 未读计数漂移 | INCRBY 与 upsert 非原子；进程崩溃于两者之间 | 软信号；读 miss 回源校正；`/notice/read-all` SET 0 自愈 |
| 重复投递 | at-least-once | upsert 幂等；INCRBY 可能多加 → 同上漂移对策。upsert 用 `RETURNING (xmax = 0) AS inserted` 只对真新增 INCRBY，可压掉大部分重复计数 |
| 乱序 | 跨 partition 同人事件无序 | 业务无顺序依赖（upsert 幂等），接受 |
| mention 刷量 | 恶意 @ 大量用户 | 上限截断 + 存在性校验；P2 可加 rate limit |
| 机器人回环 | bot 评论触发通知 → 接收人是 bot 无意义 | bot 作 recipient 仍落库无害（确认项 #6）；actor==recipient 已滤 |
| 热帖风暴 | 万赞热帖 → 单 recipient 大量事件 | flush 批量 upsert 摊平；同 (actor,target) 批内 map 去重后只剩一条 |
| 列表深翻页 | OFFSET 退化 | keyset 游标，无 OFFSET |
| 通知表膨胀 | 只增不删 | P2 加保留策略（如 180 天物理清理 job） |

## 七、分阶段交付

| 期 | 内容 |
|---|---|
| P0 | DDL + notice 域读侧 4 端点 + topic/producer/consumer + 4 触发域 publish + mention 契约 + 未读计数。即本文档全部 |
| P1 | 通知偏好设置（按类型开关）、通知删除单条、WebSocket/SSE 实时推送红点 |
| P2 | outbox 零丢失、保留期清理 job、聚合展示（"A 等 N 人赞了你"）、邮件渠道 |

## 八、关键确认项（待逐条回复）

1. **接收人解析位置**：消费端批量反查（推荐：触发域零端口改动，consumer 风格一致）vs 触发端胖事件（需动 like/collect 端口加作者查询）。
2. **负向事件**：触发端不发（推荐：省流量； unlike 本就不回收通知）vs 照发 consumer 丢弃。
3. **mention 上限**：10 个/条 + 无效 ID 静默过滤（推荐）vs 其他上限 / 校验失败报错。
4. **like/collect 的 snippet**：消费端反查 post title（推荐：列表可展示"赞了你的帖子《xxx》"）vs 空串。
5. **未读数**：Redis 计数器 + DB 兜底（推荐）vs 纯 DB COUNT（最简单，每次全表索引 count）。
6. **机器人通知**：bot 作 actor 正常触发（推荐：用户该看到 AI 回复通知）；bot 作 recipient 仍落库（推荐：无害）vs recipient=role2 过滤。
7. **R3 重赞语义**：复用行重置未读、列表位置不变（推荐：upsert 简单幂等）vs 删旧插新行（位置提前，但破坏幂等锚点）。
8. **flush 间隔**：5s（推荐：红点延迟可接受）vs 10s 对齐现有 like/collect 链路。
