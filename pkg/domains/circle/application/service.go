// Package application 提供 circle 领域的应用服务层。
//
// 职责：
//   - 圈子 CRUD + 加入/退出 + 成员状态校验
//   - 统计计数管理（Redis 实时 + DB 恢复 + Redpanda 异步持久化）
//   - 圈内帖子列表组装（调用 post Facade 与 user Facade）
//   - 通过 CircleFacade 向 post 领域暴露"圈子精简视图"查询
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ===== Facade（跨领域视图）=====

// CircleBrief 是给跨领域调用的圈子精简视图。
type CircleBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// CircleFacade 是 circle 领域给 post 等领域的跨领域查询接口。
type CircleFacade interface {
	// GetBriefs 批量获取圈子精简视图。
	GetBriefs(ctx context.Context, circleIDs []string) (map[string]CircleBrief, error)
}

// ===== 跨领域依赖（post/user Facade，由 composition 注入）=====

// UserBrief 用户精简视图（与 user.application.UserBrief 字段一致，但独立定义避免跨领域 import）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserFacade 是 circle 领域需要的 user 查询接口（由 composition 注入 user 领域实现）。
type UserFacade interface {
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// SearchBriefs 按关键字搜索用户（username 拼写容错 + email 分词匹配），
	// 返回按相关性排序的有序列表（最多 limit 个）与命中总数 total；
	// total > len(列表) 表示按 limit 截断。供成员管理搜索"关键词 → 候选用户集"用。
	SearchBriefs(ctx context.Context, keyword string, limit int) ([]UserBrief, int64, error)
}

// PostMediaFetcher 帖子媒体批量查询接口（由 composition 注入 post 领域实现）。
//
// circle 领域的 GetCirclePosts 需要批量查询帖子图片列表，
// 但帖子实体属于 post 领域。通过此接口解耦。
type PostMediaFetcher interface {
	// GetMediaByPostIDs 批量获取帖子的图片 URL 列表。
	GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error)
}

// ===== 搜索结果 DTO =====

