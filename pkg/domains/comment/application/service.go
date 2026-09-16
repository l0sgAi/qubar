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
	"errors"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/comment/domain"
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

// UserFacade comment 领域需要的 user 查询接口。
type UserFacade interface {
	// GetBriefs 批量获取用户精简视图。未找到的用户不会出现在 map 里。
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// GetBrief 获取单个用户精简视图。未找到返回 nil, nil。
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
	// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（仅含圈内机器人行，
	// 普通用户/全局机器人不出现在 map）。mention 兜底剔除越圈机器人用。
	GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}

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
	// 然后递增帖子评论计数（Redis Hash 实时 + DB 同步持久化）。
	RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error
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
	LocateComment(ctx context.Context, commentID uuid.UUID, sort, replySort int) (*CommentLocateResult, error)
	// GetCommentDetail 获取单条评论详情。
	GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error)

	// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
	// 返回评论所属帖子ID。未找到返回 nil, nil。
	GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error)
	// RestoreCommentStats 恢复评论统计缓存（如果不存在）。
	// 供 like 领域点赞前确保 Redis stats Hash 存在。
	RestoreCommentStats(ctx context.Context, commentID uuid.UUID) error

	// SetUserFacade 注入 user Facade（组装评论者/被回复人信息用）。
	SetUserFacade(f UserFacade)
	// SetPostLookup 注入帖子查询端口（发评论校验 + 帖子评论计数用）。
	SetPostLookup(p PostLookup)
	// SetAgentTrigger 注入 AI 机器人回复触发端口（评论创建后回调）。
	SetAgentTrigger(t AgentReplyTrigger)
}

