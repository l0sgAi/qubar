import request from '@/utils/request'

/**
 * 获取分类列表
 * @returns {Promise} 返回分类列表
 */
export function getCategories() {
  return request({
    url: '/category/get',
    method: 'get'
  })
}

/**
 * 创建兴趣圈
 * @param {Object} data - 兴趣圈数据
 * @param {string} data.name - 兴趣圈名称
 * @param {string} data.slug - 唯一标识符（用于URL SEO）
 * @param {string} data.avatar_url - 兴趣圈头像URL
 * @param {string} data.cover_url - 背景图URL
 * @param {string} data.description - 描述信息
 * @param {string} data.rule - 圈子规则/公告
 * @param {string} data.category_id - 分类ID(UUIDv7)
 * @param {number} data.join_type - 加入方式 0=公开（直接加入） 2=私密（邀请制）；1（需审核）已下线，传入返回 400
 * @returns {Promise}
 */
export function createCircle(data) {
  return request({
    url: '/circle/create',
    method: 'post',
    data
  })
}

/**
 * 搜索兴趣圈（使用 Elasticsearch search_after 分页）
 * @param {Object} params - 搜索参数
 * @param {string} params.keyword - 搜索关键字，匹配圈子名称和描述
 * @param {number} params.size - 每页数量，范围 1-100，默认 20
 * @param {string} params.search_after - 上一页返回的 search_after 值（JSON字符串）
 * @returns {Promise}
 */
export function searchCircles(params) {
  return request({
    url: '/circle/list',
    method: 'get',
    params
  })
}

/**
 * 获取圈子详情
 * 非成员（含访客）看私密圈时只返回 id / name / avatar_url / join_type / member_count，
 * 并带 restricted: true（后端 #170），前端展示「仅成员可见」精简页。
 * @param {string} id - 圈子ID(UUIDv7)
 * @returns {Promise} 返回圈子详情 CircleDetailVO
 */
export function getCircleDetail(id) {
  return request({
    url: `/circle/detail/${id}`,
    method: 'get'
  })
}

/**
 * 加入兴趣圈（只对公开圈生效）：私密圈 403（需邀请）、已拉黑 403、已是成员 409
 * @param {Object} data - 加入数据
 * @param {string} data.circle_id - 圈子ID(UUIDv7)
 * @returns {Promise}
 */
export function joinCircle(data) {
  return request({
    url: '/circle/join',
    method: 'post',
    data
  })
}

/**
 * 退出兴趣圈
 * @param {Object} data - 退出数据
 * @param {string} data.circle_id - 圈子ID(UUIDv7)
 * @returns {Promise}
 */
export function leaveCircle(data) {
  return request({
    url: '/circle/leave',
    method: 'post',
    data
  })
}

/**
 * 获取圈子帖子列表（私密圈的非成员 / 访客返回 403）
 * @param {Object} params - 查询参数
 * @param {string} params.circle_id - 圈子ID（必填，UUIDv7）
 * @param {number} params.type - 1=近期热点 2=最新 3=精华（必填）
 * @param {number} params.size - 每页数量，默认20，上限100
 * @param {string} params.search_after - 分页游标（上页响应中的值）
 * @returns {Promise}
 */
export function getCirclePosts(params) {
  return request({
    url: '/circle/posts',
    method: 'get',
    params
  })
}

