// Package application 提供 aiagent 领域的应用服务层。
//
// 职责：
//   - 管理端机器人 CRUD 用例编排（role=1 管理员专用，service 层统一鉴权防绕过）；
//   - api_key 应用层加密（AES-256-GCM，密钥 conf.Security.DataKey），响应只回掩码；
//   - 通过 RoleReader / BotUserCreator / BotUserMentionSyncer 等端口跨域访问 user 领域（composition 桥接注入）。
package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/util/crypto"
	"interestBar/pkg/util/netguard"

	"github.com/google/uuid"
)

// RoleReader 跨域端口：读取用户 role（composition 桥接 user.UserService.GetByID，带缓存）。
//
// 独立声明而非 import user.application，避免跨领域包依赖。
type RoleReader interface {
	// GetUserRole 返回用户 role 与是否存在（用户不存在/已删除返回 false）。
	GetUserRole(ctx context.Context, userID uuid.UUID) (int, bool, error)
}

// BotUserCreator 跨域端口：创建机器人关联系统用户（role=2 机器人账号，不发帖不登录）。
type BotUserCreator interface {
	// CreateBotUser 按给定 username/email/avatarURL 创建系统用户，返回其 ID。
	// username 用机器人 name（users.username 无唯一约束，允许重复）；
	// email 由调用方保证全局唯一（users.email 有唯一索引）。
	// circleID 非 nil 时写入 users.agent_circle_id（圈内机器人 @提及 作用域投影；
	// 全局链路传 nil）。mentionable=false 时写入 users.agent_mention_disabled=1
	// （创建即停用的机器人不出现在 @选人列表）。其余语义不变。
	CreateBotUser(ctx context.Context, username, email, avatarURL string, circleID *uuid.UUID, mentionable bool) (uuid.UUID, error)
}

// BotUserMentionSyncer 跨域端口：把机器人启用状态投影到关联系统用户
// （users.agent_mention_disabled，CDC 同步 ES 后供 @选人过滤，mention 兜底也按它剔除）。
// 停用/删除写不可 @，启用写可 @；只动投影列，不动 users.status（GetBriefs 跳过非正常用户，
// 改 status 会让机器人历史评论丢失作者信息）与 agent_circle_id（删除后仍按原圈收口）。
// 实现方需同步刷新 userinfo 缓存，且必须幂等（失败重试会重复写）。
type BotUserMentionSyncer interface {
	SetBotUserMentionable(ctx context.Context, userID uuid.UUID, mentionable bool) error
}

// BotUserProfileUpdater 跨域端口：同步机器人资料到关联系统用户。
// 机器人改名/换头像时同步 users.username/avatar_url，评论区展示才跟随。
type BotUserProfileUpdater interface {
	// UpdateBotUserProfile 部分更新机器人账号的 username/avatar_url（nil 字段不动）。
	UpdateBotUserProfile(ctx context.Context, userID uuid.UUID, username, avatarURL *string) error
}

// 管理员 role 常量（users.role，权威定义在 user 领域/DDL）。
const roleAdmin = 1

// filterPromptMaxLen 判定条件长度上限（字符，防 prompt 膨胀）。
const filterPromptMaxLen = 2000

// llmParamsKeyWhitelist 允许写入 llm_params 的通用参数键。
var llmParamsKeyWhitelist = map[string]bool{
	"temperature":       true,
	"top_p":             true,
	"max_tokens":        true,
	"presence_penalty":  true,
	"frequency_penalty": true,
}

// CreateAgentInput 创建机器人入参。
type CreateAgentInput struct {
	Name              string                 `json:"name"`
	AvatarURL         string                 `json:"avatar_url"`
	APIProtocol       string                 `json:"api_protocol"`
	BaseURL           string                 `json:"base_url"`
	APIKey            string                 `json:"api_key"` // 明文入参，入库前加密
	Model             string                 `json:"model"`
	LLMParams         map[string]interface{} `json:"llm_params"`
	SystemPrompt      string                 `json:"system_prompt"`
	FilterPrompt      string                 `json:"filter_prompt"`
	TriggerMode       int                    `json:"trigger_mode"`
	TriggerKeywords   []string               `json:"trigger_keywords"`
	MaxRepliesPerHour int                    `json:"max_replies_per_hour"`
	MinIntervalSec    int                    `json:"min_interval_sec"`
	Status            *int                   `json:"status"` // nil=默认启用；显式 0=停用
	AutoConfig        *AutoConfigInput       `json:"auto_config"`
}

// AutoConfigInput 自主行为配置入参（auto_config）。子对象按顶层 key 独立写入：
// 传了 auto_reply 即整体替换该子对象，未传的子对象（含其他自主行为）保持不变。
type AutoConfigInput struct {
	AutoReply *domain.AutoReplyConfig `json:"auto_reply"`
}

// AutoConfigVO 自主行为配置视图（未配置的子对象回显零值，即 enabled=false）。
type AutoConfigVO struct {
	AutoReply domain.AutoReplyConfig `json:"auto_reply"`
}