type commentServiceImpl struct {
	repo         domain.CommentRepository
	statsCache   domain.CommentStatsCache
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
	statsCache domain.CommentStatsCache,
	likeCache domain.CommentLikeCache,
	publisher domain.CommentEventPublisher,
) CommentService {
	return &commentServiceImpl{
		repo:       repo,
		statsCache: statsCache,
		likeCache:  likeCache,
		publisher:  publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *commentServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }
func (s *commentServiceImpl) SetPostLookup(p PostLookup) { s.postLookup = p }

// SetAgentTrigger 注入 AI 机器人回复触发端口（未注入则评论创建不触发机器人）。
func (s *commentServiceImpl) SetAgentTrigger(t AgentReplyTrigger) { s.agentTrigger = t }

// CreateComment 发评论（支持顶层评论和回复）。
//
// 与旧 controller.CreateComment 行为一致：
//  1. 校验帖子存在 + 状态(已发布) + 未锁定；
//  2. 如果是回复，校验 root_id 属于同一帖子，校验 reply_to_id 在同一楼层，获取被回复用户ID；
//  3. 清洗 content（SanitizeForPg，剔除 NULL 字节等非法 UTF8）；
//  4. 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）；
//  5. 实时递增帖子评论计数（Redis Hash + DB 同步持久化）。
func (s *commentServiceImpl) CreateComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput) (uuid.UUID, error) {
	// 1. 校验帖子
	if s.postLookup == nil {
		return uuid.Nil, errors.New("post lookup is not configured")
	}
	post, err := s.postLookup.GetPost(ctx, input.PostID)
	if err != nil {
		return uuid.Nil, err
	}
	if post == nil {
		return uuid.Nil, domain.ErrPostNotFound
	}
	// 帖子状态必须为"已发布"才能评论（PostStatusPublished=1）
	if post.Status != 1 {
		return uuid.Nil, domain.ErrPostNotCommentable
	}
	if post.IsLock == 1 {
		return uuid.Nil, domain.ErrPostLocked
	}

	// 2. 如果是回复，校验 root_id 和 reply_to_id，并获取被回复用户ID
	var replyToUserID *uuid.UUID
	if input.RootID != nil {
		rootID := *input.RootID
		// 校验根评论存在且属于同一帖子
		rootComment, err := s.repo.GetByID(ctx, rootID)
		if err != nil {
			if errors.Is(err, domain.ErrCommentNotFound) {
				return uuid.Nil, domain.ErrRootCommentNotFound
			}
			return uuid.Nil, err
		}
		if rootComment.PostID != input.PostID {
			return uuid.Nil, domain.ErrRootCommentMismatch
		}

		// 如果指定了 reply_to_id，校验被回复的评论存在并获取被回复用户ID
		if input.ReplyToID != nil {
			replyToComment, err := s.repo.GetByID(ctx, *input.ReplyToID)
			if err != nil {
				if errors.Is(err, domain.ErrCommentNotFound) {
					return uuid.Nil, domain.ErrReplyTargetNotFound
				}
				return uuid.Nil, err
			}
			// 被回复的评论必须属于同一个根评论下
			// (要么是被回复评论本身就是根评论，要么其 root_id 等于当前根评论ID)
			valid := replyToComment.ID == rootID ||
				(replyToComment.RootID != nil && *replyToComment.RootID == rootID)
			if !valid {
				return uuid.Nil, domain.ErrReplyTargetNotInThread
			}
			// 获取被回复用户ID
			uid := replyToComment.UserID
			replyToUserID = &uid
		} else {
			// 未指定 reply_to_id = 直接回复根评论：被回复人即根评论作者。
			// 不置 nil 而解析出作者，回复通知（reply_comment）才有接收人，
			// 否则该评论既不是顶层也不是回复，通知被静默丢弃（docs/notice-design.md）。
			uid := rootComment.UserID
			replyToUserID = &uid
		}
	}

	// 3. 清洗 PostgreSQL text 字段不接受的字符（NULL 字节 U+0000 及其它无效
	// UTF-8 字节序列），避免写入时报 "invalid byte sequence for encoding UTF8"。
	content := utils.SanitizeForPg(input.Content)
	if content == "" {
		return uuid.Nil, domain.ErrEmptyContent
	}

	// 4. 构建评论数据并创建
	comment := &domain.Comment{
		PostID:        input.PostID,
		UserID:        userID,
		RootID:        input.RootID,
		ReplyToID:     input.ReplyToID,
		ReplyToUserID: replyToUserID,
		Content:       content,
		ExtraData:     input.ExtraData,
		Status:        domain.CommentStatusNormal,
		Deleted:       0,
	}
	if err := s.repo.Create(ctx, comment); err != nil {
		return uuid.Nil, err
	}

	// 5. 实时递增帖子评论计数（Redis Hash + 恢复缓存 + DB 同步持久化）
	if err := s.postLookup.RestoreStatsAndIncrCommentCount(ctx, input.PostID); err != nil {
		logger.Log.Error("Failed to increment post comment count: " + err.Error())
	}

	// 6. 累积帖子热度（评论 +5，per-post 上限 cap.comment，Lua 原子 clamp；best-effort）
	// @提及：校验一次得到最终名单 → 落库（best-effort 不阻断发评论）→
	// 通知使用同一份落库名单（通知名单 == 落库名单）。
	mentionIDs := s.filterMentionUserIDs(ctx, userID, input.MentionUserIDs, post.CircleID)
	if len(mentionIDs) > 0 {
		if err := s.repo.CreateMentions(ctx, comment.ID, mentionIDs); err != nil {
			logger.Log.Error("Failed to save comment mentions: " + err.Error())
		}
	}

	if s.publisher != nil {
		if err := s.publisher.PublishCommentHot(ctx, input.PostID, 1); err != nil {
			logger.Log.Error("Failed to publish comment hot: " + err.Error())
		}
		// CF 互动：评论者对该帖写互动矩阵（weight=comment，正向）。
		if err := s.publisher.PublishCommentInteraction(ctx, userID, input.PostID); err != nil {
			logger.Log.Error("Failed to publish comment interaction: " + err.Error())
		}
		// 消息中心通知（评论/回复 + @提及，best-effort）
		s.publishCommentNotifications(ctx, userID, input.PostID, comment.ID, input.RootID == nil, replyToUserID != nil, mentionIDs, content)
	}

	// 7. AI 机器人回复触发（同步回调、实现方立即返回；机器人自身评论由实现方防回环）。
	// post.CircleID 随事件透传：圈子级机器人只在同圈帖触发（circle-agent-reply）。
	if s.agentTrigger != nil {
		s.agentTrigger.OnCommentCreated(input.PostID, post.CircleID, comment.ID, userID, comment.RootID, content)
	}

	return comment.ID, nil
}