// CircleDoc 圈子搜索结果项（对应 ES CircleDocument）。
type CircleDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description"`
	Hot         int    `json:"hot"`
	CategoryID  string `json:"category_id"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreateTime  string `json:"create_time"`
	Status      int16  `json:"status"`
	JoinType    int16  `json:"join_type"`
}

// CircleSearchResult 圈子列表搜索结果。
type CircleSearchResult struct {
	Circles     []CircleDoc `json:"circles"`
	Total       int64       `json:"total"`
	Size        int         `json:"size"`
	SearchAfter string      `json:"search_after"`
}

// MyCircleDoc 我加入的圈子结果项。
type MyCircleDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	MemberCount int    `json:"member_count"`
	// Role 该用户在圈内的角色（30 圈主 / 20 管理员 / 10 成员）。
	Role int16 `json:"role"`
	// MemberStatus 我的成员状态（1 正常 / 2 禁言）。仅 GET /circle/my 回填；
	// 他人列表不回填（禁言属个人隐私），零值经 omitempty 不出现。
	MemberStatus int16 `json:"member_status,omitempty"`
	// MuteEndTime 禁言截止时间，仅禁言生效中返回（已过期按正常处理，不返回）。
	MuteEndTime *time.Time `json:"mute_end_time,omitempty"`
}

// MyCircleSearchResult 我加入圈子搜索结果。
type MyCircleSearchResult struct {
	Circles     []MyCircleDoc `json:"circles"`
	Total       int64         `json:"total"`
	Size        int           `json:"size"`
	SearchAfter string        `json:"search_after"`
	// Truncated keyword 扫描到上限（joinedSearchMaxScan）仍未集齐 size，
	// 表示可能还有更深的命中未返回。前端可提示「细化关键字」。仅 keyword 模式可能为 true。
	Truncated bool `json:"truncated,omitempty"`
}

// PostDoc 圈内帖子搜索结果项（ES PostDocument 的精简版）。
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

// CirclePostListItem 组装后的圈内帖子项（含作者/圈子信息/图片）。
type CirclePostListItem struct {
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

// CirclePostResult 圈内帖子搜索结果（含组装后的帖子列表，返回给 HTTP 层）。
type CirclePostResult struct {
	Posts       []CirclePostListItem `json:"posts"`
	Total       int64                `json:"total"`
	Size        int                  `json:"size"`
	SearchAfter string               `json:"search_after"`
}

// RawCirclePostResult 是 searcher 返回的原始 ES 结果（未组装）。
type RawCirclePostResult struct {
	Posts       []PostDoc
	Total       int64
	Size        int
	SearchAfter string
}

// ===== 近期活跃圈子 DTO =====

// RawActiveCircleItem searcher 返回的活跃圈子原始项（circle_id + 近期发帖数）。
type RawActiveCircleItem struct {
	CircleID        uuid.UUID
	RecentPostCount int
}

// RawActiveCircleResult searcher 返回的原始活跃圈子聚合结果（未组装明细）。
type RawActiveCircleResult struct {
	Items     []RawActiveCircleItem
	Total     int64 // 活跃圈子近似总数
	Truncated bool  // 是否触达 maxScan 上限
}

// ActiveCircleDoc 近期活跃圈子项（组装明细后，返回 HTTP）。
type ActiveCircleDoc struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	AvatarURL       string    `json:"avatar_url,omitempty"`
	Description     string    `json:"description,omitempty"`
	CategoryID      string    `json:"category_id,omitempty"`
	MemberCount     int       `json:"member_count"`
	PostCount       int       `json:"post_count"`        // 累积（circle.post_count）
	Hot             int       `json:"hot"`               // 累积（circle.hot）
	RecentPostCount int       `json:"recent_post_count"` // 近期活跃信号：窗口内发帖数
	JoinType        int16     `json:"join_type"`
	CreateTime      time.Time `json:"create_time"`
}

// ActiveCircleResult 近期活跃圈子分页结果。
type ActiveCircleResult struct {
	Circles   []ActiveCircleDoc `json:"circles"`
	Total     int64             `json:"total"`
	Size      int               `json:"size"`
	Offset    int               `json:"offset"`
	Truncated bool              `json:"truncated,omitempty"` // 触达 maxScan 上限
}

// ===== 随机圈子 DTO =====

// RawRandomCircleResult searcher 返回的原始随机圈子结果（仅 ID 列表 + 总数，未组装明细）。
type RawRandomCircleResult struct {
	CircleIDs []uuid.UUID
	Total     int64 // 符合条件的圈子总数
}

// ===== 用例输入/输出 DTO =====

// CreateCircleInput 创建圈子入参。
type CreateCircleInput struct {
	Name        string
	Slug        string
	AvatarURL   string
	CoverURL    string
	Description string
	Rule        string
	CategoryID  uuid.UUID
	JoinType    int16
}

// CircleDetailVO 圈子详情（含用户成员信息）。
type CircleDetailVO struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	Hot         int        `json:"hot"`
	MemberCount int        `json:"member_count"`
	PostCount   int        `json:"post_count"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`

	IsJoined          bool       `json:"is_joined"`
	MemberRole        int16      `json:"member_role,omitempty"`
	MemberStatus      int16      `json:"member_status,omitempty"`
	MemberMuteEndTime *time.Time `json:"member_mute_end_time,omitempty"`
	MemberIsTop       int16      `json:"member_is_top,omitempty"`
	MemberIsDisturb   int16      `json:"member_is_disturb,omitempty"`

	// Restricted 私密圈对非成员（含访客）只给精简视图：service 已清空其余字段，
	// handler 按此标志改用 RestrictedView 序列化，不输出简介 / 圈规 / 创建人等。
	Restricted bool `json:"-"`
}

// CircleRestrictedVO 私密圈对非成员的精简详情。
type CircleRestrictedVO struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	JoinType    int16     `json:"join_type"`
	MemberCount int       `json:"member_count"`
	Restricted  bool      `json:"restricted"` // 恒 true：前端据此展示「私密圈，仅成员可见」
}

// RestrictedView 精简视图（仅 Restricted=true 时使用）。
func (vo *CircleDetailVO) RestrictedView() *CircleRestrictedVO {
	return &CircleRestrictedVO{
		ID: vo.ID, Name: vo.Name, AvatarURL: vo.AvatarURL,
		JoinType: vo.JoinType, MemberCount: vo.MemberCount, Restricted: true,
	}
}

// CircleSearcher 圈子搜索抽象（由 infrastructure 提供 ES 实现）。
type CircleSearcher interface {
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error)
	SearchMy(ctx context.Context, circleIDs []uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error)
	SearchCirclePosts(ctx context.Context, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*RawCirclePostResult, error)
	// SearchActive 近期活跃圈子聚合（按窗口内发帖数排序，offset 分页）；excludeCircleIDs 不参与聚合（私密圈）。
	SearchActive(ctx context.Context, size, offset int, excludeCircleIDs []uuid.UUID) (*RawActiveCircleResult, error)
	// SearchRandom 随机圈子查询（random_score 无 seed，每次结果不同；不分页）。
	SearchRandom(ctx context.Context, size int) (*RawRandomCircleResult, error)
}

// CircleService 是 circle 领域的应用服务接口。
type CircleService interface {
	CreateCircle(ctx context.Context, userID uuid.UUID, input CreateCircleInput) error
	GetCircleDetail(ctx context.Context, userID, circleID uuid.UUID) (*CircleDetailVO, error)
	// JoinCircle 自助加入圈子（仅公开圈；私密圈返回 IsPrivateCircleErr，入圈走邀请）。
	JoinCircle(ctx context.Context, userID, circleID uuid.UUID) error
	LeaveCircle(ctx context.Context, userID, circleID uuid.UUID) error
	SearchCircles(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error)
	// GetMyCircles 获取我加入的圈子列表（含我的成员状态）。role 为角色筛选：0 全部，10/20/30 单选。
	GetMyCircles(ctx context.Context, userID uuid.UUID, keyword string, role int16, size int, cursor string) (*MyCircleSearchResult, error)
	// GetUserCircles 获取任意用户加入的圈子列表（查看「他人」加入的圈子）。
	// 逻辑与 GetMyCircles 一致，仅 userID 来源不同（query 参数 vs 当前会话），且不回填成员状态。
	// 私密圈只在查看者本人也是该圈成员时展示（访客 viewerID==uuid.Nil 一律不展示）。
	GetUserCircles(ctx context.Context, viewerID, targetUserID uuid.UUID, keyword string, role int16, size int, cursor string) (*MyCircleSearchResult, error)
	// GetCirclePosts 圈内帖子列表。私密圈对非成员（含访客 viewerID==uuid.Nil）返回 errCircleContentForbidden。
	GetCirclePosts(ctx context.Context, viewerID, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*CirclePostResult, error)
	// ListActiveCircles 近期活跃圈子分页列表（按近 N 天发帖数排序）。
	ListActiveCircles(ctx context.Context, size, offset int) (*ActiveCircleResult, error)
	// ListRandomCircles 随机圈子列表（侧栏推荐；返回格式同 ActiveCircleResult，
	// offset 恒 0、recent_post_count 恒 0）。
	ListRandomCircles(ctx context.Context, size int) (*ActiveCircleResult, error)
	// ListJoinedCircleIDs 用户已加入的圈子 ID 列表（按加入时间倒序，limit 条）。
	// 供 recommend 域 C1 兴趣圈子召回用。ZSET miss 时从 DB 全量重建。
	ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)

	// ===== 圈子管理（owner/admin，权限矩阵见 manage.go）=====

	// ListManagedCircles 列出"我可管理 AI 代理的圈子"（owner/admin，offset 分页）。
	// 登录即可调用：查询本身即权限过滤（WHERE role IN (20,30)），无 ensureAdmin 式门槛；
	// keyword 非空时按圈子 name/description 子串过滤；含非正常状态圈子（UI 按 status 置灰）。
	ListManagedCircles(ctx context.Context, operatorID uuid.UUID, keyword string, page, size int) (*ManagedCircleListResult, error)
	// ListCircleMembers 管理端成员列表（admin+，可见全部状态含拉黑/退出，keyset 分页）。
	// role/status 传 -1 表示不过滤；keyword 非空时按用户名搜索（拼写容错，
	// 最多解析 100 个候选用户，超出时结果 Truncated=true），游标翻页须带同一 keyword。
	ListCircleMembers(ctx context.Context, operatorID, circleID uuid.UUID, role, status int16, keyword, cursor string, size int) (*CircleMemberListResult, error)
	// SetMemberRole 设为/取消管理员（仅圈主；role ∈ {10,20}，转让走 TransferOwner）。
	SetMemberRole(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, role int16) error
	// TransferOwner 转让圈主（仅圈主；目标须为正常状态的非圈主成员）。
	TransferOwner(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error
	// MuteMember 禁言（admin+；durationHours 1-720，仍计数为成员）。
	MuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, durationHours int) error
	// UnmuteMember 解除禁言（admin+）。
	UnmuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error
	// BanMember 拉黑/踢出（admin+；normal/muted → banned，member_count-1）。
	BanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error
	// RemoveMember 移出成员（admin+；normal/muted → left，member_count-1）。不拉黑：
	// 公开圈可再直接加入，私密圈需重新邀请。
	RemoveMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error
	// UnbanMember 解除拉黑（admin+；banned → left；公开圈可再直接加入，私密圈需重新邀请）。
	UnbanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error
	// UpdateCircleProfile 编辑圈子资料（name/slug/join_type/category_id 仅圈主，其余 admin+）。
	UpdateCircleProfile(ctx context.Context, operatorID, circleID uuid.UUID, input UpdateCircleProfileInput) error

	// SetUserFacade 注入 user Facade（GetCirclePosts 组装作者信息用）。
	SetUserFacade(f UserFacade)
	// SetPostFetcher 注入 post 媒体查询器（GetCirclePosts 组装图片用）。
	SetPostFetcher(f PostMediaFetcher)
	// SetAgentCounter 注入圈内 AI 代理计数端口（可管理圈子列表 agent_count 回填用；
	// composition 桥接 aiagent 领域，未注入时降级为 0）。端口见 manage.go。
	SetAgentCounter(c CircleAgentCounter)
	// IncrPostCount 发帖后递增圈子帖子计数（供 post 领域通过端口调用）。
	IncrPostCount(ctx context.Context, circleID uuid.UUID) error
}

type circleServiceImpl struct {
	repo         domain.CircleRepository
	memberRepo   domain.MemberRepository
	baseCache    domain.CircleBaseCache
	statsCache   domain.CircleStatsCache
	joinedCache  domain.JoinedCirclesCache
	searcher     CircleSearcher
	publisher    domain.CircleEventPublisher
	userFacade   UserFacade         // 可为 nil（GetCirclePosts 用）
	postFetcher  PostMediaFetcher   // 可为 nil（GetCirclePosts 用）
	agentCounter CircleAgentCounter // 可为 nil（ListManagedCircles agent_count 降级 0）
	visibility   *circleVisibility  // 私密圈可见性（详情精简 / 圈内帖子 403 / 私密圈集合失效）
}

// NewCircleService 构造 CircleService。
//
// userFacade 和 postFetcher 是可选依赖（仅 GetCirclePosts 需要）。
// 调用方应通过 WithUserFacade / WithPostFetcher 方法注入。
func NewCircleService(
	repo domain.CircleRepository,
	memberRepo domain.MemberRepository,
	baseCache domain.CircleBaseCache,
	statsCache domain.CircleStatsCache,
	joinedCache domain.JoinedCirclesCache,
	searcher CircleSearcher,
	publisher domain.CircleEventPublisher,
	privateCache domain.PrivateCircleCache,
) CircleService {
	return &circleServiceImpl{
		repo:        repo,
		memberRepo:  memberRepo,
		baseCache:   baseCache,
		statsCache:  statsCache,
		joinedCache: joinedCache,
		searcher:    searcher,
		publisher:   publisher,
		visibility:  newCircleVisibility(repo, memberRepo, privateCache),
	}
}

// NewCircleFacade 从 CircleService 构造 CircleFacade。
func NewCircleFacade(repo domain.CircleRepository) CircleFacade {
	return &circleFacadeAdapter{repo: repo}
}

// circleFacadeAdapter 把 CircleRepository 适配为 CircleFacade。
type circleFacadeAdapter struct {
	repo domain.CircleRepository
}

// GetBriefs 批量获取圈子精简视图。
func (f *circleFacadeAdapter) GetBriefs(ctx context.Context, circleIDs []string) (map[string]CircleBrief, error) {
	if len(circleIDs) == 0 {
		return make(map[string]CircleBrief), nil
	}
	ids := make([]uuid.UUID, 0, len(circleIDs))
	for _, s := range circleIDs {
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return make(map[string]CircleBrief), nil
	}
	circles, err := f.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]CircleBrief, len(circles))
	for id, c := range circles {
		result[id.String()] = CircleBrief{
			ID:        c.ID.String(),
			Name:      c.Name,
			AvatarURL: c.AvatarURL,
		}
	}
	return result, nil
}

// SetUserFacade 注入 user Facade（composition 层在装配后调用）。
func (s *circleServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }

// SetPostFetcher 注入 post 媒体查询器。
func (s *circleServiceImpl) SetPostFetcher(f PostMediaFetcher) { s.postFetcher = f }

// SetAgentCounter 注入圈内 AI 代理计数端口（composition 层在装配后调用）。
func (s *circleServiceImpl) SetAgentCounter(c CircleAgentCounter) { s.agentCounter = c }

// validJoinType 可写入的加入方式：0 公开 / 2 私密。1（需审核）已下线，创建 / 编辑都拒绝。
func validJoinType(t int16) bool {
	return t == domain.CircleJoinTypeDirect || t == domain.CircleJoinTypePrivate
}

// CreateCircle 创建圈子。
func (s *circleServiceImpl) CreateCircle(ctx context.Context, userID uuid.UUID, input CreateCircleInput) error {
	// 校验 join_type（只接受 0 公开 / 2 私密；1 需审核已下线）
	if !validJoinType(input.JoinType) {
		return errInvalidJoinType
	}

	// 检查名称是否已存在
	exists, err := s.repo.ExistsByName(ctx, input.Name)
	if err != nil {
		return err
	}
	if exists {
		return errCircleNameExists
	}

	// 检查 slug 是否已存在
	if input.Slug != "" {
		slug := strings.TrimSpace(input.Slug)
		exists, err := s.repo.ExistsBySlug(ctx, slug)
		if err != nil {
			return err
		}
		if exists {
			return errCircleSlugExists
		}
	}

	categoryID := input.CategoryID
	circle := &domain.Circle{
		Name:        strings.TrimSpace(input.Name),
		Slug:        strings.TrimSpace(input.Slug),
		AvatarURL:   input.AvatarURL,
		CoverURL:    input.CoverURL,
		Description: strings.TrimSpace(input.Description),
		Rule:        strings.TrimSpace(input.Rule),
		CreatorID:   userID,
		CategoryID:  &categoryID,
		Hot:         0,
		MemberCount: 1,
		PostCount:   0,
		JoinType:    input.JoinType,
		Status:      domain.CircleStatusNormal,
		Deleted:     0,
	}

	if err := s.repo.Create(ctx, circle); err != nil {
		return err
	}

	// repo.Create 在事务内把创建者设为圈主成员（status=normal），增量写 ZSET。
	s.tryAddJoined(ctx, userID, circle.ID, time.Now().UnixMilli())
	if circle.JoinType == domain.CircleJoinTypePrivate {
		s.visibility.invalidatePrivateIDs(ctx)
	}
	return nil
}

// GetCircleDetail 获取圈子详情（缓存优先 + 统计 + 成员信息）。
func (s *circleServiceImpl) GetCircleDetail(ctx context.Context, userID, circleID uuid.UUID) (*CircleDetailVO, error) {
	// 1. 基础信息缓存优先
	base, _ := s.baseCache.GetBase(ctx, circleID)
	if base == nil {
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			return nil, err
		}
		base = &domain.CircleBaseInfo{
			ID: circle.ID, Name: circle.Name, Slug: circle.Slug,
			AvatarURL: circle.AvatarURL, CoverURL: circle.CoverURL,
			Description: circle.Description, Rule: circle.Rule,
			CreatorID: circle.CreatorID, CategoryID: circle.CategoryID,
			JoinType: circle.JoinType, Status: circle.Status, Deleted: circle.Deleted,
			CreateTime: circle.CreateTime, UpdateTime: circle.UpdateTime,
		}
		if err := s.baseCache.SetBase(ctx, circleID, base); err != nil {
			logger.Log.Error("Failed to cache circle base info: " + err.Error())
		}
	}

	// 2. 统计信息（缓存 + DB 恢复）
	memberCount, postCount, hot := s.getStatsWithRecovery(ctx, circleID)

	// 3. 成员信息
	vo := &CircleDetailVO{
		ID: base.ID, Name: base.Name, Slug: base.Slug,
		AvatarURL: base.AvatarURL, CoverURL: base.CoverURL,
		Description: base.Description, Rule: base.Rule,
		CreatorID: base.CreatorID, CategoryID: base.CategoryID,
		Hot: hot, MemberCount: memberCount, PostCount: postCount,
		JoinType: base.JoinType, Status: base.Status, Deleted: base.Deleted,
		CreateTime: base.CreateTime, UpdateTime: base.UpdateTime,
		IsJoined: false,
	}

	member, err := s.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil && err != domain.ErrMemberNotFound {
		return nil, err
	}
	// 私密圈对非成员（访客 / 申请中 / 已退出 / 被移出）只给精简视图。
	if base.JoinType == domain.CircleJoinTypePrivate && (member == nil || !domain.IsJoinedStatus(member.Status)) {
		return &CircleDetailVO{
			ID: base.ID, Name: base.Name, AvatarURL: base.AvatarURL,
			JoinType: base.JoinType, MemberCount: memberCount, Restricted: true,
		}, nil
	}
	if member != nil {
		vo.IsJoined = true
		vo.MemberRole = member.Role
		vo.MemberStatus = member.Status
		vo.MemberMuteEndTime = member.MuteEndTime
		vo.MemberIsTop = member.IsTop
		vo.MemberIsDisturb = member.IsDisturb
	}

	return vo, nil
}

// JoinCircle 加入圈子（自助加入，仅公开圈；状态机见 memberRepo.JoinCircle）。
// 私密圈只能经邀请入圈，此处返回 errPrivateCircle 且不改数据。
func (s *circleServiceImpl) JoinCircle(ctx context.Context, userID, circleID uuid.UUID) error {
	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		return err
	}
	if circle.Status != domain.CircleStatusNormal {
		return errCircleNotAvailable
	}

	member, err := s.memberRepo.JoinCircle(ctx, circleID, userID, circle.JoinType)
	if err != nil {
		return mapJoinLeaveError(err)
	}

	// 加入成功（repo 只会返回 status=normal）：更新计数缓存 + 发布事件 + 写 joined ZSET。
	if err := s.incrMemberCountWithRecovery(ctx, circleID); err != nil {
		logger.Log.Error("Failed to update Redis member count: " + err.Error())
	}
	if err := s.publisher.PublishMemberCount(ctx, circleID, 1); err != nil {
		logger.Log.Error("Failed to publish join message: " + err.Error())
	}
	s.tryAddJoined(ctx, userID, circleID, member.CreateTime.UnixMilli())
	return nil
}

// LeaveCircle 退出圈子。
func (s *circleServiceImpl) LeaveCircle(ctx context.Context, userID, circleID uuid.UUID) error {
	// 检查圈子存在
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}

	// 检查成员状态
	member, err := s.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil {
		return err
	}

	if err := s.memberRepo.LeaveCircle(ctx, circleID, userID); err != nil {
		return mapJoinLeaveError(err)
	}

	if member.Status == domain.MemberStatusNormal {
		if err := s.decrMemberCountWithRecovery(ctx, circleID); err != nil {
			logger.Log.Error("Failed to update Redis member count: " + err.Error())
		}
		if err := s.publisher.PublishMemberCount(ctx, circleID, -1); err != nil {
			logger.Log.Error("Failed to publish leave message: " + err.Error())
		}
		if err := s.joinedCache.Remove(ctx, userID, circleID); err != nil {
			logger.Log.Error("Failed to remove joined circle from cache: " + err.Error())
		}
	}
	return nil
}

// SearchCircles 搜索圈子列表。
func (s *circleServiceImpl) SearchCircles(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error) {
	return s.searcher.Search(ctx, keyword, size, searchAfter)
}

// ListActiveCircles 近期活跃圈子分页列表（按近 N 天发帖数排序）。
//
// 1. searcher.SearchActive 在 post 索引聚合得 ranked circleIDs + 近期发帖数；
// 2. repo.GetByIDs 批量取明细（name/counts/hot），保留聚合顺序；
// 3. 跳过已删除（GetByIDs 已过滤 deleted=0）与非正常状态圈子。
// 私密圈在聚合阶段排除（与 /circle/list、/circle/random 一致，不在公开榜单暴露私圈）。
func (s *circleServiceImpl) ListActiveCircles(ctx context.Context, size, offset int) (*ActiveCircleResult, error) {
	privateIDs, err := s.visibility.privateIDs(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := s.searcher.SearchActive(ctx, size, offset, privateIDs)
	if err != nil {
		return nil, err
	}

	if len(raw.Items) == 0 {
		return &ActiveCircleResult{Total: raw.Total, Size: size, Offset: offset, Truncated: raw.Truncated}, nil
	}

	ids := make([]uuid.UUID, 0, len(raw.Items))
	recent := make(map[uuid.UUID]int, len(raw.Items))
	for _, it := range raw.Items {
		ids = append(ids, it.CircleID)
		recent[it.CircleID] = it.RecentPostCount
	}

	circles, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	docs := make([]ActiveCircleDoc, 0, len(ids))
	for _, id := range ids {
		c, ok := circles[id]
		if !ok {
			continue // 已删除（GetByIDs 已过滤 deleted=0）
		}
		if c.Status != domain.CircleStatusNormal {
			continue // 非正常状态圈子不上活跃榜
		}
		categoryID := ""
		if c.CategoryID != nil {
			categoryID = c.CategoryID.String()
		}
		docs = append(docs, ActiveCircleDoc{
			ID:              c.ID.String(),
			Name:            c.Name,
			AvatarURL:       c.AvatarURL,
			Description:     c.Description,
			CategoryID:      categoryID,
			MemberCount:     c.MemberCount,
			PostCount:       c.PostCount,
			Hot:             c.Hot,
			RecentPostCount: recent[id],
			JoinType:        c.JoinType,
			CreateTime:      c.CreateTime,
		})
	}

	return &ActiveCircleResult{
		Circles:   docs,
		Total:     raw.Total,
		Size:      size,
		Offset:    offset,
		Truncated: raw.Truncated,
	}, nil
}

// ListRandomCircles 随机圈子列表（侧栏推荐）。
//
// 1. searcher.SearchRandom 在 circle 索引 random_score 随机取 size 个 circleID；
// 2. repo.GetByIDs 批量取明细（name/counts/hot 以 DB 为准），保留随机顺序；
// 3. 跳过已删除（GetByIDs 已过滤 deleted=0）与非正常状态圈子。
// 返回格式同 ActiveCircleResult；recent_post_count 无随机语义，恒 0。
func (s *circleServiceImpl) ListRandomCircles(ctx context.Context, size int) (*ActiveCircleResult, error) {
	raw, err := s.searcher.SearchRandom(ctx, size)
	if err != nil {
		return nil, err
	}

	if len(raw.CircleIDs) == 0 {
		return &ActiveCircleResult{Total: raw.Total, Size: size}, nil
	}

	circles, err := s.repo.GetByIDs(ctx, raw.CircleIDs)
	if err != nil {
		return nil, err
	}

	docs := make([]ActiveCircleDoc, 0, len(raw.CircleIDs))
	for _, id := range raw.CircleIDs {
		c, ok := circles[id]
		if !ok {
			continue // 已删除（GetByIDs 已过滤 deleted=0）
		}
		if c.Status != domain.CircleStatusNormal {
			continue // 非正常状态圈子不上随机推荐
		}
		categoryID := ""
		if c.CategoryID != nil {
			categoryID = c.CategoryID.String()
		}
		docs = append(docs, ActiveCircleDoc{
			ID:          c.ID.String(),
			Name:        c.Name,
			AvatarURL:   c.AvatarURL,
			Description: c.Description,
			CategoryID:  categoryID,
			MemberCount: c.MemberCount,
			PostCount:   c.PostCount,
			Hot:         c.Hot,
			JoinType:    c.JoinType,
			CreateTime:  c.CreateTime,
		})
	}

	return &ActiveCircleResult{
		Circles: docs,
		Total:   raw.Total,
		Size:    size,
	}, nil
}

// joinedSearchBatchSize keyword 模式每批喂 ES 的 ID 数（对齐 ES SearchMy 的 size 上限 100）。
const joinedSearchBatchSize = 100

// joinedSearchMaxScan keyword 模式单次请求最大扫描 ID 数，防全量扫撑爆延迟。
// 超限置 Truncated=true，前端提示细化关键字。
const joinedSearchMaxScan = 5000

// errInvalidCursor 游标非法（非 base64 / 负 rank / 与 role 参数不匹配）。
var errInvalidCursor = errors.New("invalid search_after cursor")

// IsInvalidCursorErr 判断是否游标非法错误。
func IsInvalidCursorErr(err error) bool { return errors.Is(err, errInvalidCursor) }

// GetMyCircles 获取我加入的圈子列表（回填我的成员状态）。
func (s *circleServiceImpl) GetMyCircles(ctx context.Context, userID uuid.UUID, keyword string, role int16, size int, cursor string) (*MyCircleSearchResult, error) {
	return s.loadJoinedCircles(ctx, joinedListQuery{
		userID: userID, keyword: keyword, role: role, size: size, cursor: cursor, withStatus: true,
	})
}

// GetUserCircles 获取任意用户加入的圈子列表（查看「他人」加入的圈子；不回填成员状态）。
//
// 私密圈按查看者过滤（底层 ZSET / 游标不变，被过滤的页可能不足 size 条）。
func (s *circleServiceImpl) GetUserCircles(ctx context.Context, viewerID, targetUserID uuid.UUID, keyword string, role int16, size int, cursor string) (*MyCircleSearchResult, error) {
	res, err := s.loadJoinedCircles(ctx, joinedListQuery{
		userID: targetUserID, keyword: keyword, role: role, size: size, cursor: cursor,
	})
	if err != nil || len(res.Circles) == 0 {
		return res, err
	}
	hidden, err := s.visibility.HiddenCircleIDs(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	if len(hidden) == 0 {
		return res, nil
	}
	hiddenSet := make(map[string]struct{}, len(hidden))
	for _, id := range hidden {
		hiddenSet[id.String()] = struct{}{}
	}
	visible := res.Circles[:0]
	for _, c := range res.Circles {
		if _, ok := hiddenSet[c.ID]; !ok {
			visible = append(visible, c)
		}
	}
	res.Circles = visible
	return res, nil
}

// loadJoinedCircles 加载指定用户加入的圈子列表（GetMyCircles / GetUserCircles 共用）。
//
// 无角色筛选时数据源为用户 joined ZSET（member=circle_id, score=加入时间ms，倒序），
// 支持无上限成员数：永不物化全量 ID 列表，按 rank 游标分页/批量。
//   - 浏览（keyword 空）：ZSET 取一页 ID → ES SearchMy 过滤 status=1/deleted=0 并 hydrate
//   - 搜索（keyword 非空）：ZSET 按 rank 批量取 ID → 每批喂 ES 过滤 → 累积到 size
//
// ZSET miss 时从 DB 全量重建（ensureJoinedWarm）。查「他人」会回填对方的 ZSET。
// 带角色筛选时 ZSET 不含角色，改走 PG keyset（见 joined.go loadJoinedByRole）。
// 两种模式都按页一次批量查 circle_member 回填 role / 成员状态。
func (s *circleServiceImpl) loadJoinedCircles(ctx context.Context, q joinedListQuery) (*MyCircleSearchResult, error) {
	if !isJoinedRoleFilter(q.role) {
		return nil, errInvalidJoinedRoleFilter
	}
	cur, err := decodeJoinedCursor(q.cursor)
	if err != nil || (q.cursor != "" && cur.role != q.role) {
		// rank 游标与角色游标不可混用：切换筛选条件须从第一页开始。
		return nil, errInvalidCursor
	}
	if q.role != 0 {
		return s.loadJoinedByRole(ctx, q, cur.key)
	}

	rank := cur.rank
	if err := s.ensureJoinedWarm(ctx, q.userID); err != nil {
		return nil, err
	}
	card, err := s.joinedCache.Card(ctx, q.userID)
	if err != nil {
		return nil, err
	}
	if card == 0 || int64(rank) >= card {
		return &MyCircleSearchResult{
			Circles: []MyCircleDoc{}, Total: card, Size: q.size, SearchAfter: "",
		}, nil
	}
	var res *MyCircleSearchResult
	if q.keyword == "" {
		res, err = s.browseJoinedByRank(ctx, q.userID, q.size, rank, card)
	} else {
		res, err = s.searchJoinedByRank(ctx, q.userID, q.keyword, q.size, rank, card)
	}
	if err != nil {
		return nil, err
	}
	if res.Circles, err = s.fillJoinedMembership(ctx, q, res.Circles); err != nil {
		return nil, err
	}
	return res, nil
}

// browseJoinedByRank 浏览模式：ZSET 取一页 ID，复用 ES SearchMy 过滤+hydrate。
func (s *circleServiceImpl) browseJoinedByRank(ctx context.Context, userID uuid.UUID, size, rank int, card int64) (*MyCircleSearchResult, error) {
	ids, err := s.joinedCache.PageByRank(ctx, userID, int64(rank), int64(size))
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &MyCircleSearchResult{Circles: []MyCircleDoc{}, Total: card, Size: size, SearchAfter: ""}, nil
	}
	res, err := s.searcher.SearchMy(ctx, ids, "", size, nil)
	if err != nil {
		return nil, err
	}
	next := ""
	if int64(rank+size) < card {
		next = encodeJoinedCursor(rank + size)
	}
	return &MyCircleSearchResult{
		Circles: res.Circles, Total: card, Size: size, SearchAfter: next,
	}, nil
}

// searchJoinedByRank 搜索模式：ZSET 按 rank 批量取 ID → 每批 ES 过滤 → 累积到 size。
// 扫满 joinedSearchMaxScan 仍未集齐 → Truncated=true。
func (s *circleServiceImpl) searchJoinedByRank(ctx context.Context, userID uuid.UUID, keyword string, size, rank int, card int64) (*MyCircleSearchResult, error) {
	collected := make([]MyCircleDoc, 0, size)
	cur := int64(rank)
	hitMaxScan := false

	for len(collected) < size {
		scannedSoFar := cur - int64(rank)
		if scannedSoFar >= int64(joinedSearchMaxScan) {
			hitMaxScan = true
			break
		}
		batch := int64(joinedSearchBatchSize)
		if scannedSoFar+batch > int64(joinedSearchMaxScan) {
			batch = int64(joinedSearchMaxScan) - scannedSoFar
		}
		ids, err := s.joinedCache.PageByRank(ctx, userID, cur, batch)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break // ZSET 扫完
		}
		res, err := s.searcher.SearchMy(ctx, ids, keyword, len(ids), nil)
		if err != nil {
			return nil, err
		}
		// 按 rank 顺序（加入时间倒序）排列命中；批内超出 size 则截断，游标落在最后返回命中的下一位。
		docByID := make(map[string]MyCircleDoc, len(res.Circles))
		for _, d := range res.Circles {
			docByID[d.ID] = d
		}
		need := size - len(collected)
		truncated := false
		for i, id := range ids {
			d, ok := docByID[id.String()]
			if !ok {
				continue
			}
			collected = append(collected, d)
			need--
			if need == 0 {
				cur += int64(i + 1)
				truncated = true
				break
			}
		}
		if truncated {
			break
		}
		cur += int64(len(ids))
		if int64(len(ids)) < batch {
			break // 到末尾
		}
	}

	scannedAll := cur >= card
	next := ""
	if !scannedAll {
		next = encodeJoinedCursor(int(cur))
	}
	return &MyCircleSearchResult{
		Circles: collected, Total: 0, Size: size, SearchAfter: next,
		Truncated: hitMaxScan && !scannedAll,
	}, nil
}

// ensureJoinedWarm ZSET miss 时从 DB 全量重建（一次性，后续走增量 Add/Remove）。
func (s *circleServiceImpl) ensureJoinedWarm(ctx context.Context, userID uuid.UUID) error {
	exists, err := s.joinedCache.Exists(ctx, userID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	members, err := s.memberRepo.ListJoinedWithScore(ctx, userID, 0)
	if err != nil {
		return err
	}
	if err := s.joinedCache.Rebuild(ctx, userID, members); err != nil {
		// 不阻断读路径（Card 会返 0 → 空结果），下次访问再重建。
		logger.Log.Error("Failed to rebuild joined circles cache: " + err.Error())
	}
	return nil
}

// ListJoinedCircleIDs 用户已加入的圈子 ID 列表（按加入时间倒序，limit 条）。
// 供 recommend 域 C1 兴趣圈子召回用。ZSET miss 时 ensureJoinedWarm 从 DB 重建。
func (s *circleServiceImpl) ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	if err := s.ensureJoinedWarm(ctx, userID); err != nil {
		return nil, err
	}
	return s.joinedCache.PageByRank(ctx, userID, 0, int64(limit))
}

// tryAddJoined 增量加圈到 ZSET。仅当 ZSET 已 warm 时写入；
// 冷 key 不创建残缺 ZSET（读路径 ensureJoinedWarm 会从 DB，含本次 join，重建）。
func (s *circleServiceImpl) tryAddJoined(ctx context.Context, userID, circleID uuid.UUID, scoreMs int64) {
	exists, err := s.joinedCache.Exists(ctx, userID)
	if err != nil || !exists {
		return
	}
	if err := s.joinedCache.Add(ctx, userID, circleID, scoreMs); err != nil {
		logger.Log.Error("Failed to add joined circle to cache: " + err.Error())
	}
}

// GetCirclePosts 获取圈内帖子列表（组装作者/圈子/图片信息）。
func (s *circleServiceImpl) GetCirclePosts(ctx context.Context, viewerID, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*CirclePostResult, error) {
	visible, err := s.visibility.CanView(ctx, viewerID, circleID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, errCircleContentForbidden
	}
	result, err := s.searcher.SearchCirclePosts(ctx, circleID, sortType, size, searchAfter)
	if err != nil {
		return nil, err
	}

	// 收集 userIDs 和 postIDs
	userIDSet := make(map[uuid.UUID]struct{})
	var postIDs []uuid.UUID
	for _, doc := range result.Posts {
		if postID, err := uuid.Parse(doc.ID); err == nil {
			postIDs = append(postIDs, postID)
		}
		if uid, err := uuid.Parse(doc.UserID); err == nil {
			userIDSet[uid] = struct{}{}
		}
	}
	userIDs := make([]uuid.UUID, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}

	// 圈子信息（所有帖子同一圈子）
	var circleName, circleAvatar string
	if circle, err := s.repo.GetByID(ctx, circleID); err == nil {
		circleName = circle.Name
		circleAvatar = circle.AvatarURL
	}

	// 批量查询用户信息（通过 UserFacade）
	userMap := make(map[uuid.UUID]UserBrief)
	if s.userFacade != nil && len(userIDs) > 0 {
		idStrs := make([]string, 0, len(userIDs))
		for _, id := range userIDs {
			idStrs = append(idStrs, id.String())
		}
		if briefs, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			for idStr, b := range briefs {
				if uid, err := uuid.Parse(idStr); err == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询帖子媒体（通过 PostMediaFetcher）
	mediaMap := make(map[string][]string)
	if s.postFetcher != nil && len(postIDs) > 0 {
		idStrs := make([]string, 0, len(postIDs))
		for _, id := range postIDs {
			idStrs = append(idStrs, id.String())
		}
		if media, err := s.postFetcher.GetMediaByPostIDs(ctx, idStrs); err == nil {
			mediaMap = media
		}
	}

	// 组装帖子列表
	posts := make([]CirclePostListItem, 0, len(result.Posts))
	for _, doc := range result.Posts {
		postID, _ := uuid.Parse(doc.ID)
		uid, _ := uuid.Parse(doc.UserID)
		cid, _ := uuid.Parse(doc.CircleID)

		var authorName, authorAvatar string
		if author, ok := userMap[uid]; ok {
			authorName = author.Username
			authorAvatar = author.AvatarURL
		}

		createTime, _ := time.Parse(time.RFC3339Nano, doc.CreateTime)

		var images []string
		if media, ok := mediaMap[doc.ID]; ok {
			images = media
		}

		posts = append(posts, CirclePostListItem{
			ID: postID, CircleID: cid, UserID: uid, Type: doc.Type,
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

	return &CirclePostResult{
		Posts: posts, Total: result.Total, Size: result.Size, SearchAfter: result.SearchAfter,
	}, nil
}

// getStatsWithRecovery 获取统计信息，缓存不存在时从 DB 恢复。
//
// 与旧 controller getCircleStatistics / restoreAllCounters 行为一致。
func (s *circleServiceImpl) getStatsWithRecovery(ctx context.Context, circleID uuid.UUID) (memberCount, postCount, hot int) {
	stats, err := s.statsCache.GetStats(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to get circle statistics: " + err.Error())
		return 0, 0, 0
	}
	if stats != nil {
		return stats.MemberCount, stats.PostCount, stats.Hot
	}

	// 缓存未命中，从 DB 恢复
	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		return 0, 0, 0
	}
	restored := &domain.CircleStatistics{
		MemberCount: circle.MemberCount,
		PostCount:   circle.PostCount,
		Hot:         circle.Hot,
	}
	if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
		logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
	}
	logger.Log.Debug("Restored circle statistics cache",
		zap.String("circle_id", circleID.String()),
		zap.Int("member", circle.MemberCount),
		zap.Int("post", circle.PostCount),
		zap.Int("hot", circle.Hot))
	return circle.MemberCount, circle.PostCount, circle.Hot
}

// incrMemberCountWithRecovery 递增成员计数（缓存不存在时先从 DB 恢复）。
func (s *circleServiceImpl) incrMemberCountWithRecovery(ctx context.Context, circleID uuid.UUID) error {
	exists, err := s.statsCache.StatsExists(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
	}
	if !exists {
		// 从 DB 恢复
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		} else {
			restored := &domain.CircleStatistics{
				MemberCount: circle.MemberCount,
				PostCount:   circle.PostCount,
				Hot:         circle.Hot,
			}
			if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}
	return s.statsCache.IncrMemberCount(ctx, circleID)
}

// decrMemberCountWithRecovery 递减成员计数（缓存不存在时先从 DB 恢复）。
func (s *circleServiceImpl) decrMemberCountWithRecovery(ctx context.Context, circleID uuid.UUID) error {
	exists, err := s.statsCache.StatsExists(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
	}
	if !exists {
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		} else {
			restored := &domain.CircleStatistics{
				MemberCount: circle.MemberCount,
				PostCount:   circle.PostCount,
				Hot:         circle.Hot,
			}
			if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}
	return s.statsCache.DecrMemberCount(ctx, circleID)
}

// IncrPostCount 发帖后递增圈子帖子计数（Redis 实时 + Redpanda 异步持久化）。
// 供 post 领域通过 CirclePostCountPort 调用。
func (s *circleServiceImpl) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	if err := s.statsCache.IncrPostCount(ctx, circleID); err != nil {
		return err
	}
	return s.publisher.PublishPostCount(ctx, circleID)
}
