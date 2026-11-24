// Package application 提供 comment 领域的应用服务层。
//
// 职责：
//   - 发评论/回复（含帖子状态校验、楼层校验、内容清洗）
//   - 获取顶层评论列表（游标分页）
//   - 获取楼层内回复列表（游标分页）
//   - 获取单条评论详情
//   - 组装评论者/被回复人信息（通过 UserFacade）
//   - 点赞状态查询（Redis ZSET 缓存 + DB 回源）
package application

import (
	"context"
	"encoding/json"
	"time"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/shared/mention"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// UserBrief 用户精简视图（与 user.application.UserBrief 字段一致，独立定义避免跨领域 import）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserFacade comment 领域需要的 user 查询接口。
type UserFacade interface {
	// GetBriefs 批量获取用户精简视图。未找到的用户不会出现在 map 里。
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// GetBrief 获取单个用户精简视图。未找到返回 nil, nil。
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
	// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（仅含圈内机器人与不可 @ 的
	// 机器人，普通用户/可 @ 的全局机器人不出现在 map）。mention 兜底剔除越圈、停用或已删除的机器人用。
	GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error)
}

// AgentMentionScope 机器人账号的 @提及 可见范围（user 域投影列的跨域视图）。
// 与 post 域共用 pkg/shared/mention 的同一个类型与剔除规则（mention.StripUnmentionableAgents）。
type AgentMentionScope = mention.AgentScope

// PostInfo comment 领域需要的帖子信息（发评论时校验用）。
type PostInfo struct {
	ID     uuid.UUID
	Status int16 // 帖子状态（PostStatusPublished=1 表示可评论）
	IsLock int16 // 是否锁定（1=锁定）
	// 帖子所属圈子（mention 兜底剔除越圈机器人用；草稿可能为 Nil）。
	CircleID uuid.UUID
}

// PostLookup comment 领域需要的帖子查询端口。
//
// 用于发评论时校验帖子存在性/状态/锁定，以及恢复帖子统计缓存。
type PostLookup interface {
	// GetPost 返回帖子信息。未找到返回 nil, nil。
	GetPost(ctx context.Context, postID uuid.UUID) (*PostInfo, error)
	// RestoreStatsAndIncrCommentCount 恢复帖子统计缓存（如果不存在），
	// 然后递增帖子评论计数（Redis Hash 实时 + 帖子统计事件批量落库，#130）。
	RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error
	// CanView 查看者能否看到该帖（私密圈仅成员可见，访客视为非成员；帖子不存在返回 false）。
	CanView(ctx context.Context, viewerID, postID uuid.UUID) (bool, error)
}

// AgentReplyTrigger AI 机器人回复触发端口（composition 桥接 aiagent.ReplyService）。
//
// 评论创建成功后同步回调；实现方必须立即返回（内部异步执行），
// 不向评论创建链路传播任何错误。
type AgentReplyTrigger interface {
	// OnCommentCreated 评论创建完成后的触发入口。
	// postCircleID 为帖子所属圈子（机器人回复的作用域匹配依据：
	// 圈子级机器人只在同圈帖触发）；rootID 为 nil 表示顶层评论。
	OnCommentCreated(postID, postCircleID, commentID, userID uuid.UUID, rootID *uuid.UUID, content string)
}

// ===== DTO =====

// CommentVO 评论 VO（包含评论信息 + 评论者信息 + 被回复人信息 + 当前用户点赞状态）。
type CommentVO struct {
	ID         uuid.UUID  `json:"id"`
	PostID     uuid.UUID  `json:"post_id"`
	UserID     uuid.UUID  `json:"user_id"`
	RootID     *uuid.UUID `json:"root_id,omitempty"`
	ReplyToID  *uuid.UUID `json:"reply_to_id,omitempty"`
	Content    string     `json:"content"`
	LikeCount  int        `json:"like_count"`
	ReplyCount int        `json:"reply_count"`
	Status     int16      `json:"status"`
	CreateTime string     `json:"create_time"`

	// 扩展数据（JSON格式，包含图片URL数组等）
	// 用 json.RawMessage 而非 []byte：前者 MarshalJSON 原样透传嵌套 JSON，
	// 后者会被 encoding/json 当 []byte 做 base64 编码，导致前端读不到 extra_data.images。
	ExtraData json.RawMessage `json:"extra_data,omitempty"`

	// 评论者信息
	AuthorName   string `json:"author_name"`
	AuthorAvatar string `json:"author_avatar"`

	// 被回复人信息（仅回复时有值）
	ReplyToUserID *uuid.UUID `json:"reply_to_user_id,omitempty"`
	ReplyToName   string     `json:"reply_to_name,omitempty"`

	// 用户交互状态
	Liked bool `json:"liked"` // 当前用户是否点赞了该评论

	// @提及 用户列表（发评论时落库的最终名单，仅含未注销用户）。
	// 缺失/为空时前端回退文本反查建链，不报错。
	Mentions []MentionVO `json:"mentions"`
}

