# 用户关注（follow）设计：关注关系核心、列表与隐私开关

> 目标：新增 `pkg/domains/follow`，落地用户间关注 / 取关、四种关系状态、关注数 / 粉丝数与关注上限（issue #217，总览 #29 的第 1 步）；
> 在其上提供关注 / 粉丝 / 互关列表、批量关系查询与列表隐私开关（issue #218，见 §八）。
> 被关注通知（#219）、首页关注 tab（#220）在后续 issue，都建在本文的表和仓储之上。
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
| 关注时间 | 取关后再关注，以最近一次关注的时间为准（列表按它倒序） |
| 列表可见性 | 关注列表 / 粉丝列表由个人中心**一个开关**同时控制，默认公开（访客可读）；设为私密后只有本人能看名单，关注数 / 粉丝数仍公开 |
| 互关列表 | 只提供「我的互关列表」（仅本人，不受开关影响）；不做互关数 |

「目标用户是否存在」的判定与 `UserService.GetByID` 一致：follow 域声明端口 `application.UserChecker`，
由 `pkg/composition/follow_bridges.go` 的 `followUserChecker` 用 `UserFacade.GetBrief` 桥接，不直接查 `users` 表。

取关不校验目标用户状态：关注之后对方被禁用 / 删除，仍然可以取关。

## 二、分层与文件布局

```
pkg/domains/follow/
  domain/follow.go                  实体 UserFollow / UserFollowStat、Relation 常量与 RelationOf
  domain/repository.go              端口 RelationRepository / StatRepository + 哨兵错误
  domain/list.go                    端口 ListRepository、ListKind、FollowEdge + 列表相关哨兵错误（#218）
  application/service.go            FollowService（由小接口组合）、UserChecker 端口、构造器
  application/relation.go           RelationService：Follow / Unfollow
  application/stats.go              StatsService：GetStats
  application/list.go               ListService：三个列表 + 批量关系；UserBriefReader / ListVisibilityReader 端口（#218）
  application/list_cursor.go        列表游标编解码与作用域校验（#218）
  infrastructure/follow_repo_pg.go            followRepoPG + 两个构造器
  infrastructure/follow_relation_repo_pg.go   Follow / Unfollow / GetRelation（事务、锁顺序）
  infrastructure/follow_stat_repo_pg.go       GetStat / RecountStat
  infrastructure/follow_list_repo_pg.go       ListEdges / GetRelations + NewListRepository（#218）
  interfaces/http/handler.go, list_handler.go, routes.go
pkg/composition/wire_follow.go      newFollowService / registerFollow
pkg/composition/follow_bridges.go   followUserChecker / followUserBriefReader / followListVisibilityReader（user → follow）

pkg/domains/user/                   用户设置（#218；归 user 域，follow 域只经桥接读取）
  domain/setting.go                 实体 UserSetting、可见性常量、端口 SettingRepository
  application/setting.go            SettingService：GetSettings / UpdateSettings
  infrastructure/user_setting_repo_pg.go
  interfaces/http/setting_handler.go   GET / PUT /user/settings + RegisterSettingRoutes
```

`application` 和 `infrastructure` 按职责分文件：`FollowService` 只是把各文件里的小接口（`RelationService`、`StatsService`）组合起来，
仓储端口同样拆成 `RelationRepository`、`StatRepository`。后续 issue 新增能力时以「新增文件 + 在组合接口里加一行」为主，减少并行开发的冲突。

对外的应用服务方法：

