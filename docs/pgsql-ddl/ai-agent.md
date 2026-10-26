# AI 回复机器人域(ai-agent)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## AI 回复机器人配置表

> AI 回复机器人（agent）配置，分两种作用域：
> - **平台全局机器人**（`circle_id IS NULL`）：仅管理员（`users.role = 1`）可增删改（应用层校验），
>   参与全站回复触发链路；
> - **圈子级机器人**（`circle_id` 指向 `domains.circle.id`）：由该圈圈主/管理员管理（每圈上限 5），
>   创建后 `circle_id` 不可变，本期不参与任何回复触发（见 docs/circle-agent-manage-design.md）。
> 机器人以 `linked_user_id` 关联的系统用户身份发表评论--创建 agent 时应用层同步创建一个
> `users` 行（如 status=1, role=2 保留段），评论外键即可复用现有 `comment.user_id` 链路。
>
> - API 协议白名单（openai/anthropic/gemini/ollama 等）由应用层维护，`api_protocol` 仅存值。
> - `api_key` 应用层加密后存密文（列长 512 预留加密膨胀）。
> - `llm_params` 存各协议通用参数，未配置项走应用层默认值；协议特有参数不入库。
> - 频率限制为「每小时上限 + 最小间隔」双阈值，执行时统计 `ai_agent_reply_log`（见下表）。