// MentionVO 评论 @提及 用户视图（mentions 数组元素）。
//
// username 为当前用户名（发评论时可能不同）：前端与正文 token 做大小写不敏感
// 整名比对，改名后旧内容可能匹配不上（不建链、不会错链），为契约内边界。
type MentionVO struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// CommentListResult 评论列表结果（游标分页）。
type CommentListResult struct {
	Items      []CommentVO `json:"items"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor"`
}

// 页大小常量。LocateComment 的定位计算与列表接口必须共用同一常量，
// 否则定位游标算出的页与实际分页页大小错位（off-by-one 防线）。
const (
	// rootCommentPageSize 顶层评论列表页大小（GetRootComments 固定值）。
	rootCommentPageSize = 20
	// defaultReplyPageSize 回复列表默认页大小（GetReplies limit 缺省值；
	// 前端拉回复不传 limit 时两边一致）。
	defaultReplyPageSize = 10
)

// CommentLocateResult 评论定位结果（通知点击直达，设计见 docs/comment-locate-design.md）。
type CommentLocateResult struct {
	CommentID   uuid.UUID `json:"comment_id"`   // 回显目标评论 ID
	PostID      uuid.UUID `json:"post_id"`      // 目标所属帖子 ID（前端防串帖校验）
	RootID      uuid.UUID `json:"root_id"`      // 所属顶层评论 ID；is_root=1 时等于 comment_id
	IsRoot      int       `json:"is_root"`      // 1=顶层评论, 0=回复
	ListCursor  *string   `json:"list_cursor"`  // 顶层列表定位游标；null=根评论在首页
	ReplyCursor *string   `json:"reply_cursor"` // 仅 is_root=0 有意义；null=在回复首页
	ReplyPage   int       `json:"reply_page"`   // 仅 is_root=0 有意义（从 1）；is_root=1 固定 0
}

// CreateCommentInput 发评论/回复入参。
type CreateCommentInput struct {
	PostID         uuid.UUID
	Content        string
	ExtraData      []byte // json.RawMessage
	RootID         *uuid.UUID
	ReplyToID      *uuid.UUID
	MentionUserIDs []uuid.UUID // @提及用户（前端选人传入；后端校验存在性/去自/截断）
	// SystemActor 系统身份发评论（AI 机器人回复链路）：跳过私密圈可见性校验，
	// 机器人在私密圈的行为另行处理（#170 范围外）。HTTP 入口恒为 false。
	SystemActor bool
}

// AutoReplyCommentQuery 机器人自动回复的评论候选查询条件。
// LikeMin / ReplyMin 满足任一即可，<=0 表示该条件不参与。
type AutoReplyCommentQuery struct {
	CircleID   uuid.UUID   // 所属帖子的圈子范围；uuid.Nil = 全站
	LikeMin    int         // 点赞数门槛（含）
	ReplyMin   int         // 回复数门槛（含）
	Since      time.Time   // 只看该时间之后发布的评论
	ExcludeIDs []uuid.UUID // 排除的评论（已触发 / 已达每目标上限）
	Limit      int         // 候选上限
}

// AutoReplyCommentCandidate 机器人自动回复的评论候选。
type AutoReplyCommentCandidate struct {
	ID           uuid.UUID
	PostID       uuid.UUID
	PostCircleID uuid.UUID
	UserID       uuid.UUID
	RootID       *uuid.UUID // nil = 顶层评论
	Content      string
	LikeCount    int
	ReplyCount   int
}