| 方法 | 说明 |
|---|---|
| `Follow(ctx, followerID, followeeID) (*RelationVO, error)` | 关注，返回操作后的关系 |
| `Unfollow(ctx, followerID, followeeID) (*RelationVO, error)` | 取关，返回操作后的关系 |
| `GetStats(ctx, viewerID, userID) (*StatsVO, error)` | 关注数 / 粉丝数 / 查看者与该用户的关系 / 列表对查看者是否可见；`viewerID = uuid.Nil` 表示访客 |
| `ListFollowing / ListFollowers(ctx, viewerID, userID, cursor, size) (*FollowListVO, error)` | 关注 / 粉丝列表（§八） |
| `ListMutual(ctx, userID, cursor, size) (*FollowListVO, error)` | 我的互关列表（§八） |
| `GetRelations(ctx, viewerID, targetIDs) (*RelationsVO, error)` | 批量关系查询（§八） |

错误：`domain.ErrCannotFollowSelf`（400）、`domain.ErrUserNotFound`（404）、`domain.ErrFollowLimitReached`（400，应用层包装后 message 带上限值）、
`domain.ErrInvalidCursor`（400）、`domain.ErrFollowListPrivate`（403）、`domain.ErrTooManyTargets`（400）。

## 三、表结构

迁移：`migrations/20261110162100_add_user_follow.up.sql`；字段说明见 [pgsql-ddl/follow.md](pgsql-ddl/follow.md)。

### 3.1 `domains.user_follow`（关系）

- 每对 `(follower_id, followee_id)` 只有一行，唯一索引 `uk_user_follow_pair`（非部分索引，关注走 `INSERT … ON CONFLICT`）。
- 取关置 `deleted=1`，再次关注复用该行：`deleted=0` 并把 `follow_time` 刷成当前时间。行会复用，所以 `id` 不代表关注时间，
  排序键是单独的 `follow_time`。
- 两个方向的列表各有一个部分索引（`WHERE deleted = 0`），键为 `(follower_id | followee_id, follow_time DESC, id DESC)`，
  支持按关注时间倒序的 keyset 翻页（§八）。
- 互关不冗余 `is_mutual`：两个方向各有一行即互关，`GetRelation` 用一条查询按唯一索引取两行。

### 3.2 `domains.user_follow_stat`（计数）

- `user_id` 主键，`following_count` / `follower_count`。用户首次关注 / 被关注时建行，没有行视为 0。
- 不放在 `users` 表：`users` 在 CDC 链路里（`cdc_tables`），往上写计数会让每次关注都触发一次 ES 同步（#130 的问题）。

两张表都不进 CDC（迁移里 `-- cdc: none`），只在 PG 内读写。无 FK / CHECK，引用有效性由应用层保证。
关系表只保留 `create_time` / `update_time` / `deleted`（操作人恒为 `follower_id`，不再设 `create_by` / `update_by`）；
计数表是派生数据，没有软删除列。

### 3.3 `domains.user_setting`（用户设置，user 域）

迁移：`migrations/20261110170808_add_user_setting.up.sql`；字段说明见 [pgsql-ddl/user.md](pgsql-ddl/user.md)。

- `user_id` 主键，`follow_list_visibility`（`0` 公开 / `1` 私密，默认 `0`）。首次修改设置时建行，**没有行按全部默认值处理**。
- 不在 `users` 表加列：`users` 在 CDC / ES 同步链路里，设置项与搜索无关。同样 `-- cdc: none`，无 FK / CHECK（取值由应用层校验）。
- 一列一个设置项，按列单独 upsert（并发修改不同设置项不会互相覆盖）。#30 的「谁可以给我发私信」在这张表加列。

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
| `GET` | `/follow/stats/:user_id` | 访客可读 | `following_count` / `follower_count` / `relation` / `list_visible` |
| `GET` | `/follow/following/:user_id` | 访客可读 | 关注列表；对方设为私密时仅本人，其他人 403 |
| `GET` | `/follow/followers/:user_id` | 访客可读 | 粉丝列表；规则同上 |
| `GET` | `/follow/mutual` | 需登录 | 我的互关列表 |
| `POST` | `/follow/relations` | 需登录 | 批量查询我与一组用户（≤ 100）的关系 |
| `GET` / `PUT` | `/user/settings` | 需登录 | 读 / 改自己的设置（`follow_list_visibility`） |

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
| 被关注者之后被禁用 / 删除 | 关系行和双方计数保留（仍占关注者的名额），关注者可以取关；该用户的 stats 与列表接口返回 404。别人的列表里不再出现该用户（§8.4），计数不调整，所以**列表人数可能略少于计数** |
| 达到上限后的关注尝试 | 每次尝试会写入关系行再回滚，产生少量死元组；关注接口暂无限流（项目没有通用限流中间件，见 #217 范围外） |
| 热点用户 | 关注同一个大 V 的事务在其计数行上串行，单行更新耗时很短；真成为瓶颈时再考虑把粉丝数改成异步聚合 |
| 机器人关注别人 | 机器人账号不登录，不会走到关注接口，未做额外限制 |
| 通知 / 事件 | 本期不发任何事件；`RelationRepository.Follow` 返回的 `changed` 是 #219 判断「是否真的新关注」的依据 |