// GetRootComments 获取帖子的顶层评论列表（游标分页）。
func (s *commentServiceImpl) GetRootComments(ctx context.Context, userID, postID uuid.UUID, sort int, cursor string) (*CommentListResult, error) {
	comments, nextCursor, hasMore, err := s.repo.GetRootCommentsByCursor(ctx, postID, rootCommentPageSize, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// GetReplies 获取某条评论的子回复列表（游标分页）。
func (s *commentServiceImpl) GetReplies(ctx context.Context, userID, rootID uuid.UUID, limit, sort int, cursor string) (*CommentListResult, error) {
	// 校验根评论存在且是顶层评论
	rootComment, err := s.repo.GetByID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if rootComment.RootID != nil {
		return nil, errNotRootComment
	}

	if limit <= 0 {
		limit = defaultReplyPageSize
	}
	comments, nextCursor, hasMore, err := s.repo.GetRepliesByCursor(ctx, rootID, limit, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// LocateComment 定位评论（通知点击直达）。
//
// 流程：取目标（已删 → ErrCommentNotFound）→ 是回复则解析根评论（根已删则其下回复
// 在列表中不可达，同样按不存在处理）→ 根评论在顶层列表的定位游标 → 回复时另算
// 回复列表定位游标与页码。游标语义：目标所在页的「起始游标」（上一页末条 keyset），
// 首页为 nil（JSON null），前端不传 cursor 直接拉首页。
func (s *commentServiceImpl) LocateComment(ctx context.Context, commentID uuid.UUID, sort, replySort int) (*CommentLocateResult, error) {
	target, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return nil, err
	}

	root := target
	if target.RootID != nil {
		root, err = s.repo.GetByID(ctx, *target.RootID)
		if err != nil {
			return nil, err
		}
	}

	listCursor, err := s.repo.LocateRootCursor(ctx, root.PostID, sort, root, rootCommentPageSize)
	if err != nil {
		return nil, err
	}

	result := &CommentLocateResult{
		CommentID: target.ID,
		PostID:    target.PostID,
		RootID:    root.ID,
		IsRoot:    1,
	}
	if listCursor != "" {
		result.ListCursor = &listCursor
	}
	if target.RootID == nil {
		return result, nil
	}

	result.IsRoot = 0
	replyCursor, replyPage, err := s.repo.LocateReplyCursor(ctx, root.ID, replySort, target, defaultReplyPageSize)
	if err != nil {
		return nil, err
	}
	if replyCursor != "" {
		result.ReplyCursor = &replyCursor
	}
	result.ReplyPage = replyPage
	return result, nil
}

// GetCommentDetail 获取单条评论详情。
func (s *commentServiceImpl) GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return nil, err
	}

	vo := &CommentVO{
		ID:         comment.ID,
		PostID:     comment.PostID,
		UserID:     comment.UserID,
		RootID:     comment.RootID,
		ReplyToID:  comment.ReplyToID,
		Content:    comment.Content,
		ExtraData:  comment.ExtraData,
		LikeCount:  comment.LikeCount,
		ReplyCount: comment.ReplyCount,
		Status:     comment.Status,
		CreateTime: comment.CreateTime.Format("2006-01-02 15:04:05"),
	}

	// 获取当前用户点赞状态（缓存优先，miss 回源 DB + 回填）
	vo.Liked = s.checkLiked(ctx, userID, commentID)

	// 填充评论者信息
	if s.userFacade != nil {
		if author, err := s.userFacade.GetBrief(ctx, comment.UserID.String()); err == nil && author != nil {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}
	}

	// 填充被回复人信息
	if comment.ReplyToUserID != nil {
		vo.ReplyToUserID = comment.ReplyToUserID
		if s.userFacade != nil {
			if replyUser, err := s.userFacade.GetBrief(ctx, comment.ReplyToUserID.String()); err == nil && replyUser != nil {
				vo.ReplyToName = replyUser.Username
			}
		}
	}

	// 填充 @提及 用户列表（落库名单 → GetBrief 过滤未注销；失败置空数组，前端回退文本反查）
	vo.Mentions = []MentionVO{}
	if mentionIDsMap, err := s.repo.GetMentionUserIDsByCommentIDs(ctx, []uuid.UUID{commentID}); err == nil {
		if ids := mentionIDsMap[commentID]; len(ids) > 0 && s.userFacade != nil {
			if briefs, err := s.userFacade.GetBriefs(ctx, toStrings(ids)); err == nil {
				vo.Mentions = buildMentionVOs(ids, briefs)
			}
		}
	}

	return vo, nil
}

// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
// 返回评论所属帖子ID（冗余字段，供 like 事件消费者更新 comment.post_id）。
// 未找到返回 nil, nil。
func (s *commentServiceImpl) GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			return nil, nil
		}
		return nil, err
	}
	postID := comment.PostID
	return &postID, nil
}