/**
 * 获取圈子成员列表（仅圈主/管理员可用，普通成员返回 403）
 * @param {Object} params - 查询参数
 * @param {string} params.circle_id - 圈子ID（必填，UUIDv7）
 * @param {string} [params.role] - 按角色过滤：'10'/'20'/'30'；不传或 '-1'=全部（注意是字符串）
 * @param {string} [params.status] - 按状态过滤：'1'-'4'；不传或 '-1'=全部（注意是字符串）。'0'（待审核）已废弃，恒为空
 * @param {string} [params.keyword] - 按用户名/邮箱搜索（同一套匹配引擎，username 权重 3 倍，
 *   相关性排序模糊匹配；最多返回 100 个命中用户对应的成员；空=不过滤。
 *   keyword 与 cursor 必须配套：翻页带同一 keyword，关键词变化重置 cursor；
 *   可与 role/status 叠加；响应不含 email；搜索服务不可用返回 code=210）
 * @param {string} [params.cursor] - 上一页返回的 cursor 原样回传；首页不传；空字符串=没有更多页
 * @param {number} [params.size] - 每页数量，≤0 或 >100 按 20 处理
 * @returns {Promise} data: { members: Array, size: number, cursor: string }
 */
export function getCircleMembers(params) {
  return request({
    url: '/circle/members',
    method: 'get',
    params
  })
}

/**
 * 获取我可管理的圈子列表（我是圈主 role=30 / 管理员 role=20 的圈子）
 * AI 代理管理控制台的圈子选择器数据源；与 /circle/my（我加入的所有圈子）不同，
 * 本接口只返回管理角色圈子，直查 DB 无缓存，角色变更即时可见。
 * @param {Object} params - 查询参数
 * @param {string} [params.keyword] - 按 name/description 子串过滤（大小写不敏感，% _ \ 按字面匹配）
 * @param {number} [params.page] - 页码从 1 开始，<=0 按 1 处理
 * @param {number} [params.size] - 每页条数，<=0 或 >100 回落为 20
 * @returns {Promise} 分页信封：{ total, page, per_page, data?: ManagedCircleItem[] }
 *   注意：空结果时 data 键整体缺失，调用方必须 res.data ?? [] 兜底；
 *   排序固定（圈主在前，同角色按建圈时间新→旧）；列表含非正常状态圈子（status 0/2），前端按需置灰
 */
export function getManagedCircles(params) {
  return request({
    url: '/circle/manage/list',
    method: 'get',
    params
  })
}

/**
 * 设为/取消管理员（仅圈主可用）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @param {number} data.role - 仅接受 20=设为管理员、10=取消管理员恢复普通成员
 * @returns {Promise}
 */
export function setMemberRole(data) {
  return request({
    url: '/circle/manage/role',
    method: 'post',
    data
  })
}

/**
 * 转让圈主（仅圈主可用，不可逆；转让后当前用户立即降为普通成员）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID（须为正常在圈的非圈主成员）
 * @returns {Promise}
 */
export function transferOwnership(data) {
  return request({
    url: '/circle/manage/transfer',
    method: 'post',
    data
  })
}

/**
 * 禁言成员（圈主/管理员；仅能禁言正常在圈成员，已在禁言中再禁返回 409）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @param {number} data.duration_hours - 禁言时长（小时），1-720（30 天）
 * @returns {Promise}
 */
export function muteMember(data) {
  return request({
    url: '/circle/manage/mute',
    method: 'post',
    data
  })
}

/**
 * 解除禁言（圈主/管理员；目标不是禁言中状态返回 409）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @returns {Promise}
 */
export function unmuteMember(data) {
  return request({
    url: '/circle/manage/unmute',
    method: 'post',
    data
  })
}

/**
 * 拉黑/踢出成员（圈主/管理员；可拉黑正常或禁言中成员，成员数 -1，对方无法重新加入直到解黑）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @returns {Promise}
 */
export function banMember(data) {
  return request({
    url: '/circle/manage/ban',
    method: 'post',
    data
  })
}

/**
 * 解除拉黑（圈主/管理员；解黑后对方落为已退出(4)，不会自动回圈；公开圈可重新加入，私密圈需重新邀请）
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @returns {Promise}
 */
export function unbanMember(data) {
  return request({
    url: '/circle/manage/unban',
    method: 'post',
    data
  })
}