## 八、列表、批量关系与隐私开关（#218）

### 8.1 三个列表

| 列表 | 内容 | 排序键 | 谁能看 |
|---|---|---|---|
| 关注列表 `following` | `user_id` 关注的人 | `user_id` 关注对方的 `follow_time` | 默认所有人（含访客）；私密后仅本人 |
| 粉丝列表 `followers` | 关注 `user_id` 的人 | 对方关注 `user_id` 的 `follow_time` | 同上 |
| 互关列表 `mutual` | 与当前用户互相关注的人 | 当前用户关注对方的 `follow_time` | 仅本人（接口不带 `user_id`），不受开关影响 |

三者共用一条读取路径（`application/list.go` 的 `listPage`）：

```
listOf(viewer, kind, owner, cursor, size)            ← 关注 / 粉丝列表；互关列表直接进 listPage
  ① requireUser(owner)            不存在 / 已禁用 / 已删除 → ErrUserNotFound（404）
  ② listVisibleTo(viewer, owner)  viewer ≠ owner 且 owner 设为私密 → ErrFollowListPrivate（403）
  ③ listPage
       parseListCursor            非法 / 作用域不符 → ErrInvalidCursor（400）
       ListRepository.ListEdges   keyset 取 need+1 条关系（多取 1 条判断 has_more）
       UserBriefReader.GetBriefs  批量取用户简要信息；取不到的（已禁用 / 已删除）丢弃
       ListRepository.GetRelations  一次查询取 viewer 与本页所有用户的关系（访客跳过，relation 全为 0）
```

- SQL（`follow_list_repo_pg.go`）：`WHERE follower_id|followee_id = owner AND deleted = 0 AND (follow_time, id) < (游标) ORDER BY follow_time DESC, id DESC LIMIT n`，
  走 §3.1 的两个部分索引（`EXPLAIN` 为单个 Index Scan，行比较进 Index Cond）。互关在关注列表的基础上加
  `EXISTS (反向关系行)`，反向行按 `uk_user_follow_pair` 点查，不冗余 `is_mutual`。
- `follow_time` 相同的行按关系行 `id` 倒序定序（`id` 只做 tie-break，不代表关注时间）。
- `size` 不传或 ≤ 0 取 20，超过 50 按 50。
- 列表项的 `relation` 是**查看者**与该用户的关系（不是列表所属用户的），每页一次批量查询，不逐条查库；
  访客恒为 0，列表里出现查看者本人时也是 0。

### 8.2 游标

URL 安全的 base64(JSON)，对前端是不透明字符串（`application/list_cursor.go`），做法同评论游标 v2
（[comment-locate-design.md](comment-locate-design.md) §12）：

```
{"v":1,"list":"following|followers|mutual","scope_id":"<列表所属用户ID>","follow_time":<Unix 微秒>,"id":"<上一页最后一条关系行ID>"}
```