// ===== Service 接口 =====

// CommentService 是 comment 领域的应用服务接口。
type CommentService interface {
	// CreateComment 发评论（支持顶层评论和回复）。
	CreateComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput) (uuid.UUID, error)
	// GetRootComments 获取帖子的顶层评论列表（游标分页）。
	// sort: 0=按点赞倒序(默认), 1=按时间倒序。
	GetRootComments(ctx context.Context, userID, postID uuid.UUID, sort int, cursor string) (*CommentListResult, error)
	// GetReplies 获取某条评论的子回复列表（游标分页）。
	// sort: 0=按点赞倒序(默认), 1=按时间倒序（与顶层列表同一套排序键映射）。
	GetReplies(ctx context.Context, userID, rootID uuid.UUID, limit, sort int, cursor string) (*CommentListResult, error)
	// LocateComment 定位评论（通知点击直达，设计见 docs/comment-locate-design.md）。
	// 返回目标所属根评论在顶层列表的定位游标；目标是回复时另返回回复列表定位游标与页码。
	// sort 语义同 GetRootComments；replySort 语义同 GetReplies（回复页计算用，
	// 前端须与拉取回复时的 sort 一致，否则回复游标无效）。
	// 评论或（回复目标的）根评论不存在/已删除时返回 ErrCommentNotFound。
	// 所属帖子对 viewerID 不可见（私密圈非成员）时同样返回 ErrCommentNotFound。
	LocateComment(ctx context.Context, viewerID, commentID uuid.UUID, sort, replySort int) (*CommentLocateResult, error)
	// GetCommentDetail 获取单条评论详情。
	GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error)

	// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
	// 返回评论所属帖子ID。未找到返回 nil, nil。
	GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error)
	// IsLikedByUser 用户是否已赞该评论（缓存优先，miss 回源 DB，DB 已赞则回填缓存）。
	// 供 like 领域点赞前解析真实状态；DB 错误原样返回（调用方不得猜测）。
	IsLikedByUser(ctx context.Context, userID, commentID uuid.UUID) (bool, error)
	// ListAutoReplyCandidates 机器人自动回复的评论候选（供 aiagent 扫描器经 Facade 调用）：
	// 评论正常、时间窗口内、点赞 / 回复数达到任一门槛、作者非机器人账号、所属帖子可回复且在
	// 圈子范围内，按 like_count + reply_count 降序 top-N。
	ListAutoReplyCandidates(ctx context.Context, q AutoReplyCommentQuery) ([]AutoReplyCommentCandidate, error)

	// SetUserFacade 注入 user Facade（组装评论者/被回复人信息用）。
	SetUserFacade(f UserFacade)
	// SetPostLookup 注入帖子查询端口（发评论校验 + 帖子评论计数用）。
	SetPostLookup(p PostLookup)
	// SetAgentTrigger 注入 AI 机器人回复触发端口（评论创建后回调）。
	SetAgentTrigger(t AgentReplyTrigger)
}

type commentServiceImpl struct {
	repo         domain.CommentRepository
	likeCache    domain.CommentLikeCache
	publisher    domain.CommentEventPublisher
	userFacade   UserFacade
	postLookup   PostLookup
	agentTrigger AgentReplyTrigger
}

// NewCommentService 构造 CommentService。
//
// publisher 是评论事件发布（同域 infra，构造注入）；
// userFacade / postLookup 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewCommentService(
	repo domain.CommentRepository,
	likeCache domain.CommentLikeCache,
	publisher domain.CommentEventPublisher,
) CommentService {
	return &commentServiceImpl{
		repo:      repo,
		likeCache: likeCache,
		publisher: publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *commentServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }
func (s *commentServiceImpl) SetPostLookup(p PostLookup) { s.postLookup = p }

// SetAgentTrigger 注入 AI 机器人回复触发端口（未注入则评论创建不触发机器人）。
func (s *commentServiceImpl) SetAgentTrigger(t AgentReplyTrigger) { s.agentTrigger = t }
