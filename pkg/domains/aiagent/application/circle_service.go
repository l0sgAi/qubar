// circle_service.go 圈子级 AI 机器人管理用例（CircleAgentService）。
//
// 圈主（role=30）/管理员（role=20）管理**本圈**的 AI 机器人（每圈上限 MaxAgentsPerCircle），
// 权限模型与 circle 管理域同构：admin+ 可列表/详情/创建/更新运营字段；
// 凭据字段（api_protocol/base_url/api_key，计费凭据由圈主持有）与删除仅圈主。
// 平台超管（users.role=1）不特殊处理——其控制台是 /agent/*（全局机器人）。
// 圈内机器人回复触发已按圈落地（docs/circle-agent-reply-design.md）：本圈帖
// 关键词/@提及/圈主手动触发，限流缺省防圈主 key 裸奔。
// 权限每次直查 member 记录（无缓存）：任免/转让/禁言即时生效；圈子被封禁不阻断管理
//（owner 需要能停用/删除机器人止血），权限只看 member 记录。
// 设计见 docs/circle-agent-manage-design.md。
package application

import (
	"context"
	"errors"
	"strings"

	"interestBar/pkg/domains/aiagent/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// 圈内角色/状态常量（circle_member.role/status，权威定义在 circle 领域；
// 跨域不 import 兄弟域包，裸值对齐 circle.domain.MemberRoleAdmin/Owner、MemberStatusNormal）。
const (
	circleRoleAdmin    = 20
	circleRoleOwner    = 30
	memberStatusNormal = 1
)

// 圈内机器人限流缺省值（决策项 B，docs/circle-agent-reply-design.md §2.6）：
// 创建入参未传（<=0）时补默认，防止圈主持有的计费 key 裸奔不限速；
// 数值与 DDL 列默认一致。代价：圈内创建无法直接表达"不限"——需创建后经
// 更新接口显式设 0（更新链路原样透传，语义不变）。全局链路不做此兜底。
const (
	circleBotDefaultMaxRepliesPerHour = 30
	circleBotDefaultMinIntervalSec    = 60
)

// CircleRoleReader 跨域端口：读取用户在圈内的角色/成员状态（composition 桥接 circle 域）。
//
// 独立声明而非 import circle.application，避免跨领域包依赖。
type CircleRoleReader interface {
	// GetCircleMembership 返回用户在圈内的 (role, status)；非成员/圈子不存在 ok=false。
	// 实现方直查 member 记录（含惰性解禁自愈），不缓存。
	GetCircleMembership(ctx context.Context, circleID, userID uuid.UUID) (role, status int16, ok bool, err error)
}

// CircleAgentService 圈子级机器人管理服务接口。
//
// 所有方法第一个业务参数为操作者 userID，service 内统一做圈内角色校验
//（fail-closed：CircleRoleReader 未注入一律拒绝），handler 无需（也不应）自行判断，
// 防止新增入口漏检。
type CircleAgentService interface {
	// CreateCircleAgent 创建圈内机器人（admin+，每圈 ≤MaxAgentsPerCircle，超限 409）。
	CreateCircleAgent(ctx context.Context, operatorID, circleID uuid.UUID, input CreateAgentInput) (*AgentVO, error)
	// GetCircleAgent 圈内机器人详情（该圈 admin+）。
	GetCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID) (*AgentVO, error)
	// ListCircleAgents 圈内机器人列表（admin+，offset 分页，keyword 非空按 name 模糊过滤）。
	ListCircleAgents(ctx context.Context, operatorID, circleID uuid.UUID, keyword string, page, size int) (*AgentListResult, error)
	// UpdateCircleAgent 部分字段更新（字段分级：凭据字段 api_protocol/base_url/api_key 仅圈主）。
	UpdateCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID, input UpdateAgentInput) (*AgentVO, error)
	// DeleteCircleAgent 软删（仅圈主；deleted=1 且停用）。
	DeleteCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID) error

	// SetCircleRoleReader 注入跨域圈内角色读取端口（composition 桥接）。
	SetCircleRoleReader(r CircleRoleReader)
	// SetBotUserCreator 注入跨域机器人账号创建端口（复用全局链路端口）。
	SetBotUserCreator(c BotUserCreator)
	// SetBotUserProfileUpdater 注入跨域机器人资料同步端口（复用全局链路端口）。
	SetBotUserProfileUpdater(u BotUserProfileUpdater)
	// SetBotUserScopeCleaner 注入跨域机器人圈子绑定清理端口（复用全局链路端口；
	// 未注入时删除机器人跳过清列，仅记日志，见 clearBotCircleScope fail-open）。
	SetBotUserScopeCleaner(c BotUserScopeCleaner)
}

