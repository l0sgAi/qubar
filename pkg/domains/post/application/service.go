// Package application 提供 post 领域的应用服务层。
//
// 职责：
//   - 发帖（含成员身份校验、内容清洗、摘要生成、圈子帖子计数）
//   - 帖子详情（含点赞状态、浏览量异步累加、发帖人信息组装）
//   - 帖子列表搜索（含批量组装作者/圈子/图片信息）
package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// UserBrief 用户精简视图（与 user.application.UserBrief 字段一致，独立定义避免跨领域 import）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// MentionVO 帖子 @提及 用户视图（详情接口 mentions 数组元素）。
//
// username 为当前用户名（发帖时可能不同）：前端与正文 token 做大小写不敏感
// 整名比对，改名后旧内容可能匹配不上（不建链、不会错链），为契约内边界。
type MentionVO struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// UserFacade post 领域需要的 user 查询接口。
type UserFacade interface {
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
	// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（仅含圈内机器人行，
	// 普通用户/全局机器人不出现在 map）。mention 兜底剔除越圈机器人用。
	GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}

// CircleBrief 圈子精简视图。
type CircleBrief struct {
	ID        string
	Name      string
	AvatarURL string
}

// CircleFacade post 领域需要的 circle 查询接口。
type CircleFacade interface {
	GetBriefs(ctx context.Context, circleIDs []string) (map[string]CircleBrief, error)
}

// CircleMemberInfo 圈子成员信息（用于发帖时的成员校验）。
type CircleMemberInfo struct {
	Role        int16
	Status      int16
	MuteEndTime *time.Time
}

// CircleMemberChecker 检查用户在圈子中的成员身份（发帖校验用）。
type CircleMemberChecker interface {
	// GetMember 返回成员信息。不是成员返回 nil, nil。
	GetMember(ctx context.Context, circleID, userID uuid.UUID) (*CircleMemberInfo, error)
}

// CircleStatusChecker 检查圈子状态（发帖校验用）。
type CircleStatusChecker interface {
	// IsCircleAvailable 检查圈子是否可用（status=normal）。
	IsCircleAvailable(ctx context.Context, circleID uuid.UUID) (bool, error)
}

// CirclePostCountPort 圈子帖子计数端口（发帖后递增圈子帖子计数）。
type CirclePostCountPort interface {
	IncrPostCount(ctx context.Context, circleID uuid.UUID) error
}

// AgentPostTrigger AI 机器人回复触发端口（composition 桥接 aiagent.ReplyService）。
//
// 已发布帖子的 @提及 落库后同步回调；实现方必须立即返回（内部异步执行），
// 不向发帖链路传播任何错误。
type AgentPostTrigger interface {
	// OnPostMentioned 发帖 @提及 触发入口。
	// circleID 为帖子所属圈子（圈子级机器人只在同圈帖触发）；
	// mentionUserIDs 为已校验落库的最终名单；authorID 为发帖人。
	OnPostMentioned(postID, circleID, authorID uuid.UUID, mentionUserIDs []uuid.UUID)
}

// ===== 搜索结果 DTO =====

