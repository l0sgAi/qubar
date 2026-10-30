package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/util/crypto"

	"github.com/google/uuid"
)

// ===== 跨域端口（composition 桥接注入，不 import 兄弟域包）=====

// PostBrief 供机器人回复用的帖子摘要（post 域桥接提供）。
type PostBrief struct {
	ID       uuid.UUID
	Title    string
	Summary  string
	Status   int16 // 1=已发布（post.PostStatusPublished，避免跨域 import 用裸值）
	IsLock   int16
	AuthorID uuid.UUID
	// CircleID 帖子所属圈子（圈子级机器人回复触发的作用域匹配依据；
	// 设计见 docs/circle-agent-reply-design.md）。
	CircleID uuid.UUID
}

// PostReader 帖子读取端口（桥接 post 域）。未找到返回 nil, nil。
type PostReader interface {
	GetPostBrief(ctx context.Context, postID uuid.UUID) (*PostBrief, error)
}

// CommentCreateInput 机器人发评论入参（同构 comment.CreateCommentInput 的子集）。
type CommentCreateInput struct {
	PostID    uuid.UUID
	Content   string
	RootID    *uuid.UUID // 关键词触发时挂进触发评论楼层；手动触发为 nil（顶层评论）
	ReplyToID *uuid.UUID
}

// CommentCreator 评论创建端口（桥接 comment 域 CreateComment）。
type CommentCreator interface {
	CreateComment(ctx context.Context, userID uuid.UUID, input CommentCreateInput) (uuid.UUID, error)
}

// LLMRequest LLM 调用请求（application 层完成 api_key 解密，infra 层只管协议适配）。
type LLMRequest struct {
	Protocol     string // openai/anthropic（domain 白名单）
	BaseURL      string
	APIKey       string // 明文
	Model        string
	Params       map[string]interface{} // agent.LLMParamsJSON 原样
	SystemPrompt string
	UserPrompt   string
}

// LLMResult LLM 调用结果。
type LLMResult struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

// LLMCaller LLM 调用端口（infrastructure 用 eino 实现）。
type LLMCaller interface {
	Generate(ctx context.Context, req LLMRequest) (*LLMResult, error)
}

// CommentEvent 评论创建事件（comment 域触发钩子回调载荷）。
type CommentEvent struct {
	CommentID uuid.UUID
	PostID    uuid.UUID
	// PostCircleID 帖子所属圈子（circle-agent-reply 作用域匹配：
	// Nil=未知/全局场景，仅全局机器人触发，fail-closed）。
	PostCircleID uuid.UUID
	UserID       uuid.UUID
	RootID       *uuid.UUID
	Content      string
}

// PostMentionEvent 发帖 @提及 事件（post 域触发钩子回调载荷）。
type PostMentionEvent struct {
	PostID uuid.UUID
	// PostCircleID 帖子所属圈子（圈子级机器人只在同圈帖触发，Nil 仅全局机器人）。
	PostCircleID uuid.UUID
	UserID       uuid.UUID // 发帖人
	// MentionUserIDs 为 post 域已校验落库的最终 @ 名单（存在性/去自/越圈剔除已过）。
	MentionUserIDs []uuid.UUID
}

// ===== Service 接口 =====

// ReplyService 机器人回复执行服务（设计见 docs/agent-reply-design.md、
// docs/circle-agent-reply-design.md）。
type ReplyService interface {
	// OnCommentCreated 评论创建后的关键词触发入口。
	// 同步调用、立即返回（内部异步执行）；任何失败静默（仅日志表 + zap），
	// 绝不向评论创建链路返回错误。机器人自己的评论不触发（防回环）。
	// 候选集按 evt.PostCircleID 圈子作用域收口：全局机器人 + 该圈机器人，
	// 他圈机器人不触发（Nil 仅全局，fail-closed）。
	OnCommentCreated(evt CommentEvent)
	// OnPostMentioned 发帖 @机器人 触发入口。
	// 同步调用、立即返回（内部异步执行）；任何失败静默（仅日志表 + zap），
	// 绝不向发帖链路返回错误。被 @ 的启用机器人即触发：显式 @ 视为直接点名，
	// 不校验 trigger_mode/关键词（mode 仅约束关键词/手动入口的被动触发）。
	// 圈子作用域同 OnCommentCreated（scope 匹配与 mention 兜底剔除构成双层防线）。
	OnPostMentioned(evt PostMentionEvent)
	// ManualReply 管理员手动触发回复（同步，仅 trigger_mode=3 的启用**全局**机器人；
	// 圈内机器人返回 errAgentNotFound——跨作用域不可见，404 不泄露存在性）。
	// 返回生成的评论 ID；失败返回错误（同时已写失败日志行）。
	ManualReply(ctx context.Context, adminID, agentID, postID uuid.UUID) (uuid.UUID, error)
	// CircleManualReply 圈内手动触发回复（同步，仅该圈圈主；trigger_mode=3 的
	// 启用圈内机器人，且帖子必须属于机器人所在圈，跨圈 404）。
	// 返回生成的评论 ID；失败返回错误（同时已写失败日志行）。
	CircleManualReply(ctx context.Context, operatorID, agentID, postID uuid.UUID) (uuid.UUID, error)
	// RunAutoReplyScan 执行一轮自动回复扫描（定时扫描器调用，同步执行完本轮才返回）：
	// 平台总开关关闭直接返回；逐个加载开启 auto_reply 的启用机器人，按门槛取候选并回复。
	// 任何失败静默（日志表 + zap），ctx 取消时尽快返回。设计见 docs/agent-auto-reply-design.md。
	RunAutoReplyScan(ctx context.Context)

	// SetRoleReader 注入 user Facade（管理员校验用）。
	SetRoleReader(r RoleReader)
	// SetCircleRoleReader 注入圈内角色读取端口（圈内手动触发鉴权用，composition 桥接；
	// 未注入 fail-closed 一律拒绝）。
	SetCircleRoleReader(r CircleRoleReader)
	// SetPostReader 注入帖子读取端口。
	SetPostReader(r PostReader)
	// SetCommentCreator 注入评论创建端口。
	SetCommentCreator(c CommentCreator)
	// SetCandidateReader 注入自动回复候选读取端口（未注入则扫描不执行）。
	SetCandidateReader(r AutoReplyCandidateReader)
}