// UpdateAgentInput 更新机器人入参（指针字段，部分更新语义，同 user.UpdateProfileInput）。
type UpdateAgentInput struct {
	Name              *string                `json:"name"`
	AvatarURL         *string                `json:"avatar_url"`
	APIProtocol       *string                `json:"api_protocol"`
	BaseURL           *string                `json:"base_url"`
	APIKey            *string                `json:"api_key"`
	Model             *string                `json:"model"`
	LLMParams         map[string]interface{} `json:"llm_params"`
	SystemPrompt      *string                `json:"system_prompt"`
	FilterPrompt      *string                `json:"filter_prompt"`
	TriggerMode       *int                   `json:"trigger_mode"`
	TriggerKeywords   []string               `json:"trigger_keywords"`
	MaxRepliesPerHour *int                   `json:"max_replies_per_hour"`
	MinIntervalSec    *int                   `json:"min_interval_sec"`
	Status            *int                   `json:"status"`
	AutoConfig        *AutoConfigInput       `json:"auto_config"`
}

// AgentVO 机器人视图（api_key 只回掩码，永不回明文/密文）。
type AgentVO struct {
	ID                uuid.UUID              `json:"id"`
	Name              string                 `json:"name"`
	AvatarURL         string                 `json:"avatar_url,omitempty"`
	LinkedUserID      uuid.UUID              `json:"linked_user_id"`
	CircleID          *uuid.UUID             `json:"circle_id,omitempty"` // 绑定圈子ID;nil=平台全局（不回显）
	APIProtocol       string                 `json:"api_protocol"`
	BaseURL           string                 `json:"base_url,omitempty"`
	HasAPIKey         bool                   `json:"has_api_key"`
	APIKeyMasked      string                 `json:"api_key_masked,omitempty"`
	Model             string                 `json:"model"`
	LLMParams         map[string]interface{} `json:"llm_params"`
	SystemPrompt      string                 `json:"system_prompt,omitempty"`
	FilterPrompt      string                 `json:"filter_prompt,omitempty"`
	TriggerMode       int                    `json:"trigger_mode"`
	TriggerKeywords   []string               `json:"trigger_keywords"`
	MaxRepliesPerHour int                    `json:"max_replies_per_hour"`
	MinIntervalSec    int                    `json:"min_interval_sec"`
	AutoConfig        AutoConfigVO           `json:"auto_config"`
	Status            int                    `json:"status"`
	CreateTime        time.Time              `json:"create_time"`
	UpdateTime        time.Time              `json:"update_time"`
}

// AgentListResult 机器人列表（offset 分页）。
type AgentListResult struct {
	Total  int64     `json:"total"`
	Page   int       `json:"page"`
	Size   int       `json:"size"`
	Agents []AgentVO `json:"data"`
}

// AgentService 是 aiagent 领域的应用服务接口。
//
// 所有方法第一个业务参数为操作者 userID，service 内统一做 role=1 校验，
// handler 无需（也不应）自行判断，防止新增入口漏检。
type AgentService interface {
	// CreateAgent 创建机器人（自动创建关联系统用户 role=2）。
	CreateAgent(ctx context.Context, adminID uuid.UUID, input CreateAgentInput) (*AgentVO, error)
	// GetAgent 获取机器人详情。
	GetAgent(ctx context.Context, adminID, agentID uuid.UUID) (*AgentVO, error)
	// ListAgents offset 分页列表。keyword 非空时按 name 模糊过滤。
	ListAgents(ctx context.Context, adminID uuid.UUID, keyword string, page, size int) (*AgentListResult, error)
	// UpdateAgent 部分字段更新（api_key 传非空指针即换 key）。
	UpdateAgent(ctx context.Context, adminID, agentID uuid.UUID, input UpdateAgentInput) (*AgentVO, error)
	// DeleteAgent 软删（deleted=1 且停用）。
	DeleteAgent(ctx context.Context, adminID, agentID uuid.UUID) error

	// SetRoleReader 注入跨域 role 读取端口（composition 桥接）。
	SetRoleReader(r RoleReader)
	// SetBotUserCreator 注入跨域机器人账号创建端口（composition 桥接）。
	SetBotUserCreator(c BotUserCreator)
	// SetBotUserProfileUpdater 注入跨域机器人资料同步端口（composition 桥接）。
	SetBotUserProfileUpdater(u BotUserProfileUpdater)
	// SetBotUserMentionSyncer 注入跨域机器人可 @ 状态投影端口（composition 桥接；
	// 未注入时跳过投影，仅记日志，见 syncBotUserMentionable fail-open）。
	SetBotUserMentionSyncer(m BotUserMentionSyncer)
}

type agentServiceImpl struct {
	repo           domain.AgentRepository
	roleReader     RoleReader            // 注入前 nil，鉴权 fail-closed
	botUserCreator BotUserCreator        // 注入前 nil，创建时 fail-fast
	botUserUpdater BotUserProfileUpdater // 注入前 nil，改名/换头像时跳过同步
	mentionSyncer  BotUserMentionSyncer  // 注入前 nil，状态变更时跳过投影（fail-open）
}