// RestoreCommentStats 恢复评论统计缓存（如果不存在）。
// 供 like 领域点赞前确保 Redis stats Hash 存在，避免 Lua 脚本读到空 stats。
//
// 与旧 controller.restoreCommentStatsIfNeed 行为一致：
// 检查缓存是否存在 → 不存在则从 DB 读 like_count 写入 Redis。
func (s *commentServiceImpl) RestoreCommentStats(ctx context.Context, commentID uuid.UUID) error {
	exists, err := s.statsCache.Exists(ctx, commentID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return err
	}
	return s.statsCache.Set(ctx, commentID, comment.LikeCount)
}

// buildCommentVOs 批量构建 CommentVO（包含评论者信息和被回复人信息）。
//
// 与旧 controller.buildCommentVOs 行为一致：
//  1. 收集所有需要查询用户信息的用户ID（评论者 + 被回复人）；
//  2. 批量查询所有用户信息；
//  3. 批量查询当前用户点赞状态（Redis 优先，miss 回源 DB + 回填）；
//  4. 组装 VO。
func (s *commentServiceImpl) buildCommentVOs(ctx context.Context, userID uuid.UUID, comments []domain.Comment) []CommentVO {
	// 本页评论的提及名单（一次 IN 查询；mention 用户并入 userIDSet 共用一次 GetBriefs）
	commentIDs := make([]uuid.UUID, 0, len(comments))
	for _, cm := range comments {
		commentIDs = append(commentIDs, cm.ID)
	}
	mentionIDsMap := make(map[uuid.UUID][]uuid.UUID, len(comments))
	if len(commentIDs) > 0 {
		var err error
		mentionIDsMap, err = s.repo.GetMentionUserIDsByCommentIDs(ctx, commentIDs)
		if err != nil {
			logger.Log.Error("Failed to get comment mentions: " + err.Error())
		}
	}

	// 收集所有需要查询用户信息（评论者/被回复人/@提及用户）的用户ID
	userIDSet := make(map[uuid.UUID]struct{})
	for _, cm := range comments {
		userIDSet[cm.UserID] = struct{}{}
		if cm.ReplyToUserID != nil {
			userIDSet[*cm.ReplyToUserID] = struct{}{}
		}
	}
	for _, ids := range mentionIDsMap {
		for _, id := range ids {
			userIDSet[id] = struct{}{}
		}
	}

	// 批量查询所有用户信息（保留 string-keyed briefs 供提及组装复用）
	briefs := make(map[string]UserBrief, len(userIDSet))
	userMap := make(map[uuid.UUID]UserBrief, len(userIDSet))
	if s.userFacade != nil && len(userIDSet) > 0 {
		idStrs := make([]string, 0, len(userIDSet))
		for id := range userIDSet {
			idStrs = append(idStrs, id.String())
		}
		if fetched, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			briefs = fetched
			for idStr, b := range briefs {
				if uid, parseErr := uuid.Parse(idStr); parseErr == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询当前用户点赞状态
	likedMap := s.batchLikedStatus(ctx, userID, comments)

	// 组装
	vos := make([]CommentVO, 0, len(comments))
	for _, cm := range comments {
		vo := CommentVO{
			ID:         cm.ID,
			PostID:     cm.PostID,
			UserID:     cm.UserID,
			RootID:     cm.RootID,
			ReplyToID:  cm.ReplyToID,
			Content:    cm.Content,
			ExtraData:  cm.ExtraData,
			LikeCount:  cm.LikeCount,
			ReplyCount: cm.ReplyCount,
			Status:     cm.Status,
			CreateTime: cm.CreateTime.Format("2006-01-02 15:04:05"),
			Liked:      likedMap[cm.ID],
		}

		// 填充评论者信息
		if author, exists := userMap[cm.UserID]; exists {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}

		// 填充被回复人信息
		if cm.ReplyToUserID != nil {
			vo.ReplyToUserID = cm.ReplyToUserID
			if replyUser, exists := userMap[*cm.ReplyToUserID]; exists {
				vo.ReplyToName = replyUser.Username
			}
		}

		// 填充 @提及 用户列表（空名单给非 nil 空切片，序列化为 [] 而非 null）
		vo.Mentions = buildMentionVOs(mentionIDsMap[cm.ID], briefs)

		vos = append(vos, vo)
	}

	return vos
}

// buildMentionVOs 由提及用户ID组装 MentionVO 列表（保持提及写入顺序≈正文出现顺序）。
//
// briefs 为 GetBriefs 的返回（内部已过滤未注销用户），未命中的提及ID被静默跳过。
// 空名单返回非 nil 空切片，序列化为 [] 而非 null。
func buildMentionVOs(ids []uuid.UUID, briefs map[string]UserBrief) []MentionVO {
	mentions := make([]MentionVO, 0, len(ids))
	for _, id := range ids {
		if b, ok := briefs[id.String()]; ok {
			mentions = append(mentions, MentionVO{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL})
		}
	}
	return mentions
}

// batchLikedStatus 批量获取评论点赞状态（先查 Redis ZSET，miss 时回源 DB）。
//
// 与旧 controller.getCommentLikedStatus 行为一致。
func (s *commentServiceImpl) batchLikedStatus(ctx context.Context, userID uuid.UUID, comments []domain.Comment) map[uuid.UUID]bool {
	// 匿名访问（userID == uuid.Nil）直接返回空 map，避免对幽灵 key
	// "user:like:comments:00000000-..." 发起无意义的 Redis ZMScore + DB 回源。
	// 与 checkLiked 的 uuid.Nil 短路行为保持一致。
	if userID == uuid.Nil {
		return make(map[uuid.UUID]bool, len(comments))
	}

	commentIDs := make([]uuid.UUID, len(comments))
	for i, cm := range comments {
		commentIDs[i] = cm.ID
	}

	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool)
	}

	// 1. Batch check from Redis ZSET
	likedMap, err := s.likeCache.BatchCheck(ctx, userID, commentIDs)
	if err != nil {
		logger.Log.Error("Failed to batch check comment liked from Redis: " + err.Error())
		likedMap = make(map[uuid.UUID]bool)
	}

	// 2. Find cache misses
	var missIDs []uuid.UUID
	for _, id := range commentIDs {
		if !likedMap[id] {
			missIDs = append(missIDs, id)
		}
	}

	// 3. Fallback to DB for cache misses
	if len(missIDs) > 0 {
		dbLiked, err := s.repo.BatchCheckLiked(ctx, userID, missIDs)
		if err != nil {
			logger.Log.Error("Failed to batch check comment liked from DB: " + err.Error())
		} else {
			// Merge DB results into likedMap
			for id, liked := range dbLiked {
				likedMap[id] = liked
			}
			// Backfill ZSET for DB-confirmed likes
			var backfillIDs []uuid.UUID
			for _, id := range missIDs {
				if dbLiked[id] {
					backfillIDs = append(backfillIDs, id)
				}
			}
			if len(backfillIDs) > 0 {
				if err := s.likeCache.Backfill(ctx, userID, backfillIDs); err != nil {
					logger.Log.Error("Failed to backfill comment likes to Redis: " + err.Error())
				}
			}
		}
	}

	return likedMap
}

