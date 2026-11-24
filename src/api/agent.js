import request from '@/utils/request'

/**
 * 创建 AI 回复机器人
 * 创建成功后后端自动生成 role=2 机器人账号（linked_user_id，只读，前端不传）。
 * 创建成功返回 200，机器人默认启用。管理列表使用 updateAgent 调整状态。
 * @param {Object} data - 机器人数据
 * @param {string} data.name - 展示名，全局唯一，1-50 字符
 * @param {string} [data.avatar_url] - 头像 URL，可空
 * @param {string} data.api_protocol - 协议：openai/anthropic
 * @param {string} [data.base_url] - 自定义 API 地址，用官方默认端点时留空
 * @param {string} [data.api_key] - 明文提交（仅 HTTPS）
 * @param {string} data.model - 模型名，1-100 字符
 * @param {Object} [data.llm_params] - LLM 参数，白名单键且值为数字：
 *   temperature/top_p/max_tokens/presence_penalty/frequency_penalty
 * @param {string} [data.system_prompt] - 系统提示词，可空
 * @param {string} [data.filter_prompt] - 回复判定条件（自然语言，≤2000 字符），
 *   仅 mode=2 关键词触发生效；空 = 不判定，命中关键词即回复
 * @param {number} [data.trigger_mode] - 触发模式：1=全部新帖（默认） 2=关键词 3=手动；
 *   mode=2 时 trigger_keywords 必填非空
 * @param {string[]} [data.trigger_keywords] - 触发关键词，默认 []
 * @param {number} [data.max_replies_per_hour] - 每小时回复上限，0=不限，默认 30
 * @param {number} [data.min_interval_sec] - 两次回复最小间隔秒，0=不限，默认 60
 * @param {Object} [data.auto_config] - 自主行为配置，不传 = 不自动回复；
 *   auto_reply: { enabled, targets: ('post'|'comment')[], post_hot_min, comment_like_min,
 *   comment_reply_min, window_hours(1-168), max_per_hour(0-1000) }，规则见后端文档 §2.5
 *   auto_post: { enabled, interval_min, daily_max, active_hours, topics, require_review }，见 §2.6
 * @returns {Promise} data 为完整 AgentVO（auto_config.auto_reply 未配置时回显 enabled=false 的零值）
 */
export function createAgent(data) {
  return request({
    url: '/agent',
    method: 'post',
    data
  })
}

/**
 * 获取机器人列表（offset 分页，固定 create_time 倒序）
 * @param {Object} params - 查询参数
 * @param {number} [params.page] - 页码，默认 1
 * @param {number} [params.size] - 每页数量，默认 20，上限 100（越界回落 20）
 * @param {string} [params.keyword] - 按名称模糊匹配（ILIKE 大小写不敏感，忽略两端空格）；
 *   不传或为空 = 全量。total 为过滤后总数，直接可用于分页组件
 * @returns {Promise} 分页信封：data 为 AgentVO[]，额外带 total/page/per_page
 */
export function getAgentList(params) {
  return request({
    url: '/agent/list',
    method: 'get',
    params
  })
}

/**
 * 获取机器人详情
 * @param {string} id - 机器人 ID(UUIDv7)；不存在/已删除返回 404
 * @returns {Promise} data 为完整 AgentVO
 */
export function getAgentDetail(id) {
  return request({
    url: `/agent/${id}`,
    method: 'get'
  })
}

/**
 * 部分更新机器人（指针语义：只传要改的字段，未传字段不动）
 * 注意：
 * - api_key 传空串 = 清除 key；要「保持不变」就不传该字段
 * - filter_prompt 传空串 = 关闭判定（回到命中即回复）；不传 = 保持原条件
 * - llm_params / trigger_keywords 是整体替换语义（传了就覆盖全量），不是合并
 * - auto_config.auto_reply / auto_post 传了即整体替换该子对象（未传字段按 0 / false 处理），不传保持不变
 * - trigger_mode 改为 2 时需同请求带非空 trigger_keywords（或已存关键词非空）
 * - 空请求体（无任何可更新字段）返回 400
 * @param {string} id - 机器人 ID(UUIDv7)
 * @param {Object} data - 只含变更字段的对象，可更新字段：
 *   name/avatar_url/api_protocol/base_url/api_key/model/llm_params/system_prompt/
 *   filter_prompt/trigger_mode/trigger_keywords/max_replies_per_hour/min_interval_sec/status/
 *   auto_config
 * @returns {Promise} data 为更新后的完整 AgentVO
 */
export function updateAgent(id, data) {
  return request({
    url: `/agent/${id}`,
    method: 'put',
    data
  })
}

/**
 * 删除机器人（软删：deleted=1 且自动停用，不可恢复，无恢复接口）
 * 其关联机器人账号与历史回复保留。
 * @param {string} id - 机器人 ID(UUIDv7)
 * @returns {Promise} 成功响应 data 为空
 */
export function deleteAgent(id) {
  return request({
    url: `/agent/${id}`,
    method: 'delete'
  })
}

/**
 * 手动触发机器人在指定帖子下发表一条 AI 回复（顶层评论）
 * 同步执行（含 LLM 调用，耗时可达数十秒），前端需给足超时与 loading。
 * 前置条件（不满足返回 400/404/429，且不产生回复日志）：
 * - 机器人存在且启用（status=1）
 * - 机器人 trigger_mode=3（手动模式）
 * - 帖子已发布且未锁定
 * - 限频未超（失败也算额度）
 * 调用失败（LLM 报错/空回复/落库失败）返回 503，message 含失败原因摘要。
 * @param {string} id - 机器人 ID(UUIDv7)
 * @param {string} postId - 帖子 ID(UUID)
 * @returns {Promise} data 为生成的评论 ID(UUID)
 */
export function triggerAgentReply(id, postId) {
  return request({
    url: `/agent/${id}/reply/${postId}`,
    method: 'post',
    timeout: 120000
  })
}