// NewAgentService 构造一个 AgentService（跨域依赖 setter 注入）。
func NewAgentService(repo domain.AgentRepository) AgentService {
	return &agentServiceImpl{repo: repo}
}

func (s *agentServiceImpl) SetRoleReader(r RoleReader)         { s.roleReader = r }
func (s *agentServiceImpl) SetBotUserCreator(c BotUserCreator) { s.botUserCreator = c }
func (s *agentServiceImpl) SetBotUserProfileUpdater(u BotUserProfileUpdater) {
	s.botUserUpdater = u
}
func (s *agentServiceImpl) SetBotUserMentionSyncer(m BotUserMentionSyncer) { s.mentionSyncer = m }

// ensureAdmin 校验操作者是 role=1 管理员。端口未注入/用户不存在均拒绝（fail-closed）。
func (s *agentServiceImpl) ensureAdmin(ctx context.Context, adminID uuid.UUID) error {
	if s.roleReader == nil {
		return errNotAdmin
	}
	role, ok, err := s.roleReader.GetUserRole(ctx, adminID)
	if err != nil || !ok || role != roleAdmin {
		return errNotAdmin
	}
	return nil
}

// CreateAgent 创建机器人。
func (s *agentServiceImpl) CreateAgent(ctx context.Context, adminID uuid.UUID, input CreateAgentInput) (*AgentVO, error) {
	if err := s.ensureAdmin(ctx, adminID); err != nil {
		return nil, err
	}

	agent, err := validateAndBuildAgent(ctx, input)
	if err != nil {
		return nil, err
	}

	// 名称唯一预检（全局桶；并发兜底靠 idx_ai_agent_name 唯一索引）
	exists, err := s.repo.ExistsByNameInScope(ctx, uuid.Nil, agent.Name, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errAgentNameExists
	}

	// 创建关联系统用户（role=2 机器人账号）。
	// username 直接用机器人 name（users.username 允许重复，评论区展示该名）；
	// email 需全局唯一（users.email 唯一索引），用 uuidv7 + 时间戳保证。
	if s.botUserCreator == nil {
		return nil, errNotAdmin // 端口未注入视为装配错误，拒绝创建
	}
	agentID := sharedomain.NewID()
	linkedUserID, err := s.botUserCreator.CreateBotUser(ctx, agent.Name, botEmailForID(agentID), agent.AvatarURL, nil,
		agent.Status == domain.AgentStatusEnabled)
	if err != nil {
		return nil, err
	}

	agent.ID = agentID
	agent.LinkedUserID = linkedUserID
	if err := s.repo.Create(ctx, agent); err != nil {
		return nil, err
	}
	vo := toVO(agent)
	return &vo, nil
}

// GetAgent 获取机器人详情（全局链）。
func (s *agentServiceImpl) GetAgent(ctx context.Context, adminID, agentID uuid.UUID) (*AgentVO, error) {
	if err := s.ensureAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	agent, err := s.loadGlobalAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	vo := toVO(agent)
	return &vo, nil
}