// PostDoc 帖子搜索结果项（ES PostDocument 精简版）。
type PostDoc struct {
	ID           string `json:"id"`
	CircleID     string `json:"circle_id"`
	UserID       string `json:"user_id"`
	Type         int16  `json:"type"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	Content      string `json:"content"`
	ViewCount    int    `json:"view_count"`
	CommentCount int    `json:"comment_count"`
	LikeCount    int    `json:"like_count"`
	CollectCount int    `json:"collect_count"`
	IsPinned     int16  `json:"is_pinned"`
	IsEssence    int16  `json:"is_essence"`
	IsLock       int16  `json:"is_lock"`
	Status       int16  `json:"status"`
	CreateTime   string `json:"create_time"`
}

// PostListItem 组装后的帖子列表项。
type PostListItem struct {
	ID           uuid.UUID `json:"id"`
	CircleID     uuid.UUID `json:"circle_id"`
	UserID       uuid.UUID `json:"user_id"`
	Type         int16     `json:"type"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Content      string    `json:"content"`
	ViewCount    int       `json:"view_count"`
	CommentCount int       `json:"comment_count"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	IsPinned     int16     `json:"is_pinned"`
	IsEssence    int16     `json:"is_essence"`
	IsLock       int16     `json:"is_lock"`
	Status       int16     `json:"status"`
	CreateTime   time.Time `json:"create_time"`
	AuthorName   string    `json:"author_name"`
	AuthorAvatar string    `json:"author_avatar"`
	CircleName   string    `json:"circle_name"`
	CircleAvatar string    `json:"circle_avatar"`
	Images       []string  `json:"images"`
}

// PostSearchResult 帖子搜索结果（含组装后的列表）。
type PostSearchResult struct {
	Posts       []PostListItem `json:"posts"`
	Total       int64          `json:"total"`
	Size        int            `json:"size"`
	SearchAfter string         `json:"search_after"`
}

// RawPostSearchResult 是 searcher 返回的原始 ES 结果（未组装）。
type RawPostSearchResult struct {
	Posts       []PostDoc
	Total       int64
	Size        int
	SearchAfter string
}

// PostSearcher 帖子搜索抽象。
type PostSearcher interface {
	Search(ctx context.Context, keyword string, circleID uuid.UUID, size int, searchAfter []interface{}) (*RawPostSearchResult, error)
	// SearchMy 搜索指定用户自己的帖子（"我的发帖"，keyword 为空时返回该用户全部帖子）。
	SearchMy(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*RawPostSearchResult, error)
	// SearchByUser 搜索指定用户已发布的帖子（查看「他人」发帖，强制 status=1）。
	SearchByUser(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*RawPostSearchResult, error)
	// SearchByIDs 按 ID 列表批量查询帖子(ES terms 查询,仅未删除 + 已发布)。
	// 顺序不保证,调用方自行按业务序重排;失效帖(被删/未发布)静默过滤。
	// 供 history「最近浏览」列表用。
	SearchByIDs(ctx context.Context, postIDs []uuid.UUID, size int) (*RawPostSearchResult, error)
	// SearchByIDsAndKeyword 在 ID 集合内按关键字搜索帖子(title^3/summary multi_match),
	// 按 _score desc 排序,offset 分页。供 history「最近浏览」关键字搜索用。
	SearchByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) (*RawPostSearchResult, error)
}

// PostDetailVO 帖子详情 VO。
type PostDetailVO struct {
	ID            uuid.UUID             `json:"id"`
	CircleID      uuid.UUID             `json:"circle_id"`
	UserID        uuid.UUID             `json:"user_id"`
	Type          int16                 `json:"type"`
	Title         string                `json:"title"`
	Summary       string                `json:"summary"`
	Content       string                `json:"content"`
	MediaExtra    domain.MediaExtraJSON `json:"media_extra"`
	ViewCount     int                   `json:"view_count"`
	CommentCount  int                   `json:"comment_count"`
	LikeCount     int                   `json:"like_count"`
	CollectCount  int                   `json:"collect_count"`
	IsPinned      int16                 `json:"is_pinned"`
	IsEssence     int16                 `json:"is_essence"`
	IsLock        int16                 `json:"is_lock"`
	Status        int16                 `json:"status"`
	Deleted       int16                 `json:"deleted"`
	CreateTime    time.Time             `json:"create_time"`
	UpdateTime    time.Time             `json:"update_time"`
	LastReplyTime *time.Time            `json:"last_reply_time,omitempty"`
	AuthorID      uuid.UUID             `json:"author_id"`
	AuthorName    string                `json:"author_name"`
	AuthorAvatar  string                `json:"author_avatar"`
	IsLiked       bool                  `json:"is_liked"`
	IsCollected   bool                  `json:"is_collected"`

	// @提及 用户列表（发帖时落库的最终名单，仅含未注销用户）。
	// 缺失/为空时前端回退文本反查建链，不报错。
	Mentions []MentionVO `json:"mentions"`
}

// CreatePostInput 发帖入参。
type CreatePostInput struct {
	CircleID       uuid.UUID
	Title          string
	Content        string
	Summary        string
	Type           int16
	MediaExtra     []string
	Status         int16
	MentionUserIDs []uuid.UUID // @提及用户（前端选人传入；后端校验存在性/去自/截断）
}

// PostService 是 post 领域的应用服务接口。
type PostService interface {
	CreatePost(ctx context.Context, userID uuid.UUID, input CreatePostInput) (uuid.UUID, error)
	GetPostDetail(ctx context.Context, userID, postID uuid.UUID) (*PostDetailVO, error)
	SearchPosts(ctx context.Context, keyword string, circleID uuid.UUID, size int, searchAfter []interface{}) (*PostSearchResult, error)
	// GetMyPosts 查看自己发的帖（按 userID 过滤，支持 title/summary 模糊关键字）。
	// 不过滤 status，作者可见自己全部状态（草稿/审核/已发布/锁定），仅排除已删除。
	GetMyPosts(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostSearchResult, error)
	// GetUserPosts 查看任意指定用户的发帖记录（按 targetUserID 过滤，支持 title/summary 模糊关键字）。
	// 强制 status=1：他人不可见对方草稿/审核/拒绝/封禁帖，仅返回已发布。
	GetUserPosts(ctx context.Context, targetUserID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostSearchResult, error)
	// GetMediaByPostIDs 批量获取帖子图片（供 circle 领域 GetCirclePosts 调用）。
	GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error)
	// GetPostsByIDs 按 ID 列表批量获取已组装的帖子（仅未删除 + 已发布）。
	// 供 collect 领域「我的收藏」列表调用。顺序不保证，调用方自行按收藏时间排序；失效帖静默过滤。
	GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]PostListItem, error)
	// SearchPostsByIDs 按 ID 列表从 ES 获取已组装帖子(供 history「最近浏览」列表调用)。
	// ES terms 查询,仅未删除 + 已发布;顺序不保证,调用方自行按浏览时间排序,失效帖静默过滤。
	SearchPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]PostListItem, error)
	// SearchPostsByIDsAndKeyword 在 ID 集合内按关键字搜索并组装帖子(供 history「最近浏览」关键字搜索)。
	// title^3/summary multi_match + 失效帖过滤,按 _score desc 排序,offset 分页;
	// 返回匹配总数(供上层 next_offset 计算)。keyword/postIDs 为空返回空列表。
	SearchPostsByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) ([]PostListItem, int64, error)
	// ListCircleIDsByPostIDs 批量取帖子所属 circle_id（去重，仅未删除+已发布）。
	// 供 recommend 域 C3 行为圈子召回用（seed 帖子 → 圈子）。
	ListCircleIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error)

	// GetPostMeta 获取帖子元信息（供 comment/like 领域校验用）。
	// 未找到返回 nil, nil。
	GetPostMeta(ctx context.Context, postID uuid.UUID) (*PostMeta, error)
	// GetPostBrief 获取帖子内容摘要（供 aiagent 领域组装机器人回复 prompt 用）。
	// 未找到返回 nil, nil。
	GetPostBrief(ctx context.Context, postID uuid.UUID) (*PostBrief, error)
	// RestoreStatsAndIncrCommentCount 恢复帖子统计缓存（如果不存在），
	// 然后递增帖子评论计数（Redis Hash + Redpanda 异步持久化）。
	// 供 comment 领域发评论后调用。
	RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error
	// RestoreStats 恢复帖子统计缓存（如果不存在）。
	// 供 like 领域点赞前确保 Redis stats Hash 存在。
	RestoreStats(ctx context.Context, postID uuid.UUID) error
	// IsLikedByUser 用户是否已赞该帖（缓存优先，miss 回源 DB，DB 已赞则回填缓存）。
	// 供 like 领域点赞前解析真实状态；DB 错误原样返回（调用方不得猜测）。
	IsLikedByUser(ctx context.Context, userID, postID uuid.UUID) (bool, error)

	// SetUserFacade 注入 user Facade（组装作者信息用）。
	SetUserFacade(f UserFacade)
	// SetCircleFacade 注入 circle Facade（组装圈子信息用）。
	SetCircleFacade(f CircleFacade)
	// SetMemberChecker 注入成员身份校验器（发帖校验用）。
	SetMemberChecker(c CircleMemberChecker)
	// SetStatusChecker 注入圈子状态校验器（发帖校验用）。
	SetStatusChecker(c CircleStatusChecker)
	// SetPostCountPort 注入圈子帖子计数端口（发帖后递增计数用）。
	SetPostCountPort(p CirclePostCountPort)
	// SetCollectCache 注入收藏状态缓存（详情页 is_collected 回显用）。
	SetCollectCache(c domain.PostCollectCache)
	// SetHistoryRecorder 注入浏览历史记录器（详情页浏览 async 回调用）。
	SetHistoryRecorder(r domain.HistoryRecorder)
	// SetAgentTrigger 注入 AI 机器人回复触发端口（未注入则发帖 @机器人 不触发回复）。
	SetAgentTrigger(t AgentPostTrigger)
}

// PostMeta 帖子元信息（供 comment/like 领域校验用）。
//
// 字段刻意精简：comment/like 只关心"帖子是否存在 + 是否可评论/可点赞"。
type PostMeta struct {
	ID     uuid.UUID
	Status int16 // 帖子状态
	IsLock int16 // 是否锁定
	// 帖子所属圈子（comment 域 mention 兜底剔除越圈机器人用；草稿可能为 Nil）。
	CircleID uuid.UUID
}

// PostBrief 帖子内容摘要（供 aiagent 领域组装机器人回复 prompt 用）。
type PostBrief struct {
	ID       uuid.UUID
	Title    string
	Summary  string
	Status   int16 // 帖子状态
	IsLock   int16 // 是否锁定
	AuthorID uuid.UUID
	CircleID uuid.UUID // 帖子所属圈子（机器人回复作用域匹配用）
}

type postServiceImpl struct {
	repo          domain.PostRepository
	statsCache    domain.PostStatsCache
	likeCache     domain.PostLikeCache
	collectCache  domain.PostCollectCache
	searcher      PostSearcher
	publisher     domain.PostEventPublisher
	userFacade    UserFacade
	circleFacade  CircleFacade
	memberCheck   CircleMemberChecker
	statusCheck   CircleStatusChecker
	postCountPort CirclePostCountPort
	historyRec    domain.HistoryRecorder
	agentTrigger  AgentPostTrigger
}

// NewPostService 构造 PostService。
//
// userFacade / circleFacade / memberCheck / statusCheck / postCountPort 是
// 跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewPostService(
	repo domain.PostRepository,
	statsCache domain.PostStatsCache,
	likeCache domain.PostLikeCache,
	collectCache domain.PostCollectCache,
	searcher PostSearcher,
	publisher domain.PostEventPublisher,
) PostService {
	return &postServiceImpl{
		repo:         repo,
		statsCache:   statsCache,
		likeCache:    likeCache,
		collectCache: collectCache,
		searcher:     searcher,
		publisher:    publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *postServiceImpl) SetUserFacade(f UserFacade)                  { s.userFacade = f }
func (s *postServiceImpl) SetCircleFacade(f CircleFacade)              { s.circleFacade = f }
func (s *postServiceImpl) SetMemberChecker(c CircleMemberChecker)      { s.memberCheck = c }
func (s *postServiceImpl) SetStatusChecker(c CircleStatusChecker)      { s.statusCheck = c }
func (s *postServiceImpl) SetPostCountPort(p CirclePostCountPort)      { s.postCountPort = p }
func (s *postServiceImpl) SetCollectCache(c domain.PostCollectCache)   { s.collectCache = c }
func (s *postServiceImpl) SetHistoryRecorder(r domain.HistoryRecorder) { s.historyRec = r }
func (s *postServiceImpl) SetAgentTrigger(t AgentPostTrigger)          { s.agentTrigger = t }

// CreatePost 创建帖子。
//
// 与旧 controller.CreatePost 行为一致（除默认状态外）：
//  1. 默认 type=1(图文)；默认 status=1(已发布)，原为 2(审核中)——见下方 TODO；
//  2. 非草稿校验 circle_id/title 非空；
//  3. 校验成员身份与状态（pending/muted/banned）；
//  4. 校验圈子可用性；
//  5. 清洗 content + 生成 summary；
//  6. 创建帖子 + 递增圈子帖子计数。
func (s *postServiceImpl) CreatePost(ctx context.Context, userID uuid.UUID, input CreatePostInput) (uuid.UUID, error) {
	// 默认值
	postType := input.Type
	if postType == 0 {
		postType = domain.PostTypeTextImage
	}
	postStatus := input.Status
	if postStatus == 0 {
		// TODO(review-flow): 审核流未上线，新帖暂直接置为已发布；
		// 审核能力就绪后恢复为 domain.PostStatusReviewing。
		postStatus = domain.PostStatusPublished
	}

	// 非草稿校验
	if postStatus != domain.PostStatusDraft {
		if input.CircleID == uuid.Nil {
			return uuid.Nil, errCircleIDRequired
		}
		if input.Title == "" {
			return uuid.Nil, errTitleRequired
		}
	}

	// 校验成员身份（仅在非草稿时）
	if postStatus != domain.PostStatusDraft && s.memberCheck != nil {
		member, err := s.memberCheck.GetMember(ctx, input.CircleID, userID)
		if err != nil {
			return uuid.Nil, err
		}
		if member == nil {
			return uuid.Nil, errNotMember
		}
		// 检查成员状态（pending/muted/banned → 拒绝；normal → 放行）
		if err := checkMemberStatus(member); err != nil {
			return uuid.Nil, err
		}
	}

	// 校验圈子可用性
	if postStatus != domain.PostStatusDraft && s.statusCheck != nil {
		available, err := s.statusCheck.IsCircleAvailable(ctx, input.CircleID)
		if err != nil {
			return uuid.Nil, err
		}
		if !available {
			return uuid.Nil, errCircleNotAvailable
		}
	}

	// 清洗 + 生成摘要
	title := utils.SanitizeForPg(strings.TrimSpace(input.Title))
	content := utils.SanitizeForPg(input.Content)
	summary := input.Summary
	if summary == "" && content != "" {
		summary = utils.GenerateSummary(content)
	}
	summary = utils.SanitizeForPg(strings.TrimSpace(summary))
	if r := []rune(summary); len(r) > 2000 {
		summary = string(r[:2000])
	}

	mediaExtra := domain.MediaExtraJSON(input.MediaExtra)
	if mediaExtra == nil {
		mediaExtra = make(domain.MediaExtraJSON, 0)
	}

	post := &domain.Post{
		CircleID:   input.CircleID,
		UserID:     userID,
		Type:       postType,
		Title:      title,
		Summary:    summary,
		Content:    content,
		MediaExtra: mediaExtra,
		Status:     postStatus,
		Deleted:    0,
	}

	if err := s.repo.Create(ctx, post); err != nil {
		return uuid.Nil, err
	}

	// 递增圈子帖子计数（Redis + Redpanda）
	if s.postCountPort != nil {
		if err := s.postCountPort.IncrPostCount(ctx, post.CircleID); err != nil {
			logger.Log.Error("Failed to increment circle post count: " + err.Error())
		}
	}

	// @提及：校验一次得到最终名单 → 落库（不区分状态，草稿正文同样含提及）→
	// 仅已发布按同一名单发通知（草稿/审核中帖子不应对外产生通知）。
	// 落库/通知均为 best-effort，不阻断发帖；通知名单 == 落库名单。
	if mentionIDs := s.filterMentionUserIDs(ctx, userID, input.MentionUserIDs, input.CircleID); len(mentionIDs) > 0 {
		if err := s.repo.CreateMentions(ctx, post.ID, mentionIDs); err != nil {
			logger.Log.Error("Failed to save post mentions: " + err.Error())
		}
		if postStatus == domain.PostStatusPublished && s.publisher != nil {
			if err := s.publisher.PublishMentionNotice(ctx, userID, post.ID, mentionIDs, title); err != nil {
				logger.Log.Error("Failed to publish post mention notice: " + err.Error())
			}
		}
		// AI 机器人触发：与通知同一门槛（仅已发布）、同一名单；
		// 帖子圈子随事件透传（圈子级机器人只在同圈帖触发）；
		// 同步回调立即返回，内部异步执行，不向发帖链路传播错误。
		if postStatus == domain.PostStatusPublished && s.agentTrigger != nil {
			s.agentTrigger.OnPostMentioned(post.ID, post.CircleID, userID, mentionIDs)
		}
	}

	return post.ID, nil
}

// filterMentionUserIDs 校验 @提及 列表：去重 → 去掉自己/Nil → 校验用户存在 →
// 剔除越圈机器人 → 按上限截断。
// 圈子作用域兜底：圈内机器人（UserFacade.GetAgentCircleIDs 命中且绑定圈子 ≠ 本帖圈子）
// 静默剔除——@选人列表已在 ES 侧过滤，此处只防手搓 ID 构造请求。查询失败时 fail-open
// 跳过剔除（与列表过滤的兜底口径一致），不阻断主流程。
// 用户查询失败时降级为 nil（不发通知），不阻断主流程。
func (s *postServiceImpl) filterMentionUserIDs(ctx context.Context, actorID uuid.UUID, raw []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))
	for _, id := range raw {
		if id == uuid.Nil || id == actorID {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	if s.userFacade == nil {
		return nil
	}
	briefs, err := s.userFacade.GetBriefs(ctx, toStrings(ids))
	if err != nil {
		logger.Log.Error("Failed to validate mention users: " + err.Error())
		return nil
	}
	valid := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := briefs[id.String()]; ok {
			valid = append(valid, id)
		}
	}

	valid = s.stripOutOfScopeAgents(ctx, valid, postCircleID)

	max := conf.Config.Notice.MentionMax
	if max <= 0 {
		max = 10
	}
	if len(valid) > max {
		valid = valid[:max]
	}
	return valid
}

// stripOutOfScopeAgents 从 mention 候选中剔除越圈的圈内机器人：绑定圈子 ≠ postCircleID
// 的机器人静默移除；普通用户/全局机器人（map 无记录）保留。查询失败 fail-open 跳过剔除
//（正常用户已被 @选人列表过滤，兜底只防构造请求）。
func (s *postServiceImpl) stripOutOfScopeAgents(ctx context.Context, candidates []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(candidates) == 0 {
		return candidates
	}
	agentCircles, err := s.userFacade.GetAgentCircleIDs(ctx, candidates)
	if err != nil {
		logger.Log.Warn("Failed to load agent circle scope, skip out-of-scope filtering: " + err.Error())
		return candidates
	}
	kept := make([]uuid.UUID, 0, len(candidates))
	for _, id := range candidates {
		if bound, ok := agentCircles[id]; ok && bound != postCircleID {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}

// checkMemberStatus 检查成员状态是否允许发帖。
//
// 与旧 controller.CreatePost 中的成员状态分支一致。
// 注意：member.Status 用 int16，值与 circle.domain.MemberStatus* 常量一致。
func checkMemberStatus(member *CircleMemberInfo) error {
	const (
		statusNormal  = 1
		statusPending = 0
		statusMuted   = 2
		statusBanned  = 3
	)
	if member.Status == statusNormal {
		return nil
	}
	switch member.Status {
	case statusPending:
		return errMembershipPending
	case statusMuted:
		if member.MuteEndTime != nil && member.MuteEndTime.After(time.Now()) {
			return errMutedUntil(*member.MuteEndTime)
		}
		return nil
	case statusBanned:
		return errBannedFromCircle
	}
	return nil
}

// GetPostDetail 获取帖子详情。
func (s *postServiceImpl) GetPostDetail(ctx context.Context, userID, postID uuid.UUID) (*PostDetailVO, error) {
	post, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		return nil, err
	}

	// 权限：作者可看所有状态；其他人只能看已发布
	// 匿名（userID==uuid.Nil）天然走"非作者"分支，只能看已发布帖——符合预期。
	if userID != post.UserID && post.Status != domain.PostStatusPublished {
		return nil, domain.ErrPostNotFound
	}

	// 访客降级：匿名（userID==uuid.Nil）跳过交互态查询与浏览计数，避免：
	//   1. checkLiked/checkCollected 对 uuid.Nil 做无意义 DB/Redis 查询；
	//   2. asyncIncrementView 把 uuid.Nil 写入 user:view:posts:{00000000-...} 污染历史池
	//      + 以 uuid.Nil 作为 IncrViewCount 的去重 key。
	// 登录用户走完整路径。
	var isLiked, isCollected bool
	if userID != uuid.Nil {
		// 点赞状态（缓存优先，miss 回源 DB）
		isLiked = s.checkLiked(ctx, userID, postID)

		// 收藏状态（缓存优先，miss 回源 DB）
		isCollected = s.checkCollected(ctx, userID, postID)

		// 异步增加浏览量（独立 goroutine，需自带 panic 恢复，避免拖垮服务）
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Log.Error(fmt.Sprintf("Panic in asyncIncrementView: %v", r))
				}
			}()
			s.asyncIncrementView(postID, userID)
		}()
	}

	// 查发帖人信息
	var authorName, authorAvatar string
	if s.userFacade != nil {
		if author, err := s.userFacade.GetBrief(ctx, post.UserID.String()); err == nil && author != nil {
			authorName = author.Username
			authorAvatar = author.AvatarURL
		}
	}

	vo := &PostDetailVO{
		ID: post.ID, CircleID: post.CircleID, UserID: post.UserID,
		Type: post.Type, Title: post.Title, Summary: post.Summary,
		Content: post.Content, MediaExtra: post.MediaExtra,
		ViewCount: post.ViewCount, CommentCount: post.CommentCount,
		LikeCount: post.LikeCount, CollectCount: post.CollectCount,
		IsPinned: post.IsPinned, IsEssence: post.IsEssence, IsLock: post.IsLock,
		Status: post.Status, Deleted: post.Deleted,
		CreateTime: post.CreateTime, UpdateTime: post.UpdateTime,
		LastReplyTime: post.LastReplyTime,
		AuthorID:      post.UserID, AuthorName: authorName, AuthorAvatar: authorAvatar,
		IsLiked:     isLiked,
		IsCollected: isCollected,
	}

	// @提及 用户列表（落库名单 → GetBriefs 过滤未注销；失败置空数组，前端回退文本反查）
	vo.Mentions = s.assembleMentions(ctx, postID)

	// 用 Redis 实时统计覆盖 DB 值（全 4 字段）；缓存缺失则用 DB 兜底（值已在 vo）并回种 Redis。
	// 回种用 HSetNX：async 浏览量 goroutine 会 HINCRBY view_count，普通 HSet 会 clobber 它的 +1。
	if stats, _ := s.statsCache.Get(ctx, postID); stats != nil {
		vo.ViewCount = stats.ViewCount
		vo.CommentCount = stats.CommentCount
		vo.LikeCount = stats.LikeCount
		vo.CollectCount = stats.CollectCount
	} else {
		_ = s.statsCache.SetIfAbsent(ctx, postID, &domain.PostStatistics{
			ViewCount: post.ViewCount, CommentCount: post.CommentCount,
			LikeCount: post.LikeCount, CollectCount: post.CollectCount,
		})
	}

	return vo, nil
}

// assembleMentions 组装帖子 @提及 用户列表（详情接口回传）。
//
// 落库名单 → UserFacade 批量取精简视图（内部过滤已注销/已删除用户）→ VO。
// 任一环节失败返回空切片（非 nil，前端按"缺失/空数组回退文本反查"处理，不报错）。
func (s *postServiceImpl) assembleMentions(ctx context.Context, postID uuid.UUID) []MentionVO {
	mentionIDs, err := s.repo.GetMentionUserIDsByPostIDs(ctx, []uuid.UUID{postID})
	if err != nil {
		logger.Log.Error("Failed to get post mentions: " + err.Error())
		return []MentionVO{}
	}
	ids := mentionIDs[postID]
	if len(ids) == 0 || s.userFacade == nil {
		return []MentionVO{}
	}
	briefs, err := s.userFacade.GetBriefs(ctx, toStrings(ids))
	if err != nil {
		logger.Log.Error("Failed to get mention user briefs: " + err.Error())
		return []MentionVO{}
	}
	mentions := make([]MentionVO, 0, len(ids))
	for _, id := range ids {
		if b, ok := briefs[id.String()]; ok {
			mentions = append(mentions, MentionVO{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL})
		}
	}
	return mentions
}

// checkLiked 检查点赞状态（详情回显用，best-effort：出错视为未赞）。
func (s *postServiceImpl) checkLiked(ctx context.Context, userID, postID uuid.UUID) bool {
	liked, err := s.IsLikedByUser(ctx, userID, postID)
	if err != nil {
		logger.Log.Error("Failed to check post liked: " + err.Error())
		return false
	}
	return liked
}

// IsLikedByUser 用户是否已赞该帖（缓存优先，miss 回源 DB，DB 已赞则回填缓存）。
//
// 用户点赞 ZSET 有 TTL 与容量上限，miss 既可能是"未赞"也可能是"缓存已丢"，必须回源 DB 区分。
// 缓存故障直接回源 DB；DB 错误原样返回。
func (s *postServiceImpl) IsLikedByUser(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	likedMap, _, err := s.likeCache.BatchCheck(ctx, userID, []uuid.UUID{postID})
	if err == nil && likedMap[postID] {
		return true, nil
	}
	if err != nil {
		logger.Log.Error("Failed to check post liked from cache, falling back to DB: " + err.Error())
	}

	liked, err := s.repo.IsLiked(ctx, userID, postID)
	if err != nil {
		return false, err
	}
	if liked {
		if bfErr := s.likeCache.Backfill(ctx, userID, []uuid.UUID{postID}); bfErr != nil {
			logger.Log.Error("Failed to backfill post like cache: " + bfErr.Error())
		}
	}
	return liked, nil
}

// checkCollected 检查收藏状态（缓存优先，miss 回源 DB + 回填）。
// 未注入 collectCache 时（向后兼容）直接返回 false。
func (s *postServiceImpl) checkCollected(ctx context.Context, userID, postID uuid.UUID) bool {
	if s.collectCache == nil {
		return false
	}
	collectedMap, missed, err := s.collectCache.BatchCheck(ctx, userID, []uuid.UUID{postID})
	if err == nil {
		if collectedMap[postID] {
			return true
		}
		if len(missed) > 0 {
			isCollected, dbErr := s.repo.IsCollected(ctx, userID, postID)
			if dbErr != nil {
				return false
			}
			if isCollected {
				_ = s.collectCache.Backfill(ctx, userID, []uuid.UUID{postID})
			}
			return isCollected
		}
		return false
	}
	// 缓存故障：直接回源 DB
	isCollected, dbErr := s.repo.IsCollected(ctx, userID, postID)
	if dbErr != nil {
		return false
	}
	return isCollected
}

// asyncIncrementView 异步增加浏览量（含缓存恢复 + 发布事件）。
//
// 与旧 controller.GetPostDetail 中的 go func 行为一致。
func (s *postServiceImpl) asyncIncrementView(postID, userID uuid.UUID) {
	// 缓存恢复（如果统计 Hash 不存在，先从 DB 恢复）
	exists, err := s.statsCache.Exists(context.Background(), postID)
	if err == nil && !exists {
		s.restorePostStats(postID)
	} else if err != nil {
		logger.Log.Error("Failed to check post stats existence: " + err.Error())
	}

	newCount, err := s.statsCache.IncrViewCount(context.Background(), postID, userID)
	if err != nil {
		logger.Log.Error("Failed to increment post view count: " + err.Error())
		return
	}
	if newCount > 0 {
		if err := s.publisher.PublishViewCount(context.Background(), postID); err != nil {
			logger.Log.Error("Failed to publish view count event: " + err.Error())
		}
		// 记录浏览历史(newCount>0 即真实浏览,复用 5min 去重窗口,防刷新刷量)。
		// 失败仅日志,不影响详情接口。
		if s.historyRec != nil {
			if err := s.historyRec.RecordView(context.Background(), userID, postID); err != nil {
				logger.Log.Error("Failed to record post view history: " + err.Error())
			}
		}
	}
}

// restorePostStats 从 DB 恢复帖子统计缓存。
//
// 与旧 controller/comment.go 中的 restorePostStatsIfNeed 行为一致。
// 这里直接读 Post 实体的计数字段写入 Redis。
func (s *postServiceImpl) restorePostStats(postID uuid.UUID) {
	post, err := s.repo.GetByID(context.Background(), postID)
	if err != nil {
		logger.Log.Error("Failed to load post for stats recovery: " + err.Error())
		return
	}
	stats := &domain.PostStatistics{
		ViewCount:    post.ViewCount,
		CommentCount: post.CommentCount,
		LikeCount:    post.LikeCount,
		CollectCount: post.CollectCount,
	}
	if err := s.statsCache.Set(context.Background(), postID, stats); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}
}

// SearchPosts 搜索帖子列表（组装作者/圈子/图片信息）。
func (s *postServiceImpl) SearchPosts(ctx context.Context, keyword string, circleID uuid.UUID, size int, searchAfter []interface{}) (*PostSearchResult, error) {
	raw, err := s.searcher.Search(ctx, keyword, circleID, size, searchAfter)
	if err != nil {
		return nil, err
	}
	posts := s.assemblePostList(ctx, raw)
	return &PostSearchResult{
		Posts: posts, Total: raw.Total, Size: raw.Size, SearchAfter: raw.SearchAfter,
	}, nil
}

// GetMyPosts 查看自己发的帖（按 userID 过滤，支持 title/summary 模糊关键字）。
func (s *postServiceImpl) GetMyPosts(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostSearchResult, error) {
	raw, err := s.searcher.SearchMy(ctx, userID, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	posts := s.assemblePostList(ctx, raw)
	return &PostSearchResult{
		Posts: posts, Total: raw.Total, Size: raw.Size, SearchAfter: raw.SearchAfter,
	}, nil
}

// GetUserPosts 查看任意指定用户的发帖记录（按 targetUserID 过滤，仅已发布 status=1）。
func (s *postServiceImpl) GetUserPosts(ctx context.Context, targetUserID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostSearchResult, error) {
	raw, err := s.searcher.SearchByUser(ctx, targetUserID, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	posts := s.assemblePostList(ctx, raw)
	return &PostSearchResult{
		Posts: posts, Total: raw.Total, Size: raw.Size, SearchAfter: raw.SearchAfter,
	}, nil
}

// assemblePostList 把 ES 原始结果批量组装为 PostListItem（作者/圈子/图片信息）。
//
// 抽出来供 SearchPosts 与 GetMyPosts 共用，避免重复 ~90 行组装代码。
func (s *postServiceImpl) assemblePostList(ctx context.Context, raw *RawPostSearchResult) []PostListItem {
	// 收集 userIDs/circleIDs/postIDs
	userIDSet := make(map[uuid.UUID]struct{})
	circleIDSet := make(map[uuid.UUID]struct{})
	var postIDs []uuid.UUID
	for _, doc := range raw.Posts {
		if pid, err := uuid.Parse(doc.ID); err == nil {
			postIDs = append(postIDs, pid)
		}
		if uid, err := uuid.Parse(doc.UserID); err == nil {
			userIDSet[uid] = struct{}{}
		}
		if cid, err := uuid.Parse(doc.CircleID); err == nil {
			circleIDSet[cid] = struct{}{}
		}
	}
	userIDs := keys(userIDSet)
	circleIDs := keys(circleIDSet)

	// 批量查询用户信息
	userMap := make(map[uuid.UUID]UserBrief)
	if s.userFacade != nil && len(userIDs) > 0 {
		idStrs := toStrings(userIDs)
		if briefs, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			for idStr, b := range briefs {
				if uid, err := uuid.Parse(idStr); err == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询圈子信息
	circleMap := make(map[uuid.UUID]CircleBrief)
	if s.circleFacade != nil && len(circleIDs) > 0 {
		idStrs := toStrings(circleIDs)
		if briefs, err := s.circleFacade.GetBriefs(ctx, idStrs); err == nil {
			for idStr, b := range briefs {
				if cid, err := uuid.Parse(idStr); err == nil {
					circleMap[cid] = CircleBrief{ID: b.ID, Name: b.Name, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询帖子媒体
	mediaMap := make(map[uuid.UUID][]string)
	if len(postIDs) > 0 {
		if media, err := s.repo.GetMediaByPostIDs(ctx, postIDs); err == nil {
			for pid, m := range media {
				mediaMap[pid] = []string(m)
			}
		}
	}

	// 组装
	posts := make([]PostListItem, 0, len(raw.Posts))
	for _, doc := range raw.Posts {
		pid, _ := uuid.Parse(doc.ID)
		uid, _ := uuid.Parse(doc.UserID)
		cid, _ := uuid.Parse(doc.CircleID)

		var authorName, authorAvatar string
		if a, ok := userMap[uid]; ok {
			authorName = a.Username
			authorAvatar = a.AvatarURL
		}
		var circleName, circleAvatar string
		if c, ok := circleMap[cid]; ok {
			circleName = c.Name
			circleAvatar = c.AvatarURL
		}
		createTime, _ := time.Parse(time.RFC3339Nano, doc.CreateTime)
		var images []string
		if m, ok := mediaMap[pid]; ok {
			images = m
		}

		posts = append(posts, PostListItem{
			ID: pid, CircleID: cid, UserID: uid, Type: doc.Type,
			Title: doc.Title, Summary: doc.Summary, Content: doc.Content,
			ViewCount: doc.ViewCount, CommentCount: doc.CommentCount,
			LikeCount: doc.LikeCount, CollectCount: doc.CollectCount,
			IsPinned: doc.IsPinned, IsEssence: doc.IsEssence, IsLock: doc.IsLock,
			Status: doc.Status, CreateTime: createTime,
			AuthorName: authorName, AuthorAvatar: authorAvatar,
			CircleName: circleName, CircleAvatar: circleAvatar,
			Images: images,
		})
	}
	s.applyStatsOverlay(ctx, posts)
	return posts
}

// applyStatsOverlay 用 Redis 实时统计覆盖列表项计数字段（view/comment/like/collect）。
// 批量 pipeline 1 RTT；未命中的帖子保留原值（ES/DB 快照），不回种（避免读路径写放大）。
func (s *postServiceImpl) applyStatsOverlay(ctx context.Context, posts []PostListItem) {
	if len(posts) == 0 {
		return
	}
	postIDs := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
	}
	statsMap, err := s.statsCache.BatchGet(ctx, postIDs)
	if err != nil || len(statsMap) == 0 {
		return
	}
	for i := range posts {
		if st, ok := statsMap[posts[i].ID]; ok && st != nil {
			posts[i].ViewCount = st.ViewCount
			posts[i].CommentCount = st.CommentCount
			posts[i].LikeCount = st.LikeCount
			posts[i].CollectCount = st.CollectCount
		}
	}
}

// GetMediaByPostIDs 批量获取帖子图片（供 circle 领域调用）。
func (s *postServiceImpl) GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}
	ids := make([]uuid.UUID, 0, len(postIDs))
	for _, s := range postIDs {
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	media, err := s.repo.GetMediaByPostIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for pid, m := range media {
		result[pid.String()] = []string(m)
	}
	return result, nil
}

// GetPostsByIDs 按 ID 列表批量获取已组装的帖子（DB 来源，仅未删除 + 已发布）。
//
// 供 collect 领域「我的收藏」列表调用。与 assemblePostList（ES 来源）平行：
// 批量查作者/圈子/图片后组装为 PostListItem。顺序不保证，失效帖静默过滤。
func (s *postServiceImpl) GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]PostListItem, error) {
	if len(postIDs) == 0 {
		return []PostListItem{}, nil
	}
	posts, err := s.repo.ListByIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	return s.assembleFromPosts(ctx, posts), nil
}

// SearchPostsByIDs 按 ID 列表从 ES 获取已组装帖子(供 history「最近浏览」列表调用)。
//
// 与 GetPostsByIDs(DB 来源)平行,但走 ES terms 查询,复用 assemblePostList 组装。
// 顺序不保证,失效帖(被删/未发布)在 ES 查询时静默过滤。
func (s *postServiceImpl) SearchPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]PostListItem, error) {
	if len(postIDs) == 0 {
		return []PostListItem{}, nil
	}
	raw, err := s.searcher.SearchByIDs(ctx, postIDs, len(postIDs))
	if err != nil {
		return nil, err
	}
	return s.assemblePostList(ctx, raw), nil
}

// SearchPostsByIDsAndKeyword 在 ID 集合内按关键字搜索并组装帖子(供 history「最近浏览」关键字搜索)。
//
// 与 SearchPostsByIDs 平行,但叠加 multi_match(title^3/summary) 关键字过滤,
// 按 _score desc 排序,offset 分页;返回匹配总数(raw.Total)供上层计算 next_offset。
// 失效帖(被删/未发布)在 ES 查询时静默过滤。
func (s *postServiceImpl) SearchPostsByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) ([]PostListItem, int64, error) {
	if len(postIDs) == 0 || keyword == "" {
		return []PostListItem{}, 0, nil
	}
	raw, err := s.searcher.SearchByIDsAndKeyword(ctx, postIDs, keyword, size, offset)
	if err != nil {
		return nil, 0, err
	}
	return s.assemblePostList(ctx, raw), raw.Total, nil
}

// ListCircleIDsByPostIDs 批量取帖子所属 circle_id（去重，仅未删除+已发布）。
// 供 recommend 域 C3 行为圈子召回：seed 帖子 → circle_id 集合 → 减 joined → 热门检索。
func (s *postServiceImpl) ListCircleIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(postIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	posts, err := s.repo.ListByIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	seen := make(map[uuid.UUID]struct{}, len(posts))
	out := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		if _, ok := seen[p.CircleID]; ok {
			continue
		}
		seen[p.CircleID] = struct{}{}
		out = append(out, p.CircleID)
	}
	return out, nil
}

// assembleFromPosts 把 DB Post 实体批量组装为 PostListItem（作者/圈子/图片信息）。
//
// 与 assemblePostList 平行：后者喂 ES PostDoc，本方法喂 domain.Post。
// 两者共享 batch 查询 user/circle/media 的模式，但输入类型不同，故独立实现以保证清晰。
func (s *postServiceImpl) assembleFromPosts(ctx context.Context, posts []domain.Post) []PostListItem {
	if len(posts) == 0 {
		return []PostListItem{}
	}

	userIDSet := make(map[uuid.UUID]struct{})
	circleIDSet := make(map[uuid.UUID]struct{})
	postIDs := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
		userIDSet[p.UserID] = struct{}{}
		circleIDSet[p.CircleID] = struct{}{}
	}
	userIDs := keys(userIDSet)
	circleIDs := keys(circleIDSet)

	// 批量查询用户信息
	userMap := make(map[uuid.UUID]UserBrief)
	if s.userFacade != nil && len(userIDs) > 0 {
		if briefs, err := s.userFacade.GetBriefs(ctx, toStrings(userIDs)); err == nil {
			for idStr, b := range briefs {
				if uid, err := uuid.Parse(idStr); err == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询圈子信息
	circleMap := make(map[uuid.UUID]CircleBrief)
	if s.circleFacade != nil && len(circleIDs) > 0 {
		if briefs, err := s.circleFacade.GetBriefs(ctx, toStrings(circleIDs)); err == nil {
			for idStr, b := range briefs {
				if cid, err := uuid.Parse(idStr); err == nil {
					circleMap[cid] = CircleBrief{ID: b.ID, Name: b.Name, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询帖子媒体
	mediaMap := make(map[uuid.UUID][]string)
	if len(postIDs) > 0 {
		if media, err := s.repo.GetMediaByPostIDs(ctx, postIDs); err == nil {
			for pid, m := range media {
				mediaMap[pid] = []string(m)
			}
		}
	}

	// 组装
	result := make([]PostListItem, 0, len(posts))
	for _, p := range posts {
		var authorName, authorAvatar string
		if a, ok := userMap[p.UserID]; ok {
			authorName = a.Username
			authorAvatar = a.AvatarURL
		}
		var circleName, circleAvatar string
		if c, ok := circleMap[p.CircleID]; ok {
			circleName = c.Name
			circleAvatar = c.AvatarURL
		}
		var images []string
		if m, ok := mediaMap[p.ID]; ok {
			images = m
		}

		result = append(result, PostListItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: authorName, AuthorAvatar: authorAvatar,
			CircleName: circleName, CircleAvatar: circleAvatar,
			Images: images,
		})
	}
	s.applyStatsOverlay(ctx, result)
	return result
}

// GetPostMeta 获取帖子元信息（供 comment/like 领域校验用）。
//
// 直接读 Post 实体的 Status / IsLock 字段，不做权限过滤
// （comment/like 调用方自己判断"是否可评论/可点赞"）。
func (s *postServiceImpl) GetPostMeta(ctx context.Context, postID uuid.UUID) (*PostMeta, error) {
	post, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		if err == domain.ErrPostNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &PostMeta{
		ID:       post.ID,
		Status:   post.Status,
		IsLock:   post.IsLock,
		CircleID: post.CircleID,
	}, nil
}

// GetPostBrief 获取帖子内容摘要（供 aiagent 领域组装机器人回复 prompt 用）。
func (s *postServiceImpl) GetPostBrief(ctx context.Context, postID uuid.UUID) (*PostBrief, error) {
	post, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		if err == domain.ErrPostNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &PostBrief{
		ID:       post.ID,
		Title:    post.Title,
		Summary:  post.Summary,
		Status:   post.Status,
		IsLock:   post.IsLock,
		AuthorID: post.UserID,
		CircleID: post.CircleID,
	}, nil
}

// RestoreStatsAndIncrCommentCount 恢复帖子统计缓存（如果不存在），
// 然后递增帖子评论计数（Redis Hash 实时 + DB 同步持久化）。
//
// 评论计数不再走 Redpanda 异步聚合：评论是低频、本就带 DB 事务的写入，
// 同步 UPDATE 比批量聚合更简单且消除 DB 滞后导致的缓存回源少算问题。
// view/like/collect 仍走聚合器（高频，需批量）。
func (s *postServiceImpl) RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	// 1. 缓存恢复（如果统计 Hash 不存在，先从 DB 恢复）
	exists, err := s.statsCache.Exists(ctx, postID)
	if err != nil {
		return err
	}
	if !exists {
		s.restorePostStats(postID)
	}

	// 2. 实时递增帖子评论计数（Redis Hash）
	if err := s.statsCache.IncrCommentCount(ctx, postID); err != nil {
		return err
	}

	// 3. 同步递增 DB 评论计数（实时持久化）
	return s.repo.IncrCommentCount(ctx, postID)
}

// RestoreStats 恢复帖子统计缓存（如果不存在）。
//
// 与旧 controller.restorePostStatsIfNeed 行为一致：检查 stats Hash 是否存在，
// 不存在则从 DB 读取 Post 的计数字段写入 Redis。
// 供 like 领域点赞前确保 stats Hash 存在，避免 Lua 脚本读到空 stats。
func (s *postServiceImpl) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	exists, err := s.statsCache.Exists(ctx, postID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	// restorePostStats 内部用 context.Background()（保持与异步路径一致）
	s.restorePostStats(postID)
	return nil
}

// ===== 辅助函数 =====

func keys(m map[uuid.UUID]struct{}) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func toStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}
