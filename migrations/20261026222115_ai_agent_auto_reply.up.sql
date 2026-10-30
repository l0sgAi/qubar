-- ai_agent_auto_reply
-- 机器人自动回复高热度帖子 / 评论（#99）：ai_agent.auto_config + ai_agent_reply_log.trigger_source / target_comment_id。
-- 兼容性：只加带常量默认值的列 / 可空列，旧版本应用不读不写这些列，照常工作（expand）。
--   旧版本在「迁移后、发版前」窗口内写入的日志行 trigger_source 取默认值 0（未知），不影响任何读路径。
-- 部分唯一索引（trigger_source = 4）在下一个迁移文件里 CONCURRENTLY 创建（不能放进本文件的隐式事务）。
-- 表均不在 CDC（cdc_tables 无 ai_agent / ai_agent_reply_log），无需登记。
-- 可重复执行：加列用 IF NOT EXISTS、回填只处理 trigger_source = 0 的行（共享开发库曾以旧版本号 20261023164311 执行过本迁移）。
SET lock_timeout = '5s';

-- 1. 机器人自主行为配置（jsonb 对象）：本期只用 auto_reply 子对象，后续自动发帖（#101）复用为 auto_post 兄弟键。
ALTER TABLE domains.ai_agent ADD COLUMN IF NOT EXISTS auto_config JSONB NOT NULL DEFAULT '{}'::JSONB;
COMMENT ON COLUMN domains.ai_agent.auto_config IS '自主行为配置(JSON对象,与trigger_mode互不影响): auto_reply={enabled,targets,post_hot_min,comment_like_min,comment_reply_min,window_hours,max_per_hour}; 各子对象按顶层key独立写入';

-- 2. 回复日志：触发来源 + 目标评论
ALTER TABLE domains.ai_agent_reply_log ADD COLUMN IF NOT EXISTS trigger_source SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE domains.ai_agent_reply_log ADD COLUMN IF NOT EXISTS target_comment_id UUID;
COMMENT ON COLUMN domains.ai_agent_reply_log.trigger_source IS '触发来源: 1=评论关键词, 2=@提及, 3=手动, 4=自动回复(定时扫描); 0=未知(迁移窗口期旧版本写入)';
COMMENT ON COLUMN domains.ai_agent_reply_log.target_comment_id IS '被回复的目标评论ID(comment.id;回复挂在该评论楼层内时非NULL,回复帖子本身为NULL)';
COMMENT ON COLUMN domains.ai_agent_reply_log.status IS '结果: 0=失败, 1=成功, 2=分类器跳过, 3=分类器超时降级直回(运维事件,不参与限频统计), 4=自动回复占位(执行中;进程中断则保持4,视为已触发不重试)';

-- 3. 存量行回填 trigger_source（存量日志未记录来源，按回复形态 + 机器人触发模式推断）：
--    - status=2/3（分类器判定 / 超时降级）只有关键词触发会走分类器 → 1；
--    - 回复挂在评论楼层内（reply_to_id 非空）只有关键词触发会这样 → 1，同时回填 target_comment_id；
--    - 顶层回复：手动模式机器人 → 3（手动；被 @ 的手动模式机器人无法区分，归为手动），其余 → 2（@提及）；
--    - 无评论的失败行：按机器人当前触发模式推断（2=关键词 → 1，3=手动 → 3，其余 → 2）。
--    日志表数据量小（机器人回复受限频约束），单条 UPDATE 足够。
WITH src AS (
    SELECT l.id,
           CASE
               WHEN l.status IN (2, 3) THEN 1
               WHEN c.reply_to_id IS NOT NULL THEN 1
               WHEN a.trigger_mode = 3 THEN 3
               WHEN c.id IS NOT NULL THEN 2
               WHEN a.trigger_mode = 2 THEN 1
               ELSE 2
           END AS trigger_source,
           c.reply_to_id AS target_comment_id
    FROM domains.ai_agent_reply_log l
    LEFT JOIN domains.ai_agent a ON a.id = l.agent_id
    LEFT JOIN domains.comment c ON c.id = l.comment_id
    WHERE l.trigger_source = 0
)
UPDATE domains.ai_agent_reply_log l
SET trigger_source = src.trigger_source,
    target_comment_id = src.target_comment_id
FROM src
WHERE l.id = src.id;