```sql
DROP TABLE IF EXISTS domains.ai_agent;

CREATE TABLE domains.ai_agent (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 机器人身份与归属
    name VARCHAR(50) NOT NULL,               -- 机器人名称(展示名,作用域内唯一:全局桶或单圈桶)
    avatar_url VARCHAR(500),                 -- 机器人头像
    linked_user_id UUID NOT NULL,            -- 关联系统用户ID(机器人以该身份发评论)
    circle_id UUID,                          -- 绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=平台全局机器人(超管维护);创建后不可变
    creator_id UUID,                         -- 创建者用户ID(users.id,审计用;存量行无法追溯为NULL)

    -- 2. LLM 接入配置
    api_protocol VARCHAR(20) NOT NULL,       -- API协议: openai/anthropic/gemini/ollama(白名单应用层校验)
    base_url VARCHAR(500) NOT NULL DEFAULT '', -- API基础地址(如 https://api.openai.com/v1)
    api_key VARCHAR(512),                    -- API密钥(应用层加密存密文)
    model VARCHAR(100) NOT NULL,             -- 模型名,如 gpt-4o / claude-sonnet-5

    -- 3. LLM 通用参数
    -- 结构示例: {"temperature":0.7,"top_p":1,"max_tokens":1024,"presence_penalty":0,"frequency_penalty":0}
    llm_params JSONB NOT NULL DEFAULT '{}'::JSONB,
    system_prompt TEXT NOT NULL DEFAULT '',  -- 系统提示词(机器人人设/回复风格)
    filter_prompt TEXT NOT NULL DEFAULT '',  -- 回复判定条件(自然语言,空=不判定直接回复,仅关键词触发生效)

    -- 4. 触发方式
    trigger_mode SMALLINT NOT NULL DEFAULT 1, -- 触发模式: 1=全部新帖, 2=关键词触发, 3=手动
    -- trigger_mode=2 时的关键词列表,结构示例: ["AI助手","帮我看看"]
    trigger_keywords JSONB NOT NULL DEFAULT '[]'::JSONB,

    -- 5. 回复频率限制
    max_replies_per_hour INT NOT NULL DEFAULT 30, -- 每小时最大回复数(0=不限)
    min_interval_sec INT NOT NULL DEFAULT 60,     -- 两次回复最小间隔秒数(0=不限)

    -- 5.1 自主行为配置(与 trigger_mode 互不影响;按顶层 key 独立写入)
    -- 结构示例: {"auto_reply":{"enabled":true,"targets":["post","comment"],"post_hot_min":200,
    --            "comment_like_min":20,"comment_reply_min":10,"window_hours":24,"max_per_hour":5}}
    auto_config JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- 6. 状态与生命周期
    status SMALLINT NOT NULL DEFAULT 1,      -- 状态: 0=停用, 1=启用
    deleted SMALLINT DEFAULT 0,              -- 逻辑删除: 0=正常, 1=已删除

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.ai_agent IS 'AI回复机器人配置表(全局机器人管理员维护/圈内机器人圈主管理员维护)';
COMMENT ON COLUMN domains.ai_agent.name IS '机器人名称(展示名,未删除行内作用域唯一:全局机器人共享全零UUID桶,圈内按circle_id各自唯一)';
COMMENT ON COLUMN domains.ai_agent.linked_user_id IS '关联系统用户ID(机器人发评论的身份,创建agent时同步创建)';
COMMENT ON COLUMN domains.ai_agent.circle_id IS '绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=平台全局机器人(超管维护);创建后不可变';
COMMENT ON COLUMN domains.ai_agent.creator_id IS '创建者用户ID(users.id,审计用)';
COMMENT ON COLUMN domains.ai_agent.api_protocol IS 'API协议: openai/anthropic/gemini/ollama(白名单应用层校验)';
COMMENT ON COLUMN domains.ai_agent.base_url IS 'API基础地址(兼容OpenAI协议的中转/自托管端点)';
COMMENT ON COLUMN domains.ai_agent.api_key IS 'API密钥(应用层加密存密文,任何接口不得回显明文)';
COMMENT ON COLUMN domains.ai_agent.model IS '模型名';
COMMENT ON COLUMN domains.ai_agent.llm_params IS 'LLM通用参数(temperature/top_p/max_tokens/presence_penalty/frequency_penalty,未配置走应用层默认)';
COMMENT ON COLUMN domains.ai_agent.system_prompt IS '系统提示词(机器人人设/回复风格)';
COMMENT ON COLUMN domains.ai_agent.filter_prompt IS '回复判定条件(自然语言,如"只回复编程相关问题";空=不判定直接回复;仅trigger_mode=2关键词触发生效,见docs/agent-reply-filter-design.md)';
COMMENT ON COLUMN domains.ai_agent.trigger_mode IS '触发模式: 1=全部新帖, 2=关键词触发, 3=手动';
COMMENT ON COLUMN domains.ai_agent.trigger_keywords IS 'trigger_mode=2时的关键词列表(JSON数组,应用层匹配)';
COMMENT ON COLUMN domains.ai_agent.max_replies_per_hour IS '每小时最大回复数(0=不限,按ai_agent_reply_log统计)';
COMMENT ON COLUMN domains.ai_agent.min_interval_sec IS '两次回复最小间隔秒数(0=不限)';
COMMENT ON COLUMN domains.ai_agent.auto_config IS '自主行为配置(JSON对象,与trigger_mode互不影响): auto_reply={enabled,targets,post_hot_min,comment_like_min,comment_reply_min,window_hours,max_per_hour}; 各子对象按顶层key独立写入';
COMMENT ON COLUMN domains.ai_agent.status IS '状态: 0=停用, 1=启用';

-- --- 索引优化 ---

-- 1. 名称唯一(排除逻辑删除行;作用域分桶:全局机器人共享全零UUID桶保持全局唯一,
--    圈内按各自circle_id桶唯一——两个圈可以都有叫"小助手"的机器人)
CREATE UNIQUE INDEX idx_ai_agent_name ON domains.ai_agent
    (COALESCE(circle_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
    WHERE deleted = 0;

-- 2. 调度器扫描启用中的机器人(表小,部分索引足够)
CREATE INDEX idx_ai_agent_active ON domains.ai_agent(id) WHERE deleted = 0 AND status = 1;

-- 3. 反查机器人关联的系统用户(登录/鉴权链路排除机器人账号)
CREATE INDEX idx_ai_agent_linked_user ON domains.ai_agent(linked_user_id) WHERE deleted = 0;

-- 4. 圈内机器人列表/计数(部分索引,全局机器人circle_id为NULL不占索引行)
CREATE INDEX idx_ai_agent_circle ON domains.ai_agent (circle_id) WHERE deleted = 0;
```

## AI 回复日志表

> agent 每次回复的审计与频率限制统计事实表。无 `deleted`/`update_time`：被动触发（关键词 / @提及 / 手动）
> **append-only**（终态写入后不变，纠错靠新行）；自动回复（`trigger_source=4`）先插 `status=4` 占位行再调 LLM，
> 终态时**原地更新这一行**（部分唯一索引保证每机器人每目标只有一行，见 docs/agent-auto-reply-design.md）。
>
> - 频率限制执行：按 `(agent_id, create_time)` 统计最近 1 小时行数 + 取最新一行 `create_time` 算间隔；
>   统计口径**排除 status=2（分类器跳过）**——skipped 未产出回复，不占配额（应用层查询条件保证）。
> - 同一帖子可被同一机器人多次回复（关键词触发不设防重），用量仅靠频率限制约束。