// ListAgents offset 分页列表，keyword 非空时按 name 模糊过滤（仅全局机器人）。
func (s *agentServiceImpl) ListAgents(ctx context.Context, adminID uuid.UUID, keyword string, page, size int) (*AgentListResult, error) {
	if err := s.ensureAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	agents, total, err := s.repo.ListByOffset(ctx, strings.TrimSpace(keyword), (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	result := &AgentListResult{
		Total:  total,
		Page:   page,
		Size:   size,
		Agents: make([]AgentVO, 0, len(agents)),
	}
	for i := range agents {
		result.Agents = append(result.Agents, toVO(&agents[i]))
	}
	return result, nil
}

// UpdateAgent 部分字段更新（全局链）。
func (s *agentServiceImpl) UpdateAgent(ctx context.Context, adminID, agentID uuid.UUID, input UpdateAgentInput) (*AgentVO, error) {
	if err := s.ensureAdmin(ctx, adminID); err != nil {
		return nil, err
	}
	// 存在性 + 作用域检查（圈内机器人 → 404，不暴露存在性）。
	if _, err := s.loadGlobalAgent(ctx, agentID); err != nil {
		return nil, err
	}

	fields, err := buildAgentUpdateFields(ctx, s.repo, agentID, input, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errNoFieldsToUpdate
	}

	if err := s.repo.UpdateFields(ctx, agentID, fields); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		return nil, mapRepoError(err)
	}

	// 改名/换头像时同步到关联系统用户（users.username/avatar_url）。
	syncBotUserProfile(ctx, s.botUserUpdater, updated.LinkedUserID, fields)
	// 启用/停用时同步机器人账号能否被 @（按更新后实体投影）。
	if _, ok := fields["status"]; ok {
		syncBotUserMentionable(ctx, s.mentionSyncer, s.repo, updated.ID, updated.LinkedUserID,
			updated.Status == domain.AgentStatusEnabled)
	}

	vo := toVO(updated)
	return &vo, nil
}

// botProfileSyncMaxAttempts 资料同步失败后的异步重试次数（不含首次同步调用）。
const botProfileSyncMaxAttempts = 3

// syncBotUserProfile 改名/换头像后同步机器人资料到关联系统用户（users.username/avatar_url，
// 评论区展示从 users 表取）。全局/圈内更新共用。同步失败转异步重试（指数退避，有限次数）：
// ai_agent 已提交最新值，不能让瞬时失败把 users 资料长期撂在不一致状态；
// 重试耗尽后记 Error 日志，待人工回填（当前无 outbox 表，进程重启即丢）。
func syncBotUserProfile(ctx context.Context, updater BotUserProfileUpdater, linkedUserID uuid.UUID, fields map[string]interface{}) {
	if updater == nil {
		return
	}
	var username, avatar *string
	if v, ok := fields["name"].(string); ok {
		username = &v
	}
	if v, ok := fields["avatar_url"].(string); ok {
		avatar = &v
	}
	if username == nil && avatar == nil {
		return
	}
	if err := updater.UpdateBotUserProfile(ctx, linkedUserID, username, avatar); err != nil {
		logger.Log.Error("Sync bot user profile failed, scheduling async retry: " + err.Error())
		go retryBotUserProfileSync(updater, linkedUserID, username, avatar)
	}
}

// retryBotUserProfileSync 机器人资料同步失败后的异步补偿：指数退避重试，
// 带全量载荷（username/avatar 指针 + linkedUserID），任一尝试成功即收敛。
// 重试耗尽后只能依赖人工回填（当前无 outbox 表，进程退出即丢失待重试任务）。
func retryBotUserProfileSync(updater BotUserProfileUpdater, userID uuid.UUID, username, avatarURL *string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error(fmt.Sprintf("bot user profile sync retry panic: user=%s: %v", userID, r))
		}
	}()
	backoff := time.Second
	for attempt := 1; attempt <= botProfileSyncMaxAttempts; attempt++ {
		time.Sleep(backoff)
		// 脱离请求 ctx：更新接口已返回，重试不应随请求取消而中断。
		if err := updater.UpdateBotUserProfile(context.Background(), userID, username, avatarURL); err == nil {
			logger.Log.Info(fmt.Sprintf("Bot user profile sync recovered on retry %d: user=%s", attempt, userID))
			return
		} else {
			logger.Log.Warn(fmt.Sprintf("Bot user profile sync retry %d/%d failed: user=%s: %s",
				attempt, botProfileSyncMaxAttempts, userID, err.Error()))
		}
		backoff *= 4
	}
	logger.Log.Error(fmt.Sprintf("Bot user profile sync permanently failed after %d retries, manual backfill required: user=%s",
		botProfileSyncMaxAttempts, userID))
}

// DeleteAgent 软删（deleted=1 且 status=0）。软删生效后把机器人账号投影为不可 @
// （投影失败不回滚软删，fail-open + 异步重试，见 syncBotUserMentionable）。
func (s *agentServiceImpl) DeleteAgent(ctx context.Context, adminID, agentID uuid.UUID) error {
	if err := s.ensureAdmin(ctx, adminID); err != nil {
		return err
	}
	agent, err := s.loadGlobalAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, agentID); err != nil {
		return err
	}
	syncBotUserMentionable(ctx, s.mentionSyncer, s.repo, agent.ID, agent.LinkedUserID, false)
	return nil
}

// botMentionSyncMaxAttempts 可 @ 状态投影失败后的异步重试次数（不含首次同步调用）。
const botMentionSyncMaxAttempts = 3

// botMentionSyncBackoff 异步重试的首次退避（之后每次 ×4）；包级变量便于单测缩短。
var botMentionSyncBackoff = time.Second

// syncBotUserMentionable 把机器人启用状态投影到关联系统用户（users.agent_mention_disabled）的统一入口
// （全局/圈内的创建后停用、启用、删除共用）。投影与 ai_agent 更新不在同一事务：
// fail-open——端口未注入或写入失败都不让已生效的状态变更 / 软删报错回滚，失败时记日志并转异步重试。
// 重试每次按 ai_agent 当前状态重新推导（已删除=不可 @），不重放旧值，避免和随后的启用/停用互相覆盖。
func syncBotUserMentionable(ctx context.Context, syncer BotUserMentionSyncer, repo domain.AgentRepository,
	agentID, linkedUserID uuid.UUID, mentionable bool) {
	if linkedUserID == uuid.Nil {
		return
	}
	if syncer == nil {
		logger.Log.Warn("BotUserMentionSyncer not injected, skip syncing agent_mention_disabled: user=" + linkedUserID.String())
		return
	}
	if err := syncer.SetBotUserMentionable(ctx, linkedUserID, mentionable); err != nil {
		logger.Log.Error("Sync bot mentionable failed, scheduling async retry: user=" + linkedUserID.String() + ": " + err.Error())
		go retryBotUserMentionableSync(syncer, repo, agentID, linkedUserID, botMentionSyncBackoff)
	}
}

