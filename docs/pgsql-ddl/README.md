# 数据库表结构定义(UUIDv7 主键版)

> **表结构权威来源已迁到 [`migrations/`](../../migrations/README.md)(golang-migrate,随发布自动执行)。**
> 本目录降为**设计说明 / 字段语义文档**:改表 = 新增迁移文件 + 同步更新这里对应领域的说明。
> 这里的 `CREATE TABLE` 片段用于阅读,不再手工到生产执行;与实际 schema 不一致时以 `migrations/` 为准。

> 中间件:PostgreSQL 18,内置 `uuidv7()` 函数(无需 CREATE EXTENSION,`uuid` 类型为 PG 内置)。
> 全表主键与外键统一使用 **UUIDv7**(`UUID` 类型)。UUIDv7 前 48 位为毫秒级时间戳,**字典序 == 时间序**,可直接用于「最新优先」排序与 keyset 游标分页。
>
> **主键生成策略**:
>
> - DDL 中 `DEFAULT uuidv7()` 仅为**手工 SQL 插入**时的兜底;
> - 应用层(GORM)统一在 `BeforeCreate` 钩子中生成 UUIDv7,避免 GORM 将零值(nil UUID)当作有效值覆盖默认值,并保证 `Create` 后 ID 立即可用。
>
> **可空外键(原 int64 用 `0` 作哨兵的字段)**改为 NULLABLE,语义为 `NULL = 无父级/无值`:
> `comment.root_id`、`comment.reply_to_id`、`comment.reply_to_user_id`、`category.parent_id`、`circle.category_id`、`comment_like.post_id`。

## 目录(按领域拆分)

| 文档 | 领域 | 表 |
|---|---|---|
| [user.md](user.md) | 用户 | `users` |
| [circle.md](circle.md) | 圈子 | `category`、`circle`、`circle_member` |
| [post.md](post.md) | 帖子 | `post` |
| [comment.md](comment.md) | 评论 | `comment`、`comment_like` |
| [interaction.md](interaction.md) | 帖子互动/流水 | `post_like`、`post_collect`、`post_view_history`、`post_interaction` |
| [ai-agent.md](ai-agent.md) | AI 回复机器人 | `ai_agent`、`ai_agent_reply_log` |
| [notice.md](notice.md) | 消息中心 | `notification` |

> 约定:无 FK/CHECK 约束(引用有效性、状态枚举合法性由应用层保证);
> 逻辑删除 `deleted SMALLINT`(0=正常,1=已删除);状态列 `status SMALLINT`,枚举含义见各表注释。

---

## 附录:应用角色权限

迁移以 `qubar_owner` 执行,生产 `grants.sql`(Ansible 渲染,见 `deploy/ansible/roles/stack/templates/grants.sql.j2`)已配置
`ALTER DEFAULT PRIVILEGES FOR ROLE qubar_owner`,新表自动授予 `qubar_web_app` 行级 DML(`SELECT/INSERT/UPDATE/DELETE`)与 `debezium` 只读;
每次迁移后还会重跑 `grants.sql` 兜底。**迁移文件里不需要写 GRANT。**

约定:

- 应用角色只给行级 DML,**不给** `TRUNCATE/ALTER`——schema 变更一律走迁移(与"运行时角色无 ALTER 权限、禁止 AutoMigrate"一致);
- append-only 表(如 `ai_agent_reply_log`)如需收紧,在迁移里显式 `REVOKE UPDATE, DELETE ON domains.<table> FROM qubar_web_app;`;
- 本机 dev 库若不是以 `qubar_owner` 建表,default privileges 不生效,需自行 `GRANT`;
- 验证:`\dp domains.<table>` 或应用重试对应接口。

---

## 附录:批量更新统计的 jsonb 类型对照

异步消费者(MQ 聚合后批量写库)使用 `jsonb_to_recordset`,ID 列的类型必须从 `BIGINT` 改为 `uuid`:

```sql
-- 圈子成员/帖子计数 (circle_id)
UPDATE circle c
SET member_count = GREATEST(c.member_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(circle_id uuid, delta BIGINT)) v
WHERE c.id = v.circle_id AND c.deleted = 0;

-- 评论点赞数 (comment_id)
UPDATE comment c
SET like_count = GREATEST(c.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(comment_id uuid, delta BIGINT)) v
WHERE c.id = v.comment_id AND c.deleted = 0;

-- 帖子点赞数 (post_id)
UPDATE post p
SET like_count = GREATEST(p.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(post_id uuid, delta BIGINT)) v
WHERE p.id = v.post_id AND p.deleted = 0;

-- 帖子全量统计 (post_id + 多 delta)
UPDATE post p
SET view_count    = LEAST(GREATEST(p.view_count    + v.view_delta, 0), 1000000000),
    comment_count = GREATEST(p.comment_count + v.comment_delta, 0),
    like_count    = GREATEST(p.like_count    + v.like_delta, 0),
    collect_count = GREATEST(p.collect_count + v.collect_delta, 0),
    update_time   = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb)
      AS v(post_id uuid, view_delta BIGINT, comment_delta BIGINT, like_delta BIGINT, collect_delta BIGINT)) v
WHERE p.id = v.post_id AND p.deleted = 0;
```
