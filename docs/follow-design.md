# 用户关注（follow）设计：关注关系核心

> 目标：新增 `pkg/domains/follow`，落地用户间关注 / 取关、四种关系状态、关注数 / 粉丝数与关注上限（issue #217，总览 #29 的第 1 步）。
> 列表与隐私开关（#218）、被关注通知（#219）、首页关注 tab（#220）在后续 issue，都建在本文的表和仓储之上。
>
> 基线：此前没有用户间关注关系；首页 `following` tab 把「关注」映射成「已加入的圈子」（`recommend` 域的 `getFollowing`）。

---

## 一、业务规则（2026-11-10 维护者确认）

| 项 | 规则 |
|---|---|
| 可关注对象 | 状态正常的用户，含 AI 机器人账号（`role=2`，全局 / 圈内）。不能关注自己、不存在 / 已禁用 / 已删除的用户 |
| 关注上限 | 每人最多关注 `follow.max_following` 人（默认 2000，可配置）；粉丝数不设上限 |
| 计数 | 关注数、粉丝数对所有人公开（含访客）；不做互关数 |
| 关系状态 | `0` 未关注 / `1` 我关注了对方 / `2` 对方关注了我 / `3` 互相关注；访客或查看自己时为 `0` |
| 幂等 | 重复关注 / 重复取关都返回成功，不产生重复行，计数不变 |
| 关注时间 | 取关后再关注，以最近一次关注的时间为准（后续列表按它倒序） |

「目标用户是否存在」的判定与 `UserService.GetByID` 一致：follow 域声明端口 `application.UserChecker`，
由 `pkg/composition/follow_bridges.go` 的 `followUserChecker` 用 `UserFacade.GetBrief` 桥接，不直接查 `users` 表。

取关不校验目标用户状态：关注之后对方被禁用 / 删除，仍然可以取关。

## 二、分层与文件布局

```
pkg/domains/follow/
  domain/follow.go                  实体 UserFollow / UserFollowStat、Relation 常量与 RelationOf
  domain/repository.go              端口 RelationRepository / StatRepository + 哨兵错误
  application/service.go            FollowService（由小接口组合）、UserChecker 端口、构造器
  application/relation.go           RelationService：Follow / Unfollow
  application/stats.go              StatsService：GetStats
  infrastructure/follow_repo_pg.go            followRepoPG + 两个构造器
  infrastructure/follow_relation_repo_pg.go   Follow / Unfollow / GetRelation（事务、锁顺序）
  infrastructure/follow_stat_repo_pg.go       GetStat / RecountStat
  interfaces/http/handler.go, routes.go
pkg/composition/wire_follow.go      newFollowService / registerFollow
pkg/composition/follow_bridges.go   followUserChecker（user → follow）
```

`application` 和 `infrastructure` 按职责分文件：`FollowService` 只是把各文件里的小接口（`RelationService`、`StatsService`）组合起来，
仓储端口同样拆成 `RelationRepository`、`StatRepository`。后续 issue 新增能力时以「新增文件 + 在组合接口里加一行」为主，减少并行开发的冲突。

对外的应用服务方法：

| 方法 | 说明 |
|---|---|
| `Follow(ctx, followerID, followeeID) (*RelationVO, error)` | 关注，返回操作后的关系 |
| `Unfollow(ctx, followerID, followeeID) (*RelationVO, error)` | 取关，返回操作后的关系 |
| `GetStats(ctx, viewerID, userID) (*StatsVO, error)` | 关注数 / 粉丝数 / 查看者与该用户的关系；`viewerID = uuid.Nil` 表示访客 |

错误：`domain.ErrCannotFollowSelf`（400）、`domain.ErrUserNotFound`（404）、`domain.ErrFollowLimitReached`（400，应用层包装后 message 带上限值）。

## 三、表结构

迁移：`migrations/20261110162100_add_user_follow.up.sql`；字段说明见 [pgsql-ddl/follow.md](pgsql-ddl/follow.md)。

### 3.1 `domains.user_follow`（关系）

- 每对 `(follower_id, followee_id)` 只有一行，唯一索引 `uk_user_follow_pair`（非部分索引，关注走 `INSERT … ON CONFLICT`）。
- 取关置 `deleted=1`，再次关注复用该行：`deleted=0` 并把 `follow_time` 刷成当前时间。行会复用，所以 `id` 不代表关注时间，
  排序键是单独的 `follow_time`。
- 两个方向的列表各有一个部分索引（`WHERE deleted = 0`），键为 `(follower_id | followee_id, follow_time DESC, id DESC)`，
  支持按关注时间倒序的 keyset 翻页（#218 使用）。
- 互关不冗余 `is_mutual`：两个方向各有一行即互关，`GetRelation` 用一条查询按唯一索引取两行。

### 3.2 `domains.user_follow_stat`（计数）

- `user_id` 主键，`following_count` / `follower_count`。用户首次关注 / 被关注时建行，没有行视为 0。
- 不放在 `users` 表：`users` 在 CDC 链路里（`cdc_tables`），往上写计数会让每次关注都触发一次 ES 同步（#130 的问题）。

两张表都不进 CDC（迁移里 `-- cdc: none`），只在 PG 内读写。无 FK / CHECK，引用有效性由应用层保证。
关系表只保留 `create_time` / `update_time` / `deleted`（操作人恒为 `follower_id`，不再设 `create_by` / `update_by`）；
计数表是派生数据，没有软删除列。

## 四、计数方案：同事务更新