// retryBotUserMentionableSync 可 @ 状态投影失败后的异步补偿：指数退避重试，每次从 ai_agent 重新推导目标值。
// 重试耗尽后记 Error 日志，待人工按 docs/pgsql-ddl/user.md 的回填 SQL 修正（当前无 outbox 表，进程退出即丢）。
func retryBotUserMentionableSync(syncer BotUserMentionSyncer, repo domain.AgentRepository, agentID, linkedUserID uuid.UUID,
	backoff time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error(fmt.Sprintf("bot mentionable sync retry panic: user=%s: %v", linkedUserID, r))
		}
	}()
	for attempt := 1; attempt <= botMentionSyncMaxAttempts; attempt++ {
		time.Sleep(backoff)
		backoff *= 4
		// 脱离请求 ctx：接口已返回，重试不应随请求取消而中断。
		ctx := context.Background()
		agent, err := repo.GetByID(ctx, agentID)
		var mentionable bool
		switch {
		case errors.Is(err, domain.ErrAgentNotFound):
			mentionable = false // 已软删
		case err != nil:
			logger.Log.Warn(fmt.Sprintf("Bot mentionable sync retry %d/%d: load agent failed: agent=%s: %s",
				attempt, botMentionSyncMaxAttempts, agentID, err.Error()))
			continue
		default:
			mentionable = agent.Status == domain.AgentStatusEnabled
		}
		if err := syncer.SetBotUserMentionable(ctx, linkedUserID, mentionable); err != nil {
			logger.Log.Warn(fmt.Sprintf("Bot mentionable sync retry %d/%d failed: user=%s: %s",
				attempt, botMentionSyncMaxAttempts, linkedUserID, err.Error()))
			continue
		}
		logger.Log.Info(fmt.Sprintf("Bot mentionable sync recovered on retry %d: user=%s", attempt, linkedUserID))
		return
	}
	logger.Log.Error(fmt.Sprintf("Bot mentionable sync permanently failed after %d retries, manual backfill required: user=%s",
		botMentionSyncMaxAttempts, linkedUserID))
}

// loadGlobalAgent 加载**全局**机器人：不存在或为圈子级机器人（CircleID != nil）一律
// ErrAgentNotFound——跨作用域不可见，全局链路不暴露圈内机器人的存在性。
func (s *agentServiceImpl) loadGlobalAgent(ctx context.Context, agentID uuid.UUID) (*domain.AiAgent, error) {
	agent, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	if agent.CircleID != nil {
		return nil, errAgentNotFound
	}
	return agent, nil
}

// toVO 实体转视图（全局/圈内共用）。api_key 解密后仅给掩码；解密失败（如换了 data_key）
// 不让读接口失败，退化为 "****"。CircleID 仅圈内机器人回显（nil 全局不回显）。
func toVO(a *domain.AiAgent) AgentVO {
	vo := AgentVO{
		ID:                a.ID,
		Name:              a.Name,
		AvatarURL:         a.AvatarURL,
		LinkedUserID:      a.LinkedUserID,
		CircleID:          a.CircleID,
		APIProtocol:       a.APIProtocol,
		BaseURL:           a.BaseURL,
		HasAPIKey:         a.APIKeyEnc != "",
		Model:             a.Model,
		LLMParams:         map[string]interface{}(a.LLMParams),
		SystemPrompt:      a.SystemPrompt,
		FilterPrompt:      a.FilterPrompt,
		TriggerMode:       int(a.TriggerMode),
		TriggerKeywords:   []string(a.TriggerKeywords),
		MaxRepliesPerHour: a.MaxRepliesPerHour,
		MinIntervalSec:    a.MinIntervalSec,
		Status:            int(a.Status),
		CreateTime:        a.CreateTime,
		UpdateTime:        a.UpdateTime,
	}
	if a.APIKeyEnc != "" {
		if plain, err := crypto.Decrypt(conf.Config.Security.DataKey, a.APIKeyEnc); err == nil {
			vo.APIKeyMasked = crypto.Mask(plain)
		} else {
			vo.APIKeyMasked = "****"
		}
	}
	if vo.LLMParams == nil {
		vo.LLMParams = map[string]interface{}{}
	}
	if vo.TriggerKeywords == nil {
		vo.TriggerKeywords = []string{}
	}
	if ar, err := a.AutoConfig.AutoReply(); err == nil {
		vo.AutoConfig.AutoReply = ar
	} else {
		logger.Log.Warn(fmt.Sprintf("agent %s: invalid auto_config.auto_reply ignored: %s", a.ID, err.Error()))
	}
	if vo.AutoConfig.AutoReply.Targets == nil {
		vo.AutoConfig.AutoReply.Targets = []string{}
	}
	return vo
}

// mapRepoError 把 repo 错误归一为 application 哨兵。
func mapRepoError(err error) error {
	if err == nil {
		return nil
	}
	if err == domain.ErrAgentNotFound {
		return errAgentNotFound
	}
	if err == domain.ErrCircleAgentLimit {
		return errCircleAgentLimit
	}
	return err
}

