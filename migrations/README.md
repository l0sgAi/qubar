# 数据库迁移（golang-migrate）

`domains` schema 的表结构**权威来源**。`docs/pgsql-ddl/` 只做字段语义 / 设计说明，改表必须新增本目录下的迁移文件。

- 工具：[golang-migrate](https://github.com/golang-migrate/migrate)（生产用镜像 `migrate/migrate`，本机 `brew install golang-migrate`）。
- 静态检查：[Squawk](https://squawkhq.com)（配置 `/.squawk.toml`）+ `scripts/check-migrations.sh`。
- 版本表：`public.schema_migrations`（只有当前版本号 + dirty 标记）；生产执行记录另见服务器 `/opt/qubar/migrate/migrations.log`。
- 执行身份：`qubar_owner`。新表的 `qubar_web_app` / `debezium` 权限由 default privileges 自动授予，无需在迁移里 GRANT。

## 流程

```
make migrate-new name=add_post_pin_until   # 生成 <yyyymmddhhmmss>_add_post_pin_until.up.sql
# 编辑 SQL；同步更新 docs/pgsql-ddl/ 对应领域文档的字段说明
make migrate-up                            # 本机执行
make migrate-check                         # 与 CI 相同的检查
```

PR → develop：CI（`ci.yml` 的 `db` job）做规范检查、Squawk、在临时 PG18 上从零回放全部迁移。
develop → main：`release.yml` 在 `deploy` 之前执行 `migrate` job（同步文件 → `migrate up` → 重跑 `grants.sql` 补授权 / 刷新 `dbz_pub`），**迁移失败则不发布应用**。

## 编写规则

1. **forward-only**：只写 `.up.sql`，不维护 `.down.sql`。出错就再写一个新迁移修正。
2. **已合并的文件不许改 / 删 / 改名**（CI 拦截；唯一例外见「失败与 dirty 修复」）。基线 `*_baseline.up.sql` 同样不许改。
3. **版本号必须大于目标分支已有的最大版本号**（CI 拦截）。golang-migrate 只记录"当前版本"，版本更小的文件会被**静默跳过**。分支落后时重新 `make migrate-new` 改文件名。
4. 表名一律写 `domains.<table>`；遵守 pgsql-ddl 规范（UUIDv7、COMMENT ON、无 FK / CHECK）。
5. 文件开头 `SET lock_timeout = '5s';`（模板已带）：拿不到锁就失败，而不是排队把线上读写全部堵住。`qubar_owner` 角色本身也有 `lock_timeout = 5s` 默认值兜底。
6. 一个文件内的语句由 PG 作为**隐式事务**整体执行：失败整体回滚（版本会被标 dirty，见下）。
7. **新建 `domains` 表**：要进 ES 的，登记 `deploy/ansible/group_vars/all.yml` 的 `cdc_tables`（需要 ES sink 的再加 `es_sinks`），同一次发布内会进入 `dbz_pub`；不进 CDC 的在文件里单独写一行 `-- cdc: none`。两者都没有 → CI 失败。

### 兼容性：先迁移、后发版（单副本）

迁移执行时线上跑的还是**旧版本应用**，回滚（`release.yml` rollback）也不会回退 schema。所以每个迁移都必须让旧版本应用照常工作（expand / contract）：

| 变更 | 做法 |
|---|---|
| 加表 / 加可空列 / 加带常量默认值的列 | 直接上 |
| 加 NOT NULL 列 | 带非 volatile `DEFAULT`；否则先加可空列 → 应用写入 → 回填 → 再加 NOT NULL |
| 删列 / 删表 | 第一次发布：应用不再读写；**下一次发布**再写迁移删除 |
| 改列名 / 表名 | 加新列 → 应用双写 → 回填 → 应用切读新列 → 再删旧列（多次发布） |
| 改类型 | 同改名；直接 `ALTER TYPE` 多数会重写全表并持有 ACCESS EXCLUSIVE 锁 |
| 给已有大表建索引 | `CREATE INDEX CONCURRENTLY`，见下 |

### `CREATE INDEX CONCURRENTLY`

不能在事务里执行，而多语句文件会被 PG 包成事务，所以**单独一个文件、只有这一条语句**（连 `SET lock_timeout` 都不能有，靠角色默认值兜底）：

```sql
-- squawk-disable-assume-in-transaction
-- squawk-ignore require-lock-timeout
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_post_title ON domains.post (title);
```

失败时可能留下 INVALID 索引，修复：`DROP INDEX CONCURRENTLY IF EXISTS domains.<idx>;` 后按下面的 dirty 流程重跑。

## 失败与 dirty 修复

迁移失败时 `schema_migrations.dirty = true`，之后的 `up` 会一直拒绝执行，直到人工处理：

1. 看失败原因：GitHub Actions `migrate` job 日志，或服务器 `/opt/qubar/migrate/migrations.log`。
2. 确认失败文件的语句**没有**生效（普通文件整体回滚；CONCURRENTLY 文件检查 INVALID 索引）。
3. 把版本标回**上一个成功版本**：
   ```bash
   ssh -L 15432:127.0.0.1:5432 ssh.qubar.site
   migrate -path migrations -database 'postgres://qubar_owner@127.0.0.1:15432/qubar?sslmode=disable&x-migrations-table="public"."schema_migrations"&x-migrations-table-quoted=1' force <上一个版本>
   ```
4. 修正**失败的那个文件**（它在生产没生效、版本也已标回，下次 `up` 会重跑它）。这是"已合并不许改"的唯一例外：PR 打上 `migration-fix` 标签，CI 才放行修改。
5. 重新走 develop → main；或修完后在 Actions 里 `release` → Run workflow 勾选 `force_migrate` 只跑迁移。

## 基线

`20261015000000_baseline.up.sql` 由 `pg_dump --schema-only -n domains` 经 `deploy/scripts/clean-baseline.sh` 清洗生成，含当时全部 17 张表。
已有数据的环境（生产、本机 dev）**不执行**它，只标记：`migrate ... force 20261015000000`。
`db-migrate.sh` 有护栏：`schema_migrations` 不存在而 `domains` 已有表时拒绝执行，防止对存量库回放基线。