// checkLiked 检查单条评论的点赞状态（缓存优先，miss 回源 DB + 回填）。
//
// 与旧 controller.GetCommentDetail 中的点赞状态查询逻辑一致。
func (s *commentServiceImpl) checkLiked(ctx context.Context, userID, commentID uuid.UUID) bool {
	if userID == uuid.Nil {
		return false
	}
	likedMap, err := s.likeCache.BatchCheck(ctx, userID, []uuid.UUID{commentID})
	if err != nil {
		// 缓存故障：直接回源 DB
		isLiked, dbErr := s.repo.IsLiked(ctx, userID, commentID)
		if dbErr != nil {
			return false
		}
		return isLiked
	}

	if likedMap[commentID] {
		return true
	}
	if !likedMap[commentID] {
		// 缓存明确表示未命中（score==0），回源 DB
		isLiked, dbErr := s.repo.IsLiked(ctx, userID, commentID)
		if dbErr != nil {
			return false
		}
		if isLiked {
			_ = s.likeCache.Backfill(ctx, userID, []uuid.UUID{commentID})
		}
		return isLiked
	}
	return false
}

// ===== 消息中心通知辅助 =====

// noticeSnippetMaxRunes 通知快照上限（与 notification.snippet VARCHAR(200) 对齐留余量）。
const noticeSnippetMaxRunes = 100