// ---- 创建/更新的公共构建（全局与圈内链路共用，防两处校验规则漂移）----

// botEmailForID 生成机器人系统用户 email（users.email 唯一索引：
// uuidv7 ID + 毫秒时间戳，全局/圈内创建共用，全局唯一）。
func botEmailForID(agentID uuid.UUID) string {
	return fmt.Sprintf("%s.%d@bot.qubar.local", agentID.String(), time.Now().UnixMilli())
}

// validateAndBuildAgent 校验创建入参并构建实体公共部分（全局/圈内创建共用）。
// 全量过 validateXxx 函数组 + api_key 加密 + 默认值补齐；ID/LinkedUserID/CircleID/
// CreatorID 等归属字段由调用方按各自链路补齐。
func validateAndBuildAgent(ctx context.Context, input CreateAgentInput) (*domain.AiAgent, error) {
	name := utils.SanitizeForPg(strings.TrimSpace(input.Name))
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateProtocol(input.APIProtocol); err != nil {
		return nil, err
	}
	baseURL := utils.SanitizeForPg(strings.TrimSpace(input.BaseURL))
	if err := validateBaseURL(ctx, baseURL); err != nil {
		return nil, err
	}
	model := utils.SanitizeForPg(strings.TrimSpace(input.Model))
	if len(model) < 1 || len(model) > 100 {
		return nil, errInvalidModel
	}
	if err := validateTrigger(input.TriggerMode, input.TriggerKeywords); err != nil {
		return nil, err
	}
	if err := validateLLMParams(input.LLMParams); err != nil {
		return nil, err
	}
	if err := validateRateLimit(input.MaxRepliesPerHour, input.MinIntervalSec); err != nil {
		return nil, err
	}
	filterPrompt := utils.SanitizeForPg(strings.TrimSpace(input.FilterPrompt))
	if err := validateFilterPrompt(filterPrompt); err != nil {
		return nil, err
	}
	if input.Status != nil {
		if err := validateStatus(*input.Status); err != nil {
			return nil, err
		}
	}
	autoConfig := domain.AutoConfigJSON{}
	if input.AutoConfig != nil && input.AutoConfig.AutoReply != nil {
		raw, err := buildAutoReply(*input.AutoConfig.AutoReply)
		if err != nil {
			return nil, err
		}
		autoConfig[domain.AutoConfigKeyAutoReply] = raw
	}

	// api_key 加密（自部署免鉴权网关可免 key）
	apiKeyEnc := ""
	if input.APIKey != "" {
		enc, err := crypto.Encrypt(conf.Config.Security.DataKey, input.APIKey)
		if err != nil {
			if err == crypto.ErrEmptyKey {
				return nil, errAPIKeyNotSet
			}
			return nil, err
		}
		apiKeyEnc = enc
	}

	agent := &domain.AiAgent{
		Name:              name,
		AvatarURL:         utils.SanitizeForPg(input.AvatarURL),
		APIProtocol:       input.APIProtocol,
		BaseURL:           baseURL,
		APIKeyEnc:         apiKeyEnc,
		Model:             model,
		LLMParams:         toLLMParams(input.LLMParams),
		SystemPrompt:      utils.SanitizeForPg(input.SystemPrompt),
		FilterPrompt:      filterPrompt,
		TriggerMode:       domain.TriggerMode(input.TriggerMode),
		TriggerKeywords:   toKeywords(input.TriggerKeywords),
		MaxRepliesPerHour: input.MaxRepliesPerHour,
		MinIntervalSec:    input.MinIntervalSec,
		AutoConfig:        autoConfig,
	}
	// 触发模式关键词默认值补齐
	if input.TriggerKeywords == nil {
		agent.TriggerKeywords = domain.KeywordsJSON{}
	}
	if input.Status != nil {
		agent.Status = int16(*input.Status) // 显式传 0 即停用
	} else {
		agent.Status = domain.AgentStatusEnabled // 未传 status 时默认启用（DDL 默认 1）
	}
	if input.TriggerMode == 0 {
		agent.TriggerMode = domain.TriggerModeAllPost // DDL 默认 1
	}
	return agent, nil
}

