-- users_agent_mention_disabled
-- 机器人账号"不可被 @"投影列（#171）：ai_agent 停用 / 删除时写 1，启用时写 0；经 CDC 同步 ES user 索引，
-- @选人（GET /user/search）按它过滤，发帖 / 评论 mention 兜底按它剔除。
-- 取值用"禁用标记"而非"可 @ 标记"：零值 0 即正常语义——GORM 对带 default 的零值字段会省略写入、
-- userinfo 缓存 JSON 不含该列（json:"-"），零值都必须落在安全的一侧（普通用户恒为 0）。
-- 兼容性：只加带常量默认值的 NOT NULL 列（PG 11+ 不重写表），旧版本应用不读不写，照常工作（expand）。
-- 存量回填不放在本迁移：发布流水线里迁移先于 ES sink connector 重新注册（release.yml migrate → deploy →
-- register-connectors.sh），迁移内的回填 UPDATE 会被旧字段白名单丢掉新列，ES 永远收不到。
-- 回填改为发版后执行的一次性幂等脚本，见 docs/pgsql-ddl/user.md「agent_mention_disabled 存量回填」。
-- users 已在 CDC（cdc_tables / es_sinks.users），字段白名单同步加了本列（deploy/ansible/group_vars/all.yml）。
SET lock_timeout = '5s';

ALTER TABLE domains.users ADD COLUMN IF NOT EXISTS agent_mention_disabled SMALLINT NOT NULL DEFAULT 0;
COMMENT ON COLUMN domains.users.agent_mention_disabled IS '机器人不可被@: 0=正常(普通用户恒为0), 1=机器人已停用或已删除(ai_agent 状态投影,@选人与mention兜底排除);仅 role=2 有意义';
