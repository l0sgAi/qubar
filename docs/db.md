# 数据库表结构定义

> 本文档已按领域拆分至 [docs/pgsql-ddl/](pgsql-ddl/) 目录,本文件仅作入口保留(历史链接兼容)。
>
> **表结构权威来源是 [migrations/](../migrations/README.md)**(golang-migrate 版本化迁移);pgsql-ddl/ 为设计说明 / 字段语义。
>
> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除、jsonb 批量更新附录)见 [pgsql-ddl/README.md](pgsql-ddl/README.md)。
>
> 各领域 DDL:
>
> - [用户域](pgsql-ddl/user.md): `users`
> - [圈子域](pgsql-ddl/circle.md): `category`、`circle`、`circle_member`
> - [帖子域](pgsql-ddl/post.md): `post`
> - [评论域](pgsql-ddl/comment.md): `comment`、`comment_like`
> - [帖子互动域](pgsql-ddl/interaction.md): `post_like`、`post_collect`、`post_view_history`、`post_interaction`
> - [AI 回复机器人域](pgsql-ddl/ai-agent.md): `ai_agent`、`ai_agent_reply_log`
