-- squawk-disable-assume-in-transaction
-- ai_reply_log_auto_target_unique：自动回复（trigger_source=4）每个机器人对同一目标最多一行（#99）。
-- 目标 = (post_id, target_comment_id)，回复帖子本身时 target_comment_id 为 NULL，用全零 UUID 归桶。
-- 列顺序 post_id 在前：同一索引同时服务「每目标已有自动回复数」统计（max_agents_per_target）。
-- 应用先插占位行再调 LLM，撞此索引即跳过（多实例 / 并发兜底）。存量行 trigger_source 均为 0-3，不会冲突。
-- squawk-ignore require-lock-timeout
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_ai_reply_auto_target ON domains.ai_agent_reply_log (post_id, (COALESCE(target_comment_id, '00000000-0000-0000-0000-000000000000'::uuid)), agent_id) WHERE trigger_source = 4;