// replyService 配置兜底默认值（conf <=0 时使用）。
const (
	defaultReplyTimeoutSec  = 30
	defaultMaxContentChars  = 4000
	defaultReplyConcurrency = 3
	defaultSystemPrompt     = "你是兴趣社区的智能助手，回复需友好、简洁、有帮助。"
	errMsgMaxLen            = 2048
)

// 分类器（阶段1）常量：judgeSystemPrompt 强约束 JSON 输出；
// judgeMaxTokens 压低判定成本（判定输出只需几十个 token）。
const (
	judgeSystemPrompt = "你是内容分类器。根据用户给出的判定条件，判断机器人是否应回复该帖子。" +
		"只输出 JSON：{\"reply\":true|false,\"reason\":\"一句话原因\"}，禁止输出任何其他内容。"
	// judgeMaxTokens 分类器输出预算。需覆盖推理模型的思考 token（reasoning 计入
	// max_completion_tokens）与较长 reason，64 曾被烧光导致空输出/半截 JSON。
	judgeMaxTokens = 2048
)

// judgeReplyRe 半截 JSON 兜底：unmarshal 失败时抢救 reply 字段（推理模型截断高发）。
var judgeReplyRe = regexp.MustCompile(`"reply"\s*:\s*(true|false)`)

// judgeResult 分类器判定结果（阶段1 JSON 输出）。
type judgeResult struct {
	Reply  bool   `json:"reply"`
	Reason string `json:"reason"`
}

type replyServiceImpl struct {
	agentRepo      domain.AgentRepository
	replyLogRepo   domain.ReplyLogRepository
	llm            LLMCaller
	roleReader     RoleReader
	circleRoles    CircleRoleReader // 注入前 nil，圈内手动触发鉴权 fail-closed
	postReader     PostReader
	commentCreator CommentCreator
	candidates     AutoReplyCandidateReader // 注入前 nil，自动回复扫描不执行
	sem            chan struct{}            // 回复执行并发上限（关键词/@提及/自动回复共享）
	autoRound      atomic.Uint64            // 自动回复扫描轮次（奇偶轮交替帖子/评论优先，防一方饿死）
}

// NewReplyService 构造 ReplyService。
//
// agentRepo/replyLogRepo/llm 为同域依赖（构造注入）；
// roleReader/postReader/commentCreator 为跨域依赖（setter 注入，composition 桥接）。
func NewReplyService(
	agentRepo domain.AgentRepository,
	replyLogRepo domain.ReplyLogRepository,
	llm LLMCaller,
) ReplyService {
	c := conf.Config.AiAgent.ReplyConcurrency
	if c <= 0 {
		c = defaultReplyConcurrency
	}
	return &replyServiceImpl{
		agentRepo:    agentRepo,
		replyLogRepo: replyLogRepo,
		llm:          llm,
		sem:          make(chan struct{}, c),
	}
}

func (s *replyServiceImpl) SetRoleReader(r RoleReader)             { s.roleReader = r }
func (s *replyServiceImpl) SetCircleRoleReader(r CircleRoleReader) { s.circleRoles = r }
func (s *replyServiceImpl) SetPostReader(r PostReader)             { s.postReader = r }
func (s *replyServiceImpl) SetCommentCreator(c CommentCreator)     { s.commentCreator = c }
func (s *replyServiceImpl) SetCandidateReader(r AutoReplyCandidateReader) {
	s.candidates = r
}