type circleAgentServiceImpl struct {
	repo           domain.AgentRepository
	circleRoles    CircleRoleReader      // 注入前 nil，鉴权 fail-closed
	botUserCreator BotUserCreator        // 注入前 nil，创建时 fail-fast
	botUserUpdater BotUserProfileUpdater // 注入前 nil，改名/换头像时跳过同步
	scopeCleaner   BotUserScopeCleaner   // 注入前 nil，删除时跳过清列（fail-open）
}

// NewCircleAgentService 构造 CircleAgentService（跨域依赖 setter 注入）。
func NewCircleAgentService(repo domain.AgentRepository) CircleAgentService {
	return &circleAgentServiceImpl{repo: repo}
}

func (s *circleAgentServiceImpl) SetCircleRoleReader(r CircleRoleReader) { s.circleRoles = r }
func (s *circleAgentServiceImpl) SetBotUserCreator(c BotUserCreator)     { s.botUserCreator = c }
func (s *circleAgentServiceImpl) SetBotUserProfileUpdater(u BotUserProfileUpdater) {
	s.botUserUpdater = u
}
func (s *circleAgentServiceImpl) SetBotUserScopeCleaner(c BotUserScopeCleaner) { s.scopeCleaner = c }

// requireCircleManager 校验操作者是该圈 admin+（role>=20）且成员状态正常。
// 端口未注入/非成员/圈子不存在/状态非 normal（禁言/拉黑/待审/退出）一律拒绝
//（fail-closed；非成员不暴露"你不是成员"细节，防成员身份探测）。
func (s *circleAgentServiceImpl) requireCircleManager(ctx context.Context, circleID, operatorID uuid.UUID) error {
	if s.circleRoles == nil {
		return errNotCircleAdmin
	}
	role, status, ok, err := s.circleRoles.GetCircleMembership(ctx, circleID, operatorID)
	if err != nil || !ok || role < circleRoleAdmin || status != memberStatusNormal {
		return errNotCircleAdmin
	}
	return nil
}

// requireCircleOwner 校验操作者是该圈圈主（role=30）且成员状态正常。
func (s *circleAgentServiceImpl) requireCircleOwner(ctx context.Context, circleID, operatorID uuid.UUID) error {
	if s.circleRoles == nil {
		return errNotCircleOwner
	}
	role, status, ok, err := s.circleRoles.GetCircleMembership(ctx, circleID, operatorID)
	if err != nil || !ok || role != circleRoleOwner || status != memberStatusNormal {
		return errNotCircleOwner
	}
	return nil
}

// loadCircleAgent 加载**圈内**机器人：不存在或为平台全局机器人（CircleID == nil）一律
// ErrAgentNotFound（404）——跨作用域不可见，圈内链路不暴露全局机器人的存在性。
// 读取先于权限校验：不存在/越属访问恒 404，不泄露归属。
func (s *circleAgentServiceImpl) loadCircleAgent(ctx context.Context, agentID uuid.UUID) (*domain.AiAgent, error) {
	agent, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		if errors.Is(err, domain.ErrAgentNotFound) {
			return nil, errAgentNotFound
		}
		return nil, err
	}
	if agent.CircleID == nil {
		return nil, errAgentNotFound
	}
	return agent, nil
}

