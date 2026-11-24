# 用户关注：`user_follow` / `user_follow_stat`

> [返回目录](README.md)。权威 DDL 见 `migrations/20261110162100_add_user_follow.up.sql`；设计见 [follow-design.md](../follow-design.md)（#217）。
> 两张表都不进 CDC（`-- cdc: none`），只在 PG 内读写。

## 用户关注关系表

```sql
CREATE TABLE domains.user_follow (
    -- ID主键 (UUIDv7,应用层生成;行会被复用,不代表关注时间)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    follower_id UUID NOT NULL,       -- 关注者 (users.id)
    followee_id UUID NOT NULL,       -- 被关注者 (users.id,可以是 AI 机器人账号)

    -- 最近一次关注的时间 (取关后再关注时更新;列表排序键)
    follow_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- 关注状态 (0=关注中, 1=已取关)
    deleted SMALLINT NOT NULL DEFAULT 0
);

-- 1. 【核心】每对用户只有一行
-- ⚠️ 应用依赖：关注走 INSERT … ON CONFLICT (follower_id, followee_id)，勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_user_follow_pair ON domains.user_follow (follower_id, followee_id);

-- 2. 「我关注的」：按关注时间倒序 keyset 翻页
CREATE INDEX idx_user_follow_following ON domains.user_follow (follower_id, follow_time DESC, id DESC) WHERE deleted = 0;

-- 3. 「关注我的」：按关注时间倒序 keyset 翻页
CREATE INDEX idx_user_follow_follower ON domains.user_follow (followee_id, follow_time DESC, id DESC) WHERE deleted = 0;
```

| 字段 | 说明 |
|---|---|
| `id` | 主键。取关后再关注复用同一行，所以它**不代表关注时间**，排序不要用它当主键 |
| `follower_id` / `followee_id` | 关注者 / 被关注者，无 FK。关注时应用层校验被关注者是正常状态用户；之后对方被禁用 / 删除，行保留 |
| `follow_time` | 最近一次关注的时间。首次关注 = `create_time`；取关后再关注时刷新。关注 / 粉丝列表按 `(follow_time DESC, id DESC)` 排序 |
| `create_time` | 首次关注时间（行创建时间），之后不变 |
| `update_time` | 关注 / 取关切换时由应用层更新 |
| `deleted` | `0` = 关注中，`1` = 已取关 |

- 互关不冗余字段：`(A,B)` 与 `(B,A)` 两行都 `deleted=0` 即互关。
- 审计列只保留 `create_time` / `update_time` / `deleted`：纯关系表，操作人恒为 `follower_id`，不设 `create_by` / `update_by`。

## 用户关注计数表

```sql
CREATE TABLE domains.user_follow_stat (
    user_id UUID PRIMARY KEY,                     -- 用户ID (users.id),每个用户至多一行
    following_count INTEGER NOT NULL DEFAULT 0,   -- 关注数
    follower_count  INTEGER NOT NULL DEFAULT 0,   -- 粉丝数
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

| 字段 | 说明 |
|---|---|
| `user_id` | 主键，无 FK。用户首次关注 / 被关注时建行；**没有行视为两个计数都是 0** |
| `following_count` | 该用户关注了多少人。上限 `follow.max_following`（默认 2000）由关注事务里的条件自增保证，库里没有约束 |
| `follower_count` | 多少人关注了该用户，不设上限 |
| `update_time` | 计数变化时由应用层更新 |

- 与 `user_follow` 在**同一个事务**里更新，只在关系实际变化时 ±1；不放在 `users` 表是因为 `users` 在 CDC 链路里。
- 两个用户互相并发关注时，事务按 `user_id` 升序更新两行计数，避免死锁。
- 派生数据，没有软删除列；出现不一致时用仓储的 `RecountStat` 按明细重算。

核对计数与明细是否一致（应为 0 行）：

```sql
SELECT s.user_id, s.following_count, s.follower_count
FROM domains.user_follow_stat s
WHERE s.following_count <> (SELECT count(*) FROM domains.user_follow f WHERE f.follower_id = s.user_id AND f.deleted = 0)
   OR s.follower_count  <> (SELECT count(*) FROM domains.user_follow f WHERE f.followee_id = s.user_id AND f.deleted = 0);
```