// agentInScope 机器人回复的作用域匹配（三条入口共用，唯一核心规则）：
//   - 全局机器人（CircleID=nil）全站回复（现状语义，含圈内帖）；
//   - 圈内机器人仅在帖子属于同一圈时触发；
//   - postCircleID=Nil（理论不可达，防御性）时仅全局机器人触发——fail-closed。
//
// 候选集已由 repo ListEnabledForCircle 按圈收口，此函数是触发链内的第二道防线
// （防候选集语义漂移；手动触发路径无候选集，直接用它做归属校验）。
func agentInScope(agent *domain.AiAgent, postCircleID uuid.UUID) bool {
	if agent.CircleID == nil {
		return true
	}
	return postCircleID != uuid.Nil && *agent.CircleID == postCircleID
}

// replyTimeoutSec 单阶段 LLM 调用超时（配置兜底）。分类器与生成各自持有一份
// 该额度的子预算，互不挤占；关键词触发的整链兜底上限为 2*replyTimeout。
func replyTimeout() time.Duration {
	sec := conf.Config.AiAgent.TimeoutSec
	if sec <= 0 {
		sec = defaultReplyTimeoutSec
	}
	return time.Duration(sec) * time.Second
}

// maxContentChars prompt 各段截断长度（配置兜底）。
func maxContentChars() int {
	n := conf.Config.AiAgent.MaxContentChars
	if n <= 0 {
		n = defaultMaxContentChars
	}
	return n
}

// OnCommentCreated 评论关键词触发（异步、静默）。
func (s *replyServiceImpl) OnCommentCreated(evt CommentEvent) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Log.Error(fmt.Sprintf("agent reply panic: %v", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), replyTimeout())
		defer cancel()

		// 防回环：机器人评论不再触发（ExistsByLinkedUserID 走 idx_ai_agent_linked_user）。
		isBot, err := s.agentRepo.ExistsByLinkedUserID(ctx, evt.UserID)
		if err != nil {
			logger.Log.Error("agent reply: check linked user failed: " + err.Error())
			return
		}
		if isBot {
			return
		}

		agents, err := s.agentRepo.ListEnabledForCircle(ctx, evt.PostCircleID)
		if err != nil {
			logger.Log.Error("agent reply: list enabled agents failed: " + err.Error())
			return
		}
		for i := range agents {
			agent := &agents[i]
			// 第二道防线：候选集已按圈收口，这里再挡一次（防候选集语义漂移）。
			if !agentInScope(agent, evt.PostCircleID) {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=out_of_scope", agent.ID, evt.PostID))
				continue
			}
			if domain.TriggerMode(agent.TriggerMode) != domain.TriggerModeKeyword {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=mode_not_keyword", agent.ID, evt.PostID))
				continue
			}
			if !matchKeyword(evt.Content, agent.TriggerKeywords) {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=keyword_miss", agent.ID, evt.PostID))
				continue
			}
			// 并发上限满则跳过本轮（尽力而为，不阻塞评论链路）。
			select {
			case s.sem <- struct{}{}:
			default:
				logger.Log.Warn(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=concurrency_full", agent.ID, evt.PostID))
				continue
			}
			// 每个命中 agent 独立 goroutine + 独立超时：sem 真正限制并发，
			// 单个慢 LLM 调用不再挤占其它机器人的时间片。
			// 整体预算 2*replyTimeout：分类器（阶段1）与生成（阶段2）各自持有
			// replyTimeout 子预算（见 runClassifier 与生成调用处的 WithTimeout），
			// 分类器耗时不再吃掉生成预算，超时降级后仍有完整额度产出直接回复。
			// defer 释放信号量：executeReply panic（本 goroutine recover 兜底）
			// 也必须归还槽位，否则并发上限被永久占用。
			go func(agent *domain.AiAgent) {
				defer func() { <-s.sem }()
				defer func() {
					if r := recover(); r != nil {
						logger.Log.Error(fmt.Sprintf("agent reply panic: agent=%s post=%s: %v", agent.ID, evt.PostID, r))
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 2*replyTimeout())
				defer cancel()
				_, err := s.executeReply(ctx, agent, evt.PostID, &replyRun{source: domain.TriggerSourceKeyword, trigger: &evt})
				if err != nil {
					// 前置跳过（帖子不可回/限频）已在 executeReply 内打 Info，不重复；
					// 调用链路失败已写日志行，这里补一条 Error 便于告警。
					if errors.Is(err, errPostNotReplyable) || errors.Is(err, errRateLimited) || errors.Is(err, errSkippedByFilter) {
						return
					}
					logger.Log.Error(fmt.Sprintf("agent reply: agent=%s post=%s: %s",
						agent.ID, evt.PostID, err.Error()))
				}
			}(agent)
		}
	}()
}