/**
 * 移出成员（圈主/管理员；后端 #168）：正常或禁言中成员 → 已退出(4)，成员数 -1。
 * 不拉黑：公开圈可重新直接加入，私密圈需重新邀请。权限同拉黑：只能移出角色严格低于自己的人，
 * 不能移出自己 / 圈主（403）；对已拉黑 / 已退出成员 409；目标不是该圈成员 404。
 * 原入圈审核接口 POST /circle/manage/review 已随审核下线删除。
 * @param {Object} data - 操作数据
 * @param {string} data.circle_id - 圈子ID
 * @param {string} data.target_user_id - 目标用户ID
 * @returns {Promise}
 */
export function removeMember(data) {
  return request({
    url: '/circle/manage/remove',
    method: 'post',
    data
  })
}

/**
 * 编辑圈子资料（圈主可改全部字段；管理员仅 avatar/cover/description/rule）。
 * 只传需要修改的字段，未传字段保持不变；一个字段都不传返回 400。
 * 特殊语义：slug 传 "" 清空；category_id 传全零 UUID 清除分类；avatar_url/cover_url/rule 传 "" 清空；description 不可清空。
 * @param {Object} data - 变更字段（按权限裁剪）
 * @param {string} data.circle_id - 圈子ID（必填）
 * @param {string} [data.name] - 圈子名称，1-50 字（仅圈主，重名 409）
 * @param {string} [data.slug] - 唯一标识 ≤60 字（仅圈主，重名 409）
 * @param {number} [data.join_type] - 加入方式 0=公开 / 2=私密（仅圈主；1 需审核已下线，传入 400）
 * @param {string} [data.category_id] - 分类 UUID（仅圈主）
 * @param {string} [data.avatar_url] - 头像 URL ≤500 字
 * @param {string} [data.cover_url] - 封面 URL ≤500 字
 * @param {string} [data.description] - 简介 1-2000 字
 * @param {string} [data.rule] - 规则 ≤2000 字
 * @returns {Promise}
 */
export function updateCircle(data) {
  return request({
    url: '/circle/update',
    method: 'put',
    data
  })
}

// ==================== 圈内 AI 机器人管理（/circle/agent）====================
// 与全局 /agent/* 控制台互不相通：两套机器人 ID 各自独立，跨作用域访问一律 404。
// 权限：列表/详情/创建/运营字段更新为 admin+（20/30）；凭据字段更新与删除仅圈主（30）。
// 注意：本期圈内机器人不参与任何回复触发（关键词/手动/@提及均不生效），仅作为配置资产存在。

/**
 * 创建圈内机器人（admin+；每圈上限 5 个，超出返回 409）
 * 创建成功返回 200，机器人默认启用。管理列表使用 updateCircleAgent 调整状态。
 * @param {Object} data - 机器人数据
 * @param {string} data.circle_id - 归属圈子 ID（必填，来自 /circle/manage/list）
 * @param {string} data.name - 展示名，圈内唯一，1-50 字符（UTF-8 字节，中文约 16 字）
 * @param {string} [data.avatar_url] - 头像 URL，可空
 * @param {string} data.api_protocol - 协议：openai/anthropic
 * @param {string} [data.base_url] - 自定义 API 地址，可空
 * @param {string} [data.api_key] - 明文提交，服务端加密存储，响应永不回显
 * @param {string} data.model - 模型名，1-100 字符
 * @param {Object} [data.llm_params] - LLM 参数，白名单键且值为数字
 * @param {string} [data.system_prompt] - 系统提示词，可空
 * @param {string} [data.filter_prompt] - 回复判定条件，≤2000 字符，可空
 * @param {number} [data.trigger_mode] - 触发模式：1=全部新帖（本期不生效）2=关键词 3=手动
 * @param {string[]} [data.trigger_keywords] - 触发关键词；mode=2 时必须非空
 * @param {number} [data.max_replies_per_hour] - 每小时回复上限，0=不限
 * @param {number} [data.min_interval_sec] - 最小回复间隔秒，0=不限
 * @param {Object} [data.auto_config] - 自主行为配置：auto_reply 自动回复本圈高热度帖子 / 评论，
 *   字段与校验同全局机器人（见 createAgent / src/utils/agentAutoReply.js），不传 = 不自动回复
 * @returns {Promise} data 为创建后的完整圈内机器人对象（含 circle_id）
 */
