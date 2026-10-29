# 用户域(user)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 用户表

```sql
DROP TABLE IF EXISTS domains.users;

CREATE TABLE domains.users (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    username        VARCHAR(50) NOT NULL,
    email           VARCHAR(100) NOT NULL,
    phone           VARCHAR(20),
    pwd             VARCHAR(512),
    motto           VARCHAR(2048),
    google_id       VARCHAR(100),
    x_id            VARCHAR(100),
    github_id       VARCHAR(100),
    microsoft_id    VARCHAR(100),
    avatar_url      VARCHAR(500),
    agent_circle_id UUID,
    agent_mention_disabled SMALLINT NOT NULL DEFAULT 0,
    gender          SMALLINT DEFAULT 0,
    birthdate       DATE,
    status          SMALLINT DEFAULT 1,
    role            SMALLINT DEFAULT 0,
    deleted         SMALLINT DEFAULT 0,
    create_time     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 添加列注释
COMMENT ON COLUMN domains.users.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';
COMMENT ON COLUMN domains.users.username IS '用户名/昵称';
COMMENT ON COLUMN domains.users.email IS '邮箱(唯一凭证)';
COMMENT ON COLUMN domains.users.google_id IS 'Google平台唯一ID';
COMMENT ON COLUMN domains.users.x_id IS 'X/Twitter平台唯一ID';
COMMENT ON COLUMN domains.users.github_id IS 'Github平台唯一ID';
COMMENT ON COLUMN domains.users.agent_circle_id IS '机器人绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=普通用户或全局机器人;仅 role=2 行有意义(ai_agent.circle_id 的投影,供 @提及 作用域过滤)';
COMMENT ON COLUMN domains.users.agent_mention_disabled IS '机器人不可被@: 0=正常(普通用户恒为0), 1=机器人已停用或已删除(ai_agent 状态投影,@选人与mention兜底排除);仅 role=2 有意义';
COMMENT ON COLUMN domains.users.status IS '状态：0=禁用，1=启用';
COMMENT ON COLUMN domains.users.deleted IS '逻辑删除：0=正常，1=已删除';

-- 索引
CREATE UNIQUE INDEX idx_users_email ON domains.users(email);
CREATE INDEX idx_users_google_id ON domains.users(google_id);
CREATE INDEX idx_users_x_id ON domains.users(x_id);
CREATE INDEX idx_users_github_id ON domains.users(github_id);
CREATE INDEX idx_users_phone ON domains.users(phone);
```

## 存量表迁移（agent_circle_id 可空列）

> 2026-08-31 变更：圈内机器人 @提及 可见范围改造——users 行记录机器人绑定圈子
> （`ai_agent.circle_id` 的投影，CDC 同步 ES user 文档后供 `GET /user/search` 作用域过滤，
> 设计见 [../circle-agent-mention-scope-design.md](../circle-agent-mention-scope-design.md)）。
> `agent_circle_id` NULL=普通用户或全局机器人；不加索引：过滤全在 ES 侧，PG 仅按主键回查。
> 已建表的存量库由 DB-owner 执行：