// OnPostMentioned 发帖 @提及 触发（异步、静默）。
//
// 与评论关键词触发的差异：
//   - 候选集按「被 @ 的 linked_user_id」精确过滤，命中才进入执行；
//   - 不校验 trigger_mode/关键词：显式 @ 即点名，任何启用机器人都应回应；
//   - trigger=nil 走 executeReply 手动触发同构路径：顶层评论、title+summary
//     组 prompt、绕过分类器（filter_prompt 仅关键词触发生效）。
//
// 防回环：机器人当前不发帖；发帖人即机器人关联账号时兜底跳过。
func (s *replyServiceImpl) OnPostMentioned(evt PostMentionEvent) {
	if len(evt.MentionUserIDs) == 0 {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Log.Error(fmt.Sprintf("agent reply panic: %v", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), replyTimeout())
		defer cancel()

		agents, err := s.agentRepo.ListEnabledForCircle(ctx, evt.PostCircleID)
		if err != nil {
			logger.Log.Error("agent reply: list enabled agents failed: " + err.Error())
			return
		}
		mentioned := make(map[uuid.UUID]struct{}, len(evt.MentionUserIDs))
		for _, id := range evt.MentionUserIDs {
			mentioned[id] = struct{}{}
		}
		for i := range agents {
			agent := &agents[i]
			// 第二道防线：上期 mention 兜底已在名单层剔除越圈机器人，这里在
			// 触发层再挡一次（防构造请求、防投影列漂移）。
			if !agentInScope(agent, evt.PostCircleID) {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=out_of_scope", agent.ID, evt.PostID))
				continue
			}
			if _, ok := mentioned[agent.LinkedUserID]; !ok {
				continue
			}
			if agent.LinkedUserID == evt.UserID {
				continue
			}
			// 并发上限满则跳过本轮（尽力而为，不阻塞发帖链路）。
			select {
			case s.sem <- struct{}{}:
			default:
				logger.Log.Warn(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=concurrency_full", agent.ID, evt.PostID))
				continue
			}
			// 与关键词触发同一并发模型：每命中 agent 独立 goroutine + 独立
			// 2*replyTimeout 预算；defer 释放信号量，panic 也必须归还槽位。
			go func(agent *domain.AiAgent) {
				defer func() { <-s.sem }()
				defer func() {
					if r := recover(); r != nil {
						logger.Log.Error(fmt.Sprintf("agent reply panic: agent=%s post=%s: %v", agent.ID, evt.PostID, r))
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 2*replyTimeout())
				defer cancel()
				_, err := s.executeReply(ctx, agent, evt.PostID, &replyRun{source: domain.TriggerSourceMention})
				if err != nil {
					// 前置跳过（帖子不可回/限频）已在 executeReply 内打 Info，不重复；
					// 调用链路失败已写日志行，这里补一条 Error 便于告警。
					if errors.Is(err, errPostNotReplyable) || errors.Is(err, errRateLimited) || errors.Is(err, errSkippedByFilter) {
						return
					}
					logger.Log.Error(fmt.Sprintf("agent reply: agent=%s post=%s: %s",
						agent.ID, evt.PostID, err.Error()))
				}
			}(agent)
		}
	}()
}

// ManualReply 管理员手动触发（同步，仅全局机器人）。
func (s *replyServiceImpl) ManualReply(ctx context.Context, adminID, agentID, postID uuid.UUID) (uuid.UUID, error) {
	if err := s.ensureReplyAdmin(ctx, adminID); err != nil {
		return uuid.Nil, err
	}
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return uuid.Nil, err
	}
	if agent.CircleID != nil {
		// 跨作用域不可见：全局控制台不展示圈内机器人，404 不泄露存在性
		//（原 errCircleReplyUnsupported 403 风格与全局链其余方法惯例不一致，已废）。
		return uuid.Nil, errAgentNotFound
	}
	if agent.Status != domain.AgentStatusEnabled {
		return uuid.Nil, errAgentDisabled
	}
	if domain.TriggerMode(agent.TriggerMode) != domain.TriggerModeManual {
		return uuid.Nil, errNotManualMode
	}
	commentID, err := s.executeReply(ctx, agent, postID, &replyRun{source: domain.TriggerSourceManual})
	if err != nil {
		return uuid.Nil, err
	}
	return *commentID, nil
}

// CircleManualReply 圈内手动触发（同步，仅该圈圈主，仅本圈帖）。
//
// 校验链：先 404 后 403（loadCircleAgent 同款惯例，不泄露归属）→ 圈主（手动触发
// 直烧圈主持有的计费凭据，owner-only 对齐凭据字段/删除分级）→ 启用 + mode=3 →
// 帖子门槛（已发布未锁定 + 归属机器人所在圈，跨圈 404 防拿本圈机器人刷它圈帖）。
// 限流/两阶段分类器与全局链共用 executeReply，per-agent 配置原样生效。
func (s *replyServiceImpl) CircleManualReply(ctx context.Context, operatorID, agentID, postID uuid.UUID) (uuid.UUID, error) {
	if s.postReader == nil || s.commentCreator == nil || s.llm == nil {
		return uuid.Nil, errors.New("agent reply dependencies not configured")
	}
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		if errors.Is(err, domain.ErrAgentNotFound) {
			return uuid.Nil, errAgentNotFound
		}
		return uuid.Nil, err
	}
	if agent.CircleID == nil {
		// 全局机器人不归圈内链管：圈内链不暴露全局机器人的存在性（404）。
		return uuid.Nil, errAgentNotFound
	}
	if err := s.requireCircleOwnerForReply(ctx, *agent.CircleID, operatorID); err != nil {
		return uuid.Nil, err
	}
	if agent.Status != domain.AgentStatusEnabled {
		return uuid.Nil, errAgentDisabled
	}
	if domain.TriggerMode(agent.TriggerMode) != domain.TriggerModeManual {
		return uuid.Nil, errNotManualMode
	}
	// 帖子门槛：已发布未锁定，且必须属于机器人所在圈（手动触发无候选集收口，
	// 此处即作用域主防线，agentInScope 语义与触发链一致）。
	post, err := s.postReader.GetPostBrief(ctx, postID)
	if err != nil {
		return uuid.Nil, err
	}
	if post == nil || post.Status != 1 || post.IsLock == 1 {
		return uuid.Nil, errPostNotReplyable
	}
	if !agentInScope(agent, post.CircleID) {
		return uuid.Nil, errPostNotInAgentCircle
	}
	commentID, err := s.executeReply(ctx, agent, postID, &replyRun{source: domain.TriggerSourceManual})
	if err != nil {
		return uuid.Nil, err
	}
	return *commentID, nil
}