| 游标 | 处理 |
|---|---|
| `list` 与 `scope_id` 都和本次请求一致 | 正常翻页 |
| `list` 不一致（关注列表的游标用在粉丝 / 互关列表） | `ErrInvalidCursor` → 400 |
| `scope_id` 不一致（A 的游标用在 B 的列表） | `ErrInvalidCursor` → 400 |
| 未知版本、缺字段、类型错误、base64 / JSON 损坏 | `ErrInvalidCursor` → 400（不 panic；具体原因不回给客户端） |

`follow_time` 用 Unix 微秒（PG `timestamptz` 的精度），编解码不丢精度。游标只带 keyset 键，不带隐私状态：每次请求都重新做 §8.1 的 ①②。
翻页过程中有人取关 / 再关注：keyset 保证不重复；再关注的人 `follow_time` 变新，会跑到列表最前（已翻过的位置），本轮翻页不会再遇到。

### 8.3 批量关系查询

`GetRelations(viewer, targetIDs)`：一条 SQL 取两个方向的有效关系行（`follower = viewer AND followee IN (…)` 或反向），
在内存里按位合成 relation。返回请求里**每个** ID 的关系：没有关系、目标不存在、或目标就是自己，都是 `0`（不校验目标用户是否存在）。
一次最多 100 个 ID（`MaxRelationTargets`），超过返回 `ErrTooManyTargets`（400）；重复的 ID 合并。

### 8.4 已禁用 / 已删除用户

follow 仓储不读 `users` 表：列表先按关系行取一段，再用 user 域的 `GetBriefs` 取简要信息，取不到的就是已禁用 / 已删除的用户，直接丢弃。

- 计数表不因此调整，所以**列表里的人数可能略少于关注数 / 粉丝数**。
- 过滤后不足一页时接着往后取，一页最多取 `maxListRounds = 3` 轮；连续遇到大量失效用户时一页可以少于 `size` 条（甚至为空），
  但 `has_more` / `next_cursor` 始终准确，前端以 `has_more` 判断是否结束。
- `GetBriefs` 目前是逐个走 `UserService.GetByID`（缓存优先），一页最多 50 人；成为瓶颈时在 user 域加真正的批量读取。

### 8.5 隐私开关

- 存储与读写归 user 域：`domains.user_setting.follow_list_visibility`，接口 `GET` / `PUT /user/settings`（只读写当前登录用户自己的设置）。
  取值非法（不是 0 / 1）或请求体里没有任何设置项返回 400。
- follow 域声明端口 `application.ListVisibilityReader`，由 `followListVisibilityReader` 桥接到 `userapp.SettingService`，不直接查表。
- 判定在 `listVisibleTo(viewer, owner)` 一处：本人恒可见（不读设置）；其他人（含访客）= 开关不是私密。
  关注 / 粉丝列表和 `GetStats` 的 `list_visible` 都用它，两处结论一致。
- 私密**只**挡两个列表的名单：两个计数、`relation`、批量关系查询、关注 / 取关都不受影响。
  （批量关系只回答「我和 TA」的关系，本来就是当事人自己的信息。）
- 读设置是按主键读一行，没有加缓存；开关修改后立即生效。

| 请求 | 目标用户公开 | 目标用户私密 |
|---|---|---|
| 本人看自己的关注 / 粉丝列表 | 200 | 200 |
| 其他登录用户 / 访客看 | 200 | 403 |
| `/follow/stats` 的计数与 `relation` | 正常 | 正常（`list_visible=false`，本人为 `true`） |
| 本人的互关列表 | 200 | 200 |

## 九、分阶段交付

| 阶段 | issue | 内容 |
|---|---|---|
| P0 | #217（本文 §一 ~ §七） | 关注 / 取关、关系状态、计数、上限、重算方法 |
| P1 | #218（本文 §八） | 关注 / 粉丝 / 互关列表、批量关系查询、列表隐私开关 |
| P1 | #219 | 被关注通知 |
| P1 | #220 | 首页关注 tab 合并关注用户的帖子 |
