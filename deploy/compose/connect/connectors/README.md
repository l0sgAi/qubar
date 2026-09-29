# Kafka Connect 连接器定义（⚠️ 模板，需与本地已跑通的配置对齐）

仓库里之前没有 CDC 配置（本地是手工搭的）。这两份是按代码约定推出来的**起点**，上线前请用本地环境校对：

```bash
# 在 Mac 上导出现有连接器配置，逐项对比
curl -s localhost:8083/connectors | jq
curl -s localhost:8083/connectors/<name>/config | jq
```

代码侧的硬约定（`pkg/server/storage/elasticsearch`）：

- ES 索引名 = `pg.domains.<table>`（`elasticsearch.index_prefix: pg.domains`）→ Debezium `topic.prefix=pg`、`schema.include.list=domains`，sink 直接用 topic 名当索引名。
- 文档 `_id` = 主键 `id`（UUID）。
- 删除事件要能删掉 ES 文档（tombstone → `behavior.on.null.values=delete`）；逻辑删除字段 `deleted` 由查询侧过滤。
- 索引模板 `pg_domains_template` 与 `circle` 索引由应用启动时创建（需 IK 插件），**先启动应用、再注册 sink**，否则动态映射会抢先建出错误的 text 字段。

`${PG_DEBEZIUM_PASSWORD}` 由 `scripts/register-connectors.sh` 用 .env 替换；文件内容就是 connector 的 config 对象，连接器名取文件名。