// buildAgentUpdateFields 校验部分更新入参并组装字段 map（全局/圈内更新共用）。
// scope 为名称唯一预检的作用域：uuid.Nil=全局桶（circle_id IS NULL），否则圈内桶，
// 与 idx_ai_agent_name 的分桶口径一致。返回空 map 表示无字段要更新
// （调用方决定是否报 errNoFieldsToUpdate）。
func buildAgentUpdateFields(ctx context.Context, repo domain.AgentRepository, agentID uuid.UUID, input UpdateAgentInput, scope uuid.UUID) (map[string]interface{}, error) {
	fields := make(map[string]interface{})

	if input.Name != nil {
		name := utils.SanitizeForPg(strings.TrimSpace(*input.Name))
		if err := validateName(name); err != nil {
			return nil, err
		}
		exists, err := repo.ExistsByNameInScope(ctx, scope, name, agentID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errAgentNameExists
		}
		fields["name"] = name
	}
	if input.AvatarURL != nil {
		fields["avatar_url"] = utils.SanitizeForPg(*input.AvatarURL)
	}
	if input.APIProtocol != nil {
		if err := validateProtocol(*input.APIProtocol); err != nil {
			return nil, err
		}
		fields["api_protocol"] = *input.APIProtocol
	}
	if input.BaseURL != nil {
		baseURL := utils.SanitizeForPg(strings.TrimSpace(*input.BaseURL))
		if err := validateBaseURL(ctx, baseURL); err != nil {
			return nil, err
		}
		fields["base_url"] = baseURL
	}
	if input.APIKey != nil {
		if *input.APIKey == "" {
			fields["api_key"] = "" // 传空串 = 清除 key
		} else {
			enc, err := crypto.Encrypt(conf.Config.Security.DataKey, *input.APIKey)
			if err != nil {
				if err == crypto.ErrEmptyKey {
					return nil, errAPIKeyNotSet
				}
				return nil, err
			}
			fields["api_key"] = enc
		}
	}
	if input.Model != nil {
		model := utils.SanitizeForPg(strings.TrimSpace(*input.Model))
		if len(model) < 1 || len(model) > 100 {
			return nil, errInvalidModel
		}
		fields["model"] = model
	}
	if input.LLMParams != nil {
		if err := validateLLMParams(input.LLMParams); err != nil {
			return nil, err
		}
		v, _ := toLLMParams(input.LLMParams).Value()
		fields["llm_params"] = v
	}
	if input.SystemPrompt != nil {
		fields["system_prompt"] = utils.SanitizeForPg(*input.SystemPrompt)
	}
	if input.FilterPrompt != nil {
		filterPrompt := utils.SanitizeForPg(strings.TrimSpace(*input.FilterPrompt))
		if err := validateFilterPrompt(filterPrompt); err != nil {
			return nil, err
		}
		fields["filter_prompt"] = filterPrompt
	}
	if input.TriggerMode != nil {
		if err := validateTrigger(*input.TriggerMode, input.TriggerKeywords); err != nil {
			return nil, err
		}
		fields["trigger_mode"] = *input.TriggerMode
	}
	if input.TriggerKeywords != nil {
		if err := validateTriggerKeywordValues(input.TriggerKeywords); err != nil {
			return nil, err
		}
		v, _ := toKeywords(input.TriggerKeywords).Value()
		fields["trigger_keywords"] = v
	}
	if input.MaxRepliesPerHour != nil {
		if err := validateRateLimit(*input.MaxRepliesPerHour, 0); err != nil {
			return nil, err
		}
		fields["max_replies_per_hour"] = *input.MaxRepliesPerHour
	}
	if input.MinIntervalSec != nil {
		if err := validateRateLimit(0, *input.MinIntervalSec); err != nil {
			return nil, err
		}
		fields["min_interval_sec"] = *input.MinIntervalSec
	}
	if input.Status != nil {
		if err := validateStatus(*input.Status); err != nil {
			return nil, err
		}
		fields["status"] = *input.Status
	}
	if input.AutoConfig != nil && input.AutoConfig.AutoReply != nil {
		raw, err := buildAutoReply(*input.AutoConfig.AutoReply)
		if err != nil {
			return nil, err
		}
		// 顶层 key 合并补丁：只替换 auto_reply，其他自主行为子对象保持不变。
		fields["auto_config"] = domain.AutoConfigPatch{domain.AutoConfigKeyAutoReply: raw}
	}
	return fields, nil
}

// ---- 校验 ----

func validateName(name string) error {
	if len(name) < 1 || len(name) > 50 {
		return errInvalidName
	}
	return nil
}

// validateProtocol 本期只放行 infra 已实现的协议（gemini/ollama 为 P2 决策待办，
// 见 docs/agent-reply-design.md；实现前不允许入库，避免运行时必失败）。
func validateProtocol(p string) error {
	switch p {
	case domain.ProtocolOpenAI, domain.ProtocolAnthropic:
		return nil
	}
	return errInvalidProtocol
}

// baseURLResolver 保存时校验 base_url 用的 DNS 解析器（nil = net.DefaultResolver；测试可替换）。
var baseURLResolver netguard.Resolver

// baseURLResolveTimeout 保存时 DNS 校验超时。
const baseURLResolveTimeout = 3 * time.Second

// validateBaseURL 校验 base_url（#48 防 SSRF）。空串 = 用协议官方默认地址，放行。
// 保存时：https、无 userinfo、非内网字面量 IP、域名解析结果全部为公网地址。
// 这只是提前反馈；真正的强制点在 LLM 调用的拨号阶段（infrastructure.llmEino → netguard.NewHTTPClient）。
// aiagent.allow_private_base_url=true 时放行 http 与内网地址（本地开发 / 内网自建网关）。
func validateBaseURL(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > 500 {
		return errInvalidBaseURL
	}
	allowPrivate := conf.Config.AiAgent.AllowPrivateBaseURL
	u, err := netguard.CheckURL(raw, allowPrivate)
	if err != nil {
		return errInvalidBaseURL
	}
	lookupCtx, cancel := context.WithTimeout(ctx, baseURLResolveTimeout)
	defer cancel()
	if err := netguard.CheckHostResolves(lookupCtx, baseURLResolver, u.Hostname(), allowPrivate); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // 请求已取消（客户端断开），不是 base_url 的问题
		}
		if errors.Is(err, netguard.ErrUnresolvable) {
			return errBaseURLNoDNS
		}
		return errInvalidBaseURL
	}
	return nil
}