// requireCircleOwnerForReply 圈内手动触发的圈主校验（fail-closed：端口未注入/
// 非成员/角色不足/状态非 normal 一律拒绝）。与 CircleAgentService.requireCircleOwner
// 同语义（独立实现：ReplyService 的注入点与失败语义不同，不强行共用结构）。
func (s *replyServiceImpl) requireCircleOwnerForReply(ctx context.Context, circleID, operatorID uuid.UUID) error {
	if s.circleRoles == nil {
		return errNotCircleOwner
	}
	role, status, ok, err := s.circleRoles.GetCircleMembership(ctx, circleID, operatorID)
	if err != nil || !ok || role != circleRoleOwner || status != memberStatusNormal {
		return errNotCircleOwner
	}
	return nil
}

// ensureReplyAdmin 手动触发的管理员校验（fail-closed，同 AgentService.ensureAdmin）。
func (s *replyServiceImpl) ensureReplyAdmin(ctx context.Context, adminID uuid.UUID) error {
	if s.roleReader == nil {
		return errNotAdmin
	}
	role, ok, err := s.roleReader.GetUserRole(ctx, adminID)
	if err != nil || !ok || role != roleAdmin {
		return errNotAdmin
	}
	return nil
}

// replyRun 一次回复执行的触发上下文（executeReply 入参）。
type replyRun struct {
	source domain.TriggerSource
	// trigger 非 nil 时回复挂在该评论楼层内（关键词触发 / 自动回复评论），并把评论原文拼进 prompt；
	// nil 为顶层评论（@提及 / 手动 / 自动回复帖子）。
	trigger *CommentEvent
	// placeholderID 自动回复占位行 ID（executeReply 内占位成功后回填）：非 Nil 时终态日志
	// 原地更新这一行，而不是追加新行（部分唯一索引下同一目标只能有一行）。
	placeholderID uuid.UUID
}

// targetCommentID 日志行的目标评论（回复挂在该评论楼层内时非 nil）。
func (r *replyRun) targetCommentID() *uuid.UUID {
	if r.trigger == nil {
		return nil
	}
	id := r.trigger.CommentID
	return &id
}

// useClassifier 是否走分类器（阶段1）：关键词触发与自动回复在配置了 filter_prompt 时判定；
// @提及 / 手动是显式点名，天然绕过。
func (r *replyRun) useClassifier(agent *domain.AiAgent) bool {
	return agent.FilterPrompt != "" &&
		(r.source == domain.TriggerSourceKeyword || r.source == domain.TriggerSourceAuto)
}