export function createCircleAgent(data) {
  return request({
    url: '/circle/agent',
    method: 'post',
    data
  })
}

/**
 * 获取圈内机器人列表（admin+；offset 分页）
 * @param {Object} params - 查询参数
 * @param {string} params.circle_id - 圈子 ID（必填，非法 UUID 返回 400）
 * @param {string} [params.keyword] - 按机器人 name 子串过滤（大小写不敏感）
 * @param {number} [params.page] - 页码，默认 1（<=0 规整为 1）
 * @param {number} [params.size] - 每页数量，默认 20（<=0 或 >100 回落 20）
 * @returns {Promise} 分页信封：{ total, page, per_page, data?: CircleAgent[] }
 *   注意：空结果时 data 键整体缺失（omitempty），调用方必须 res.data ?? [] 兜底；
 *   total 可用于配额展示（每圈上限 5）
 */
export function getCircleAgentList(params) {
  return request({
    url: '/circle/agent/list',
    method: 'get',
    params
  })
}

/**
 * 获取圈内机器人详情（admin+）
 * 机器人不存在 / 属于其他圈 / 是全局机器人 → 一律 404（不区分原因）。
 * @param {string} id - 机器人 ID(UUID)
 * @returns {Promise} data 为完整圈内机器人对象；
 *   可选字符串字段（avatar_url/base_url/system_prompt/filter_prompt/api_key_masked）
 *   为空时键缺失，读取时用 ?? '' 兜底
 */
export function getCircleAgentDetail(id) {
  return request({
    url: `/circle/agent/${id}`,
    method: 'get'
  })
}

/**
 * 部分更新圈内机器人（指针语义：只传要改的字段，未传字段不动；全部不传返回 400）
 * 权限分组：运营字段（name/avatar_url/model/llm_params/system_prompt/filter_prompt/
 * trigger_mode/trigger_keywords/max_replies_per_hour/min_interval_sec/auto_config/status）admin+ 可改；
 * 凭据字段（api_protocol/base_url/api_key）仅圈主——请求体中任一凭据字段非空即整体
 * 要求圈主，管理员混提也会 403，前端须按表单分组控制提交。
 * 特殊语义：api_key 传空串=清除，不传=保持不变；llm_params/trigger_keywords 整体替换；
 * auto_config.auto_reply（运营字段，admin+ 可改）传了即整体替换该子对象，不传保持不变。
 * @param {string} id - 机器人 ID(UUID)
 * @param {Object} data - 只含变更字段的对象（按权限裁剪）
 * @returns {Promise} data 为更新后的完整圈内机器人对象
 */
export function updateCircleAgent(id, data) {
  return request({
    url: `/circle/agent/${id}`,
    method: 'put',
    data
  })
}

/**
 * 删除圈内机器人（仅圈主；软删=停用+标记删除，名称随即释放可复用，无恢复入口）
 * @param {string} id - 机器人 ID(UUID)
 * @returns {Promise} 成功响应 data 为空
 */
export function deleteCircleAgent(id) {
  return request({
    url: `/circle/agent/${id}`,
    method: 'delete'
  })
}

/**
 * 圈主手动触发本圈启用的手动机器人回复本圈已发布且未锁定的帖子。
 * @param {string} id - 圈内机器人 UUID
 * @param {string} postId - 帖子 UUID
 * @returns {Promise} data 为生成的评论 UUID。同步生成，失败不自动重发。
 */
export function triggerCircleAgentReply(id, postId) {
  return request({
    url: `/circle/agent/${id}/reply/${postId}`,
    method: 'post',
    timeout: 120000
  })
}