```sql
DROP TABLE IF EXISTS domains.ai_agent_reply_log;

CREATE TABLE domains.ai_agent_reply_log (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 关联关系
    agent_id UUID NOT NULL,                  -- 机器人ID(ai_agent.id)
    post_id UUID NOT NULL,                   -- 被回复帖子ID(post.id)
    comment_id UUID,                       -- 生成的评论ID(comment.id,调用失败时为NULL)
    user_id UUID NOT NULL,                   -- 帖子作者ID(冗余,便于风控分析)
    trigger_source SMALLINT NOT NULL DEFAULT 0, -- 触发来源: 1=评论关键词, 2=@提及, 3=手动, 4=自动回复; 0=未知(迁移窗口期旧版本写入)
    target_comment_id UUID,                  -- 被回复的目标评论ID(回复挂在该评论楼层内时非NULL,回复帖子本身为NULL)

    -- 2. 调用结果
    status SMALLINT NOT NULL DEFAULT 1,      -- 结果: 0=失败, 1=成功, 2=分类器跳过, 3=分类器超时降级, 4=自动回复占位(执行中)
    error_msg VARCHAR(2048),                 -- 失败原因(截断后的错误信息;status=2时为分类器跳过原因)
    latency_ms INT NOT NULL DEFAULT 0,       -- LLM调用耗时(毫秒,两阶段总耗时)
    prompt_tokens INT NOT NULL DEFAULT 0,    -- 输入token数(供应商返回,两阶段求和,失败为0)
    completion_tokens INT NOT NULL DEFAULT 0, -- 输出token数(供应商返回,两阶段求和,失败为0)

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.ai_agent_reply_log IS 'AI回复日志表(append-only,频率限制统计+审计)';
COMMENT ON COLUMN domains.ai_agent_reply_log.agent_id IS '机器人ID(ai_agent.id,无FK,应用层保证)';
COMMENT ON COLUMN domains.ai_agent_reply_log.post_id IS '被回复帖子ID(post.id,无FK,应用层保证)';
COMMENT ON COLUMN domains.ai_agent_reply_log.comment_id IS '生成的评论ID(comment.id,调用失败时为NULL)';
COMMENT ON COLUMN domains.ai_agent_reply_log.user_id IS '帖子作者ID(冗余,风控分析用)';
COMMENT ON COLUMN domains.ai_agent_reply_log.trigger_source IS '触发来源: 1=评论关键词, 2=@提及, 3=手动, 4=自动回复(定时扫描); 0=未知(迁移窗口期旧版本写入)';
COMMENT ON COLUMN domains.ai_agent_reply_log.target_comment_id IS '被回复的目标评论ID(comment.id;回复挂在该评论楼层内时非NULL,回复帖子本身为NULL)';
COMMENT ON COLUMN domains.ai_agent_reply_log.status IS '结果: 0=失败, 1=成功, 2=分类器跳过, 3=分类器超时降级直回(运维事件,不参与限频统计), 4=自动回复占位(执行中;进程中断则保持4,视为已触发不重试)';
COMMENT ON COLUMN domains.ai_agent_reply_log.latency_ms IS 'LLM调用耗时(毫秒,分类器+生成两阶段总耗时)';
COMMENT ON COLUMN domains.ai_agent_reply_log.prompt_tokens IS '输入token数(供应商返回,失败为0)';
COMMENT ON COLUMN domains.ai_agent_reply_log.completion_tokens IS '输出token数(供应商返回,失败为0)';

-- --- 索引优化 ---

-- 1. 频率限制统计: 最近1小时计数 + 最新回复时间
CREATE INDEX idx_ai_reply_agent_time ON domains.ai_agent_reply_log(agent_id, create_time DESC);

-- 2. 自动回复每机器人每目标最多一行(trigger_source=4;回复帖子本身时 target_comment_id 为 NULL,全零UUID归桶)。
--    post_id 在前:同一索引服务"每目标已有自动回复机器人数"统计(max_agents_per_target)
CREATE UNIQUE INDEX idx_ai_reply_auto_target ON domains.ai_agent_reply_log
    (post_id, (COALESCE(target_comment_id, '00000000-0000-0000-0000-000000000000'::uuid)), agent_id)
    WHERE trigger_source = 4;
```