// executeReply 单个机器人对单个帖子的回复执行核心（关键词/@提及/手动/自动回复共用）。
//
// 返回 (成功时非nil的评论ID, error)。前置条件不满足（帖子不可回/限频/自动回复目标已占用）
// 返回对应哨兵错误且不写日志；调用链路失败（解密/LLM/空回复/评论落库）写失败
// 日志行后返回包装错误。被动触发同一帖子可被同一机器人多次回复（不设防重），
// 仅靠限频控制用量；自动回复由占位行 + 部分唯一索引保证每机器人每目标最多一次。
func (s *replyServiceImpl) executeReply(ctx context.Context, agent *domain.AiAgent, postID uuid.UUID, run *replyRun) (*uuid.UUID, error) {
	if s.postReader == nil || s.commentCreator == nil || s.llm == nil {
		return nil, errors.New("agent reply dependencies not configured")
	}
	trigger := run.trigger

	// 1. 帖子门槛：已发布且未锁定。
	post, err := s.postReader.GetPostBrief(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Status != 1 || post.IsLock == 1 {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=post_not_replyable", agent.ID, postID))
		return nil, errPostNotReplyable
	}
	// 自动回复没有事件载荷的圈子可比，按帖子实际所在圈再做一次作用域校验（候选查询已收口，第二道防线）。
	if run.source == domain.TriggerSourceAuto && !agentInScope(agent, post.CircleID) {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=out_of_scope", agent.ID, postID))
		return nil, errPostNotReplyable
	}

	// 2. 限频：最近 1h 行数（含失败）+ 距最新一条的间隔。
	now := time.Now()
	if agent.MaxRepliesPerHour > 0 {
		count, err := s.replyLogRepo.CountSinceByAgent(ctx, agent.ID, now.Add(-time.Hour))
		if err != nil {
			return nil, err
		}
		if count >= int64(agent.MaxRepliesPerHour) {
			logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=rate_limited_hourly count=%d limit=%d",
				agent.ID, postID, count, agent.MaxRepliesPerHour))
			return nil, errRateLimited
		}
	}
	if agent.MinIntervalSec > 0 {
		last, err := s.replyLogRepo.GetLastByAgent(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		if last != nil && last.CreateTime.Add(time.Duration(agent.MinIntervalSec)*time.Second).After(now) {
			logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=rate_limited_min_interval min_interval_sec=%d",
				agent.ID, postID, agent.MinIntervalSec))
			return nil, errRateLimited
		}
	}
	// 2.1 自动回复：每小时上限 + 每目标机器人数上限 + 占位（撞唯一索引即跳过）。
	// 占位必须在限频检查之后：占位行本身计入限频统计，先占位会把自己挡掉。
	if run.source == domain.TriggerSourceAuto {
		if err := s.claimAutoTarget(ctx, agent, post, run); err != nil {
			return nil, err
		}
	}

	// 3. 调用链路（失败写日志行）。
	start := time.Now()
	var promptTokens, completionTokens int

	fail := func(errMsg string) (*uuid.UUID, error) {
		s.writeReplyLog(run, &domain.ReplyLog{
			AgentID: agent.ID, PostID: postID, UserID: post.AuthorID,
			Status:       domain.ReplyStatusFailed,
			ErrorMsg:     truncateErr(errMsg),
			LatencyMs:    int(time.Since(start).Milliseconds()),
			PromptTokens: promptTokens, CompletionTokens: completionTokens,
		})
		return nil, fmt.Errorf("%w: %s", errLLMCall, errMsg)
	}

	// 3.1 解密 api_key。
	apiKey := ""
	if agent.APIKeyEnc != "" {
		apiKey, err = crypto.Decrypt(conf.Config.Security.DataKey, agent.APIKeyEnc)
		if err != nil {
			return fail("decrypt api_key failed: " + err.Error())
		}
	}

	// 3.2 分类器判定（阶段1）：关键词触发 / 自动回复且配置了判定条件时执行。
	// @提及 / 手动是显式点名，天然绕过；filter_prompt 为空时跳过，行为与旧版一致。
	if run.useClassifier(agent) {
		failErr := func(msg string) error { _, e := fail(msg); return e }
		if err := s.runClassifier(ctx, agent, post, run, apiKey, start, failErr, &promptTokens, &completionTokens); err != nil {
			return nil, err
		}
	}

	// 3.3 LLM 生成（阶段2，system_prompt 空时用默认人设兜底）。
	// 生成持独立子预算 replyTimeout：分类器（阶段1）耗时不影响本阶段额度，
	// 分类器超时降级后仍有完整时间产出直接回复；父 ctx 取消（手动触发的
	// 请求取消 / 关键词整链 2*replyTimeout 兜底）仍会正常中断本阶段。
	systemPrompt := agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	genCtx, genCancel := context.WithTimeout(ctx, replyTimeout())
	res, err := s.llm.Generate(genCtx, LLMRequest{
		Protocol:     agent.APIProtocol,
		BaseURL:      agent.BaseURL,
		APIKey:       apiKey,
		Model:        agent.Model,
		Params:       agent.LLMParams,
		SystemPrompt: systemPrompt,
		UserPrompt:   buildUserPrompt(agent, post, trigger),
	})
	genCancel()
	if err != nil {
		return fail("llm generate failed: " + err.Error())
	}
	promptTokens += res.PromptTokens
	completionTokens += res.CompletionTokens

	// 3.4 清洗输出（用户文本入库前必须过 SanitizeForPg）。
	content := utils.SanitizeForPg(strings.TrimSpace(res.Content))
	if content == "" {
		return fail("llm returned empty content")
	}

	// 3.5 评论落库：关键词触发 / 自动回复评论挂进目标评论楼层，其余为顶层评论。
	var rootID, replyToID *uuid.UUID
	if trigger != nil {
		replyToID = &trigger.CommentID
		if trigger.RootID != nil {
			rootID = trigger.RootID
		} else {
			cid := trigger.CommentID
			rootID = &cid
		}
	}
	commentID, err := s.commentCreator.CreateComment(ctx, agent.LinkedUserID, CommentCreateInput{
		PostID: postID, Content: content, RootID: rootID, ReplyToID: replyToID,
	})
	if err != nil {
		return fail("create comment failed: " + err.Error())
	}

	// 3.6 成功日志行。
	s.writeReplyLog(run, &domain.ReplyLog{
		AgentID: agent.ID, PostID: postID, UserID: post.AuthorID,
		CommentID:        &commentID,
		Status:           domain.ReplyStatusOK,
		LatencyMs:        int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})
	return &commentID, nil
}