// publishCommentNotifications 发布评论相关的消息中心通知（best-effort，不阻断主流程）。
//
// 顶层评论 → comment_post（接收人=帖子作者）；回复 → reply_comment（接收人=被回复评论作者）；
// @提及 → mention（接收人=被提及用户）。mentionIDs 为已校验并落库的最终名单
// （CreateComment 中 filterMentionUserIDs 的结果，通知名单 == 落库名单）；
// 接收人除 mention 外均由 consumer 反查解析。
func (s *commentServiceImpl) publishCommentNotifications(ctx context.Context, userID, postID, commentID uuid.UUID, isTopLevel, isReply bool, mentionIDs []uuid.UUID, content string) {
	snippet := truncateRunes(content, noticeSnippetMaxRunes)

	switch {
	case isTopLevel:
		if err := s.publisher.PublishCommentNotice(ctx, userID, postID, commentID, false, snippet); err != nil {
			logger.Log.Error("Failed to publish comment_post notification: " + err.Error())
		}
	case isReply:
		if err := s.publisher.PublishCommentNotice(ctx, userID, postID, commentID, true, snippet); err != nil {
			logger.Log.Error("Failed to publish reply_comment notification: " + err.Error())
		}
	}

	if len(mentionIDs) > 0 {
		if err := s.publisher.PublishMentionNotice(ctx, userID, &postID, &commentID, mentionIDs, snippet); err != nil {
			logger.Log.Error("Failed to publish mention notification: " + err.Error())
		}
	}
}

// filterMentionUserIDs 校验 @提及用户列表：去重 → 去自己 → 存在性（UserFacade）→
// 剔除越圈机器人 → 截断上限。
// 圈子作用域兜底：圈内机器人（GetAgentCircleIDs 命中且绑定圈子 ≠ 帖子圈子）静默剔除，
// 只防手搓 ID 构造请求（@选人列表已在 ES 侧过滤）。校验失败（如 user 服务不可用）
// 降级为不发 mention 通知，不影响评论主流程。
func (s *commentServiceImpl) filterMentionUserIDs(ctx context.Context, actorID uuid.UUID, mentionUserIDs []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(mentionUserIDs) == 0 || s.userFacade == nil {
		return nil
	}

	seen := make(map[uuid.UUID]struct{}, len(mentionUserIDs))
	ordered := make([]uuid.UUID, 0, len(mentionUserIDs))
	candidates := make([]string, 0, len(mentionUserIDs))
	for _, id := range mentionUserIDs {
		if id == uuid.Nil || id == actorID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
		candidates = append(candidates, id.String())
	}
	if len(candidates) == 0 {
		return nil
	}

	briefs, err := s.userFacade.GetBriefs(ctx, candidates)
	if err != nil {
		logger.Log.Error("Failed to validate mention user ids: " + err.Error())
		return nil
	}

	// 按去重后的原始顺序筛选，不存在的用户静默过滤；重复提及既不重复入列，
	// 也不占用 MentionMax 配额（截断在剔除之后，见下）。
	max := conf.Config.Notice.MentionMax
	if max <= 0 {
		max = 10
	}
	valid := make([]uuid.UUID, 0, len(briefs))
	for _, id := range ordered {
		if _, ok := briefs[id.String()]; !ok {
			continue // 不存在的用户静默过滤
		}
		valid = append(valid, id)
	}
	// 越圈机器人剔除先于截断：越圈机器人不占用 MentionMax 配额。
	valid = stripOutOfScopeAgents(ctx, s.userFacade, valid, postCircleID)
	if len(valid) > max {
		valid = valid[:max]
	}
	return valid
}

// stripOutOfScopeAgents 从 mention 候选中剔除越圈的圈内机器人：绑定圈子 ≠ postCircleID
// 的机器人静默移除；普通用户/全局机器人（map 无记录）保留。查询失败 fail-open 跳过剔除。
// post/comment 两域同构逻辑（各自 UserFacade 名义类型不同，无法共用实现）。
func stripOutOfScopeAgents(ctx context.Context, facade UserFacade, candidates []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(candidates) == 0 {
		return candidates
	}
	agentCircles, err := facade.GetAgentCircleIDs(ctx, candidates)
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

// truncateRunes 按 rune 截断字符串。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// toStrings 批量转换 uuid 列表为字符串列表。
func toStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}