## 存量表迁移（comment_id 可空）

> 2026-08-25 修正：`comment_id` 原误标 `NOT NULL`，与「调用失败时为 NULL」语义矛盾（agent-reply 链路需要写失败行）。
> 已建表的存量库由 DB-owner 执行：

```sql
ALTER TABLE domains.ai_agent_reply_log ALTER COLUMN comment_id DROP NOT NULL;
```

## 存量表迁移（去除一帖一回防重）

> 2026-08-26 变更：需求调整为「同一帖子可被同一机器人多次关键词触发回复」，
> 用量仅靠频率限制（max_replies_per_hour / min_interval_sec）约束，不再设防重。
> 已建表的存量库由 DB-owner 执行：

```sql
DROP INDEX IF EXISTS domains.idx_ai_reply_unique;
```

## 存量表迁移（两阶段回复：filter_prompt 列 + status=2 语义）

> 2026-08-26 变更：新增「回复判定条件」列支撑两阶段（分类器+生成器）回复链路
> （设计见 docs/agent-reply-filter-design.md）；`ai_agent_reply_log.status` 扩展取值 2=分类器跳过
> （无结构变更，仅注释语义扩展；限频统计口径改为排除 status=2，由应用层查询条件保证）。
> 已建表的存量库由 DB-owner 执行：

```sql
ALTER TABLE domains.ai_agent ADD COLUMN filter_prompt TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN domains.ai_agent.filter_prompt IS '回复判定条件(自然语言,如"只回复编程相关问题";空=不判定直接回复;仅trigger_mode=2关键词触发生效,见docs/agent-reply-filter-design.md)';
COMMENT ON COLUMN domains.ai_agent_reply_log.status IS '结果: 0=失败, 1=成功, 2=分类器跳过, 3=分类器超时降级直回(运维事件,不参与限频统计)';
```

## 存量表迁移（圈子级机器人：circle_id/creator_id 列 + 名称唯一索引重定作用域）

> 2026-08-31 变更：支持圈子级 AI 机器人——圈主/管理员管理本圈机器人（每圈上限 5），
> 设计见 [circle-agent-manage-design.md](../circle-agent-manage-design.md)。
> `circle_id` NULL=平台全局机器人（存量行全部保持 NULL，语义与行为零变化）；
> 名称唯一索引从 `(name) WHERE deleted=0` 全局唯一改为 `(COALESCE(circle_id, 全零UUID), name)`
> 作用域桶唯一——存量名称本就全局唯一（同落全零桶），新索引可直接建、不会撞。
> 已建表的存量库由 DB-owner 执行：

```sql
ALTER TABLE domains.ai_agent ADD COLUMN circle_id UUID NULL;
ALTER TABLE domains.ai_agent ADD COLUMN creator_id UUID NULL;
COMMENT ON COLUMN domains.ai_agent.circle_id IS '绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=平台全局机器人(超管维护);创建后不可变';
COMMENT ON COLUMN domains.ai_agent.creator_id IS '创建者用户ID(users.id,审计用)';

DROP INDEX IF EXISTS domains.idx_ai_agent_name;
CREATE UNIQUE INDEX idx_ai_agent_name ON domains.ai_agent
    (COALESCE(circle_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
    WHERE deleted = 0;

CREATE INDEX idx_ai_agent_circle ON domains.ai_agent (circle_id) WHERE deleted = 0;
```

## 存量表迁移（自动回复：auto_config / trigger_source / target_comment_id + 部分唯一索引）

> 2026-10-23 变更（#99）：机器人自动回复高热度帖子 / 评论，设计见 [agent-auto-reply-design.md](../agent-auto-reply-design.md)。
> 权威 DDL 在 `migrations/20261026222115_ai_agent_auto_reply.up.sql`（加列 + 存量回填）与
> `migrations/20261026222116_ai_reply_log_auto_target_unique.up.sql`（`CREATE UNIQUE INDEX CONCURRENTLY`），
> 发布流水线以 DB-owner 自动执行，无需手工操作。
>
> 存量行 `trigger_source` 回填规则（存量日志未记录来源，按回复形态 + 机器人触发模式推断）：
> status=2/3 → 1；回复挂在评论楼层内 → 1（同时回填 `target_comment_id`）；顶层回复：手动模式机器人 → 3，其余 → 2；
> 无评论的失败行按机器人当前触发模式推断（2 → 1，3 → 3，其余 → 2）。