```sql
ALTER TABLE domains.users ADD COLUMN agent_circle_id uuid NULL;
COMMENT ON COLUMN domains.users.agent_circle_id IS
    '机器人绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=普通用户或全局机器人;仅 role=2 行有意义';

-- 存量圈内机器人回填投影（幂等，可重复执行）：
-- 本列由应用层仅在"创建机器人"时写入，本功能上线前已创建的圈内机器人
-- （ai_agent.circle_id 非 NULL 但 users 行无绑定）没有任何代码路径会补写，
-- 不回填则其 users 行永远落入"非圈内机器人"桶、全站 @ 列表可见（作用域失效）。
-- ai_agent.circle_id 为权威，users 列仅投影；只回填未删除的圈内机器人。
UPDATE domains.users u
SET agent_circle_id = a.circle_id
FROM domains.ai_agent a
WHERE a.linked_user_id = u.id
  AND a.deleted = 0
  AND a.circle_id IS NOT NULL
  AND u.agent_circle_id IS DISTINCT FROM a.circle_id;

-- ⚠️ 已废弃（#171，2026-10-29）：删除圈内机器人不再清空绑定（清空后会落入"全局机器人"桶、全站可见），
-- 下面这段不要再执行；停用 / 删除 / 孤儿机器人改由 agent_mention_disabled 排除，见下节回填脚本。
-- 清孤儿绑定（幂等）：users 行有绑定但已无对应未删除圈内机器人——
-- 建机器人时 users 行写入成功而 ai_agent 插入失败（如每圈限额 409）的遗留，
-- 或删除机器人时清列失败的补偿（删除链路清列 fail-open，见设计文档风险表）。
UPDATE domains.users u
SET agent_circle_id = NULL
WHERE u.agent_circle_id IS NOT NULL
  AND u.role = 2
  AND NOT EXISTS (
      SELECT 1 FROM domains.ai_agent a
      WHERE a.linked_user_id = u.id
        AND a.deleted = 0
        AND a.circle_id = u.agent_circle_id
  );
```

## agent_mention_disabled 存量回填（#171，发版后一次性执行）

> 2026-10-29 变更：机器人停用 / 删除后不可再被 @（设计见
> [../circle-agent-mention-scope-design.md](../circle-agent-mention-scope-design.md) §九）。
> 列由迁移 `20261029191406_users_agent_mention_disabled` 新增（`NOT NULL DEFAULT 0`）。应用层只在创建、
> 改状态、删除机器人时写它，**上线前已经停用 / 删除的机器人没有任何代码路径会补写**，需要一次性回填。
>
> **执行时机：release 的 deploy job 跑完 `register-connectors.sh` 之后**（ES sink 字段白名单已包含
> `agent_mention_disabled`）。不能放进迁移：迁移先于 connector 重新注册，回填产生的 CDC 事件会被旧白名单
> 丢掉新列，ES 文档永远收不到。脚本幂等，可重复执行；回填出来的 UPDATE 经 CDC 同步到 ES user 索引。
> 执行：`docker exec -i qubar-postgres psql -U qubar_owner -d qubar -v ON_ERROR_STOP=1 < 本段 SQL`。

```sql
BEGIN;

-- 1. 不可 @：机器人账号（role=2）没有对应的「未删除且启用」ai_agent 行 → 1。
--    覆盖已停用、已删除的机器人，以及建号成功但 ai_agent 插入失败（如每圈限额 409）留下的孤儿账号。
UPDATE domains.users u
SET agent_mention_disabled = 1
WHERE u.role = 2
  AND u.agent_mention_disabled = 0
  AND NOT EXISTS (
      SELECT 1 FROM domains.ai_agent a
      WHERE a.linked_user_id = u.id
        AND a.deleted = 0
        AND a.status = 1
  );

-- 2. 恢复已删除圈内机器人的圈子绑定：旧版本删除链路把 agent_circle_id 清成了 NULL（落入全局桶）。
--    第 1 步已让它们不可 @，这一步只让投影与 ai_agent.circle_id 重新一致（纵深防御）。
UPDATE domains.users u
SET agent_circle_id = a.circle_id
FROM domains.ai_agent a
WHERE a.linked_user_id = u.id
  AND a.deleted = 1
  AND a.circle_id IS NOT NULL
  AND u.agent_circle_id IS NULL;

COMMIT;

-- 核对（应为 0 行）：启用中的机器人却被标记不可 @ / 停用或删除的机器人仍可 @。
SELECT u.id, u.agent_mention_disabled, a.status, a.deleted
FROM domains.users u
JOIN domains.ai_agent a ON a.linked_user_id = u.id
WHERE (a.deleted = 0 AND a.status = 1) = (u.agent_mention_disabled = 1);
```