关注是低频操作，计数直接和关系行在**同一个 PG 事务**里更新，不走项目里点赞 / 收藏那套「Redis 增量 + Redpanda 异步落库」：

- 没有缓存与库不一致、重启丢增量（#50）这类问题，计数不会漂移；
- 读路径就是按主键读一行，不需要缓存。

只在关系**实际变化**时改计数，判断依据是关系行语句影响的行数：

```
Follow(follower, followee, max)                      Unfollow(follower, followee)
  BEGIN                                                BEGIN
  ① INSERT user_follow … ON CONFLICT DO UPDATE         ① UPDATE user_follow SET deleted=1
       SET deleted=0, follow_time=now                       WHERE … AND deleted=0
       WHERE deleted<>0                                     影响 0 行 → 本来没关注，提交，结束
     影响 0 行 → 本来就在关注，提交，结束               ② 按 user_id 升序更新两侧计数行（减到 0 为止）：
  ② 按 user_id 升序更新两侧计数行：                         follower:  following_count-1
       follower:  following_count+1  WHERE < max            followee:  follower_count-1
                  影响 0 行 → ErrFollowLimitReached，回滚   COMMIT
       followee:  follower_count+1
  COMMIT
```

计数行的自增是 `INSERT … ON CONFLICT (user_id) DO UPDATE`，首次出现时顺带建行。

### 重算

`StatRepository.RecountStat(ctx, userID)` 按关系明细重算并覆盖一个用户的两个计数：先 `SELECT … FOR UPDATE` 锁住该用户的计数行，
再用两个 `count(*)` 子查询更新。任何改这个用户计数的关注 / 取关事务都要拿同一把行锁，所以重算与并发写互斥，不会丢更新。
本期只提供方法并用集成测试保证「重算结果 = 明细」，不加定时任务和触发接口（同事务计数不会漂移，出现问题再加）。

## 五、并发处理

| 场景 | 处理 | 集成测试 |
|---|---|---|
| 同一对用户并发重复关注 | 都落在同一关系行上（唯一索引）：先到者插入 / 复活，后到者等它提交后走 `ON CONFLICT`，`WHERE deleted<>0` 不成立，影响 0 行，不动计数 | `TestFollowRepo_ConcurrentDuplicateFollowCountsOnce` |
| 并发关注不同目标冲击上限 | 关注者计数行的条件自增（`following_count < max`）由行锁串行化，`READ COMMITTED` 下等锁后按最新值重新判断；影响 0 行即超限，整个事务回滚（关系行一并撤销） | `TestFollowRepo_ConcurrentFollowNeverExceedsLimit` |
| A 关注 B 与 B 关注 A 并发 | 两个事务的关系行不同；各自都要改 A、B 两行计数，统一按 `user_id` 升序更新（`inStatOrder`），锁顺序全局一致，不会成环 | `TestFollowRepo_MutualConcurrentFollowNoDeadlock` |
| 重算与关注 / 取关并发 | 重算先锁计数行再统计，见上节 | `TestFollowRepo_RecountStatConcurrentWithWrites` |

死锁分析：一个事务最多持有 1 行关系行 + 2 行计数行，且关系行总是第一个拿。等待关系行的事务此时不持有任何锁，不可能在环里；
计数行之间按固定顺序获取，也不会成环。去掉 `inStatOrder` 的排序后，互相并发关注的测试会稳定复现 `deadlock detected (40P01)`。

## 六、接口与配置

接口（详见 [api/follow-api.md](api/follow-api.md)）：

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| `POST` | `/follow/:user_id` | 需登录 | 关注，幂等 |
| `DELETE` | `/follow/:user_id` | 需登录 | 取关，幂等 |
| `GET` | `/follow/stats/:user_id` | 访客可读 | `following_count` / `follower_count` / `relation` |

配置：

```yaml
follow:
  max_following: 2000 # 每个用户最多关注人数(<=0 兜底 2000)；粉丝数不设上限
```

`pkg/conf/conf_follow.go` 的 `Follow.MaxFollowingOrDefault()` 负责兜底；service 每次关注时读取，配置热更新后立即生效。
调低上限不会清理已超出的关注，只是在回落到上限以下之前不能再关注新的人。

## 七、边界与已知限制

| 项 | 现状 |
|---|---|
| 被关注者之后被禁用 / 删除 | 关系行和双方计数保留（仍占关注者的名额），关注者可以取关；该用户的 stats 接口返回 404。列表里是否过滤由 #218 决定 |
| 达到上限后的关注尝试 | 每次尝试会写入关系行再回滚，产生少量死元组；关注接口暂无限流（项目没有通用限流中间件，见 #217 范围外） |
| 热点用户 | 关注同一个大 V 的事务在其计数行上串行，单行更新耗时很短；真成为瓶颈时再考虑把粉丝数改成异步聚合 |
| 机器人关注别人 | 机器人账号不登录，不会走到关注接口，未做额外限制 |
| 通知 / 事件 | 本期不发任何事件；`RelationRepository.Follow` 返回的 `changed` 是 #219 判断「是否真的新关注」的依据 |

## 八、分阶段交付

| 阶段 | issue | 内容 |
|---|---|---|
| P0 | #217（本文） | 关注 / 取关、关系状态、计数、上限、重算方法 |
| P1 | #218 | 关注 / 粉丝 / 互关列表、批量关系查询、列表隐私开关 |
| P1 | #219 | 被关注通知 |
| P1 | #220 | 首页关注 tab 合并关注用户的帖子 |