// CreateCircleAgent 创建圈内机器人。
func (s *circleAgentServiceImpl) CreateCircleAgent(ctx context.Context, operatorID, circleID uuid.UUID, input CreateAgentInput) (*AgentVO, error) {
	if err := s.requireCircleManager(ctx, circleID, operatorID); err != nil {
		return nil, err
	}

	// 复用全局链路的校验函数组 + api_key 加密 + 默认值补齐（语义一致，防规则漂移）。
	agent, err := validateAndBuildAgent(ctx, input)
	if err != nil {
		return nil, err
	}
	// 圈内限流缺省（决策项 B）：计费 key 由圈主持有，创建未传即兜底防裸奔；
	// 全局链路语义不变（0=不限）。
	if agent.MaxRepliesPerHour <= 0 {
		agent.MaxRepliesPerHour = circleBotDefaultMaxRepliesPerHour
	}
	if agent.MinIntervalSec <= 0 {
		agent.MinIntervalSec = circleBotDefaultMinIntervalSec
	}

	// 名称唯一预检（圈内桶；并发兜底靠 (circle 桶, name) 部分唯一索引）。
	exists, err := s.repo.ExistsByNameInScope(ctx, circleID, agent.Name, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errAgentNameExists
	}

	// 建机器人系统用户（role=2，email uuid 派生全局唯一），与全局链路同构。
	if s.botUserCreator == nil {
		return nil, errNotCircleAdmin // 端口未注入视为装配错误，拒绝创建
	}
	agentID := sharedomain.NewID()
	botEmail := botEmailForID(agentID)
	// 圈内机器人：users.agent_circle_id 写入圈子ID（ai_agent.circle_id 的投影，
	// CDC 同步 ES 后供 @选人 作用域过滤）。与 CreateInCircle 非同一事务（跨库行
	// 本就独立写，孤儿行语义与全局链路一致）。
	linkedUserID, err := s.botUserCreator.CreateBotUser(ctx, agent.Name, botEmail, agent.AvatarURL, &circleID)
	if err != nil {
		return nil, err
	}

	agent.ID = agentID
	agent.LinkedUserID = linkedUserID
	agent.CircleID = &circleID // 创建后不可变（不提供全局↔圈内迁移）
	agent.CreatorID = operatorID

	// 行锁 + 计数的事务化创建：并发同圈创建只过一个（超限 ErrCircleAgentLimit → 409）。
	if err := s.repo.CreateInCircle(ctx, agent, domain.MaxAgentsPerCircle); err != nil {
		return nil, mapRepoError(err)
	}
	vo := toVO(agent)
	return &vo, nil
}

// GetCircleAgent 圈内机器人详情。
func (s *circleAgentServiceImpl) GetCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID) (*AgentVO, error) {
	agent, err := s.loadCircleAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if err := s.requireCircleManager(ctx, *agent.CircleID, operatorID); err != nil {
		return nil, err
	}
	vo := toVO(agent)
	return &vo, nil
}

// ListCircleAgents 圈内机器人列表（page/size 规整同 ListAgents）。
func (s *circleAgentServiceImpl) ListCircleAgents(ctx context.Context, operatorID, circleID uuid.UUID, keyword string, page, size int) (*AgentListResult, error) {
	if err := s.requireCircleManager(ctx, circleID, operatorID); err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	agents, total, err := s.repo.ListByCircle(ctx, circleID, strings.TrimSpace(keyword), (page-1)*size, size)
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

// UpdateCircleAgent 部分字段更新（字段分级：input 携带任一凭据字段 → 仅圈主，否则 admin+）。
func (s *circleAgentServiceImpl) UpdateCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID, input UpdateAgentInput) (*AgentVO, error) {
	// 先 404 后 403：不存在的/全局的机器人不泄露归属。
	agent, err := s.loadCircleAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if input.APIProtocol != nil || input.BaseURL != nil || input.APIKey != nil {
		// 凭据字段（计费凭据由圈主持有）：对齐 UpdateCircleProfile「身份字段仅圈主」先例。
		if err := s.requireCircleOwner(ctx, *agent.CircleID, operatorID); err != nil {
			return nil, err
		}
	} else if err := s.requireCircleManager(ctx, *agent.CircleID, operatorID); err != nil {
		return nil, err
	}

	fields, err := buildAgentUpdateFields(ctx, s.repo, agentID, input, *agent.CircleID)
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

	// 改名/换头像同步机器人系统用户（含异步重试补偿），与全局链路同构。
	syncBotUserProfile(ctx, s.botUserUpdater, updated.LinkedUserID, fields)

	vo := toVO(updated)
	return &vo, nil
}

// DeleteCircleAgent 软删（仅圈主：破坏性操作，对齐转让/任免的 owner-only 惯例）。
// 软删生效后清机器人账号的圈子绑定（fail-open，语义同全局链 DeleteAgent）。
func (s *circleAgentServiceImpl) DeleteCircleAgent(ctx context.Context, operatorID, agentID uuid.UUID) error {
	agent, err := s.loadCircleAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if err := s.requireCircleOwner(ctx, *agent.CircleID, operatorID); err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, agentID); err != nil {
		return err
	}
	clearBotCircleScope(ctx, s.scopeCleaner, agent.LinkedUserID)
	return nil
}