// runClassifier 执行分类器判定（阶段1）。返回 nil = 继续生成阶段；
// 返回 errSkippedByFilter = 判定不回复（status=2 日志已写）；
// 返回其他错误 = 调用/解析失败（fail-closed，失败日志行已经 failErr 写好）。
//
// 超时（context.DeadlineExceeded）是唯一 fail-open 路径：判定是省 token/提质的
// 优化而非门槛，慢端点（如方舟 coding 推理模型，TTFT 数十秒）超时降级直回，
// 记 status=3（ReplyStatusClassifierTimeout）运维事件日志，error_msg 前缀
// classifier_timeout_fallback 便于观察频率；与 status=2（分类器判定不回复）
// 区分，避免运维事件被误读为内容拒绝。status=3 同样不参与限频统计（若降级后
// 产出回复，另有 status=0/1 行计数）。
// 分类器持独立子预算 replyTimeout：父 ctx（关键词整链 2*replyTimeout / 手动
// 触发为请求 ctx）取消仍会中断本阶段，但分类器耗时不吃掉生成阶段预算。
func (s *replyServiceImpl) runClassifier(
	ctx context.Context,
	agent *domain.AiAgent,
	post *PostBrief,
	run *replyRun,
	apiKey string,
	start time.Time,
	failErr func(string) error,
	promptTokens, completionTokens *int,
) error {
	judgeParams := copyLLMParams(agent.LLMParams)
	// 不接受采样参数的模型（如 anthropic 新一代）由 infrastructure 丢弃该覆盖（#140）。
	judgeParams["temperature"] = float64(0)
	judgeParams["max_tokens"] = float64(judgeMaxTokens)
	judgeCtx, judgeCancel := context.WithTimeout(ctx, replyTimeout())
	jres, err := s.llm.Generate(judgeCtx, LLMRequest{
		Protocol:     agent.APIProtocol,
		BaseURL:      agent.BaseURL,
		APIKey:       apiKey,
		Model:        agent.Model,
		Params:       judgeParams,
		SystemPrompt: judgeSystemPrompt,
		UserPrompt:   buildJudgeUserPrompt(agent, post, run.trigger),
	})
	judgeCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// token 未知，不计入。status=3 运维事件（非分类器拒绝），不参与限频统计。
			logger.Log.Info(fmt.Sprintf("agent reply: classifier timeout, fallback to direct reply: agent=%s post=%s", agent.ID, post.ID))
			s.writeReplyLog(run, &domain.ReplyLog{
				AgentID: agent.ID, PostID: post.ID, UserID: post.AuthorID,
				Status:    domain.ReplyStatusClassifierTimeout,
				ErrorMsg:  truncateErr("classifier_timeout_fallback: " + err.Error()),
				LatencyMs: int(time.Since(start).Milliseconds()),
			})
			return nil
		}
		return failErr("classifier generate failed: " + err.Error())
	}
	*promptTokens += jres.PromptTokens
	*completionTokens += jres.CompletionTokens
	judge, err := parseJudgeResult(jres.Content)
	if err != nil {
		// fail-closed：解析失败宁可漏回，不可放进未判定内容。error_msg 带原始输出现场便于诊断。
		return failErr("classifier parse failed: " + err.Error() + " raw=" + truncateRunes(jres.Content, 100))
	}
	if !judge.Reply {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=classifier_rejected detail=%s",
			agent.ID, post.ID, judge.Reason))
		s.writeReplyLog(run, &domain.ReplyLog{
			AgentID: agent.ID, PostID: post.ID, UserID: post.AuthorID,
			Status:           domain.ReplyStatusSkipped,
			ErrorMsg:         truncateErr(judge.Reason),
			LatencyMs:        int(time.Since(start).Milliseconds()),
			PromptTokens:     *promptTokens,
			CompletionTokens: *completionTokens,
		})
		return errSkippedByFilter
	}
	return nil
}