// validateTrigger 校验触发模式；mode=2 时 keywords 必须非空。
func validateTrigger(mode int, keywords []string) error {
	m := domain.TriggerMode(mode)
	if m == 0 {
		m = domain.TriggerModeAllPost // 未传时取默认
	}
	if !m.Valid() {
		return errInvalidTrigger
	}
	if m == domain.TriggerModeKeyword && len(keywords) == 0 {
		return errInvalidTrigger
	}
	if keywords != nil {
		return validateTriggerKeywordValues(keywords)
	}
	return nil
}

func validateTriggerKeywordValues(keywords []string) error {
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			return errInvalidTrigger
		}
	}
	return nil
}

// validateLLMParams 只允许白名单键 + 数值型值（范围不做硬约束，交给供应商报错）。
func validateLLMParams(params map[string]interface{}) error {
	for k, v := range params {
		if !llmParamsKeyWhitelist[k] {
			return errInvalidLLMParams
		}
		switch v.(type) {
		case float64, int, int64:
		default:
			return errInvalidLLMParams
		}
	}
	return nil
}

func validateRateLimit(perHour, minInterval int) error {
	if perHour < 0 || minInterval < 0 {
		return errInvalidRateLimit
	}
	return nil
}

// validateFilterPrompt 判定条件长度上限（字符数）。
func validateFilterPrompt(s string) error {
	if len([]rune(s)) > filterPromptMaxLen {
		return errInvalidFilter
	}
	return nil
}

// 自动回复配置取值范围（每个机器人的配置；平台级上限见 conf aiagent.autonomy）。
const (
	autoReplyMaxWindowHours = 168     // 时间窗口最长 7 天
	autoReplyMaxPerHour     = 1000    // 每小时上限的合法范围上界（防误填）
	autoReplyMaxThreshold   = 1 << 30 // 门槛上界（防溢出 / 误填）
)

// buildAutoReply 校验自动回复配置并序列化为 auto_config.auto_reply 子对象。
//
// 规则：targets 只能是 post / comment 且不重复；门槛、窗口、上限均非负且有上界；
// 启用时至少一个目标、window_hours 必须 >= 1，且每个启用的目标都要有生效门槛
// （帖子 post_hot_min >= 1；评论 comment_like_min / comment_reply_min 至少一个 >= 1）——
// 门槛全 0 等于"窗口内全部内容"，属于 trigger_mode=1 的语义，不在自动回复范围内。
func buildAutoReply(cfg domain.AutoReplyConfig) (json.RawMessage, error) {
	seen := make(map[string]bool, len(cfg.Targets))
	targets := make([]string, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		t = strings.TrimSpace(t)
		if (t != domain.AutoReplyTargetPost && t != domain.AutoReplyTargetComment) || seen[t] {
			return nil, errInvalidAutoConfig
		}
		seen[t] = true
		targets = append(targets, t)
	}
	cfg.Targets = targets
	for _, v := range []int{cfg.PostHotMin, cfg.CommentLikeMin, cfg.CommentReplyMin} {
		if v < 0 || v > autoReplyMaxThreshold {
			return nil, errInvalidAutoConfig
		}
	}
	if cfg.WindowHours < 0 || cfg.WindowHours > autoReplyMaxWindowHours ||
		cfg.MaxPerHour < 0 || cfg.MaxPerHour > autoReplyMaxPerHour {
		return nil, errInvalidAutoConfig
	}
	if cfg.Enabled {
		if len(targets) == 0 || cfg.WindowHours < 1 {
			return nil, errInvalidAutoConfig
		}
		if seen[domain.AutoReplyTargetPost] && cfg.PostHotMin < 1 {
			return nil, errInvalidAutoConfig
		}
		if seen[domain.AutoReplyTargetComment] && cfg.CommentLikeMin < 1 && cfg.CommentReplyMin < 1 {
			return nil, errInvalidAutoConfig
		}
	}
	return json.Marshal(cfg)
}

func validateStatus(status int) error {
	if status != domain.AgentStatusDisabled && status != domain.AgentStatusEnabled {
		return errInvalidStatus
	}
	return nil
}

// ---- 类型转换 ----

func toLLMParams(m map[string]interface{}) domain.LLMParamsJSON {
	if m == nil {
		return domain.LLMParamsJSON{}
	}
	return domain.LLMParamsJSON(m)
}

func toKeywords(k []string) domain.KeywordsJSON {
	if k == nil {
		return domain.KeywordsJSON{}
	}
	return domain.KeywordsJSON(k)
}