// writeReplyLog 落日志行，补齐触发来源与目标评论。写库失败仅告警（调用链路已尽力）。
//
// 被动触发 append-only 追加新行；自动回复（占位成功后）把终态原地写回占位行——
// 部分唯一索引下同一目标只能有一行，status=3（分类器超时降级）这类中间事件不单独落行，只记 zap。
// 用 Background ctx：调用链路 ctx 可能已超时 / 取消，终态仍要落库。
func (s *replyServiceImpl) writeReplyLog(run *replyRun, log *domain.ReplyLog) {
	log.TriggerSource = run.source
	log.TargetCommentID = run.targetCommentID()
	if run.placeholderID != uuid.Nil {
		if log.Status == domain.ReplyStatusClassifierTimeout {
			return
		}
		log.ID = run.placeholderID
		if err := s.replyLogRepo.FinishAutoPlaceholder(context.Background(), log); err != nil {
			logger.Log.Error("agent reply: finish auto reply placeholder failed: " + err.Error())
		}
		return
	}
	log.ID = sharedomain.NewID()
	if err := s.replyLogRepo.Create(context.Background(), log); err != nil {
		logger.Log.Error("agent reply: write reply log failed: " + err.Error())
	}
}

// matchKeyword 评论内容是否命中任一触发关键词（不区分大小写子串匹配）。
func matchKeyword(content string, keywords domain.KeywordsJSON) bool {
	if len(keywords) == 0 {
		return false
	}
	lower := strings.ToLower(content)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// copyLLMParams 复制 LLM 参数表（分类器覆盖 temperature/max_tokens 不污染原 map）。
func copyLLMParams(src domain.LLMParamsJSON) map[string]interface{} {
	dst := make(map[string]interface{}, len(src)+2)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// buildJudgeUserPrompt 组装分类器用户提示词：帖子 +（有则）触发评论 + 判定条件，各段截断。
func buildJudgeUserPrompt(agent *domain.AiAgent, post *PostBrief, trigger *CommentEvent) string {
	limit := maxContentChars()
	var b strings.Builder
	b.WriteString(truncateRunes("帖子标题："+post.Title, limit))
	b.WriteString("\n")
	b.WriteString(truncateRunes("帖子摘要："+post.Summary, limit))
	b.WriteString("\n")
	if trigger != nil {
		b.WriteString(truncateRunes("用户评论："+trigger.Content, limit))
		b.WriteString("\n")
	}
	b.WriteString(truncateRunes("判定条件："+agent.FilterPrompt, limit))
	return b.String()
}

// parseJudgeResult 解析分类器 JSON 输出（容错剥 markdown code fence）。
// unmarshal 失败时兜底正则抢救 "reply" 字段（截断的半截 JSON 仍可取判定）；
// 兜底也失败返回错误，调用方 fail-closed（不回复）。
func parseJudgeResult(content string) (*judgeResult, error) {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	var r judgeResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		if m := judgeReplyRe.FindStringSubmatch(s); m != nil {
			return &judgeResult{Reply: m[1] == "true", Reason: "（输出截断，reason 丢失）"}, nil
		}
		return nil, err
	}
	return &r, nil
}

// buildUserPrompt 组装用户提示词：title + summary（+ 评论触发的评论原文），各段截断。
func buildUserPrompt(agent *domain.AiAgent, post *PostBrief, trigger *CommentEvent) string {
	limit := maxContentChars()
	var b strings.Builder
	b.WriteString(truncateRunes("帖子标题："+post.Title, limit))
	b.WriteString("\n")
	b.WriteString(truncateRunes("帖子摘要："+post.Summary, limit))
	b.WriteString("\n")
	if trigger != nil {
		b.WriteString(truncateRunes("用户评论："+trigger.Content, limit))
		b.WriteString("\n")
	}
	b.WriteString("请以「" + agent.Name + "」的身份对上述帖子写一条回复。")
	return b.String()
}

// truncateRunes 按字符数截断（避免截断 UTF-8 多字节字符），超长加省略号。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// truncateErr 失败原因截断（error_msg 列 varchar(2048)）。
func truncateErr(msg string) string {
	runes := []rune(msg)
	if len(runes) <= errMsgMaxLen {
		return msg
	}
	return string(runes[:errMsgMaxLen])
}
