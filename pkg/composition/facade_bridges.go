// Package composition 的 facade_bridges.go：
// 跨领域 Facade 桥接器，把各领域的 Facade 接口连起来。
//
// 这是"领域间通过 Facade 通信"模式的核心：
//   - user 领域暴露 UserFacade（GetBriefs/GetBrief）
//   - circle 领域暴露 CircleFacade（GetBriefs）+ CircleMemberChecker + CircleStatusChecker + CirclePostCountPort
//   - post 领域暴露 PostMediaFetcher（GetMediaByPostIDs）
//
// 各领域定义的 Facade 接口字段类型（如 user.application.UserBrief vs
// circle.application.UserBrief）虽然结构相同但是不同类型，
// 因此桥接器需要做字段级转换。
package composition

import (
	"context"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	commentapp "interestBar/pkg/domains/comment/application"
	discoverdomain "interestBar/pkg/domains/discover/domain"
	historyapp "interestBar/pkg/domains/history/application"
	noticeapp "interestBar/pkg/domains/notice/application"
	postapp "interestBar/pkg/domains/post/application"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
	trendingdomain "interestBar/pkg/domains/trending/domain"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// ===== user → (circle, post) =====

// circleUserFacade 把 user.application.UserFacade 适配为 circle.application.UserFacade。
type circleUserFacade struct {
	delegate userapp.UserFacade
}

func (f *circleUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]circleapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]circleapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// SearchBriefs 按关键字搜索用户（circle 成员管理搜索用），保序映射为 circle 域的
// UserBrief，并透传命中总数 total（total > len(列表) 表示按 limit 截断）。
func (f *circleUserFacade) SearchBriefs(ctx context.Context, keyword string, limit int) ([]circleapp.UserBrief, int64, error) {
	briefs, total, err := f.delegate.SearchBriefs(ctx, keyword, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]circleapp.UserBrief, 0, len(briefs))
	for _, b := range briefs {
		result = append(result, circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL})
	}
	return result, total, nil
}

// postUserFacade 把 user.application.UserFacade 适配为 post.application.UserFacade。
type postUserFacade struct {
	delegate userapp.UserFacade
}

func (f *postUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]postapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *postUserFacade) GetBrief(ctx context.Context, userID string) (*postapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（mention 兜底剔除越圈机器人用，
// 入出参类型一致直接透传）。
func (f *postUserFacade) GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return f.delegate.GetAgentCircleIDs(ctx, userIDs)
}

// ===== circle → post =====

// postCircleFacade 把 circle.application.CircleFacade 适配为 post.application.CircleFacade。
type postCircleFacade struct {
	delegate circleapp.CircleFacade
}

func (f *postCircleFacade) GetBriefs(ctx context.Context, circleIDs []string) (map[string]postapp.CircleBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, circleIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.CircleBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.CircleBrief{ID: b.ID, Name: b.Name, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// postMediaFetcherForCircle 把 post.application.PostService 适配为
// circle.application.PostMediaFetcher。
type postMediaFetcherForCircle struct {
	delegate postapp.PostService
}

func (f *postMediaFetcherForCircle) GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error) {
	return f.delegate.GetMediaByPostIDs(ctx, postIDs)
}

// ===== post → circle（成员/状态校验 + 帖子计数端口）=====

// circleMemberCheckerForPost 把 circle MemberRepository 适配为 post 的 CircleMemberChecker。
type circleMemberCheckerForPost struct {
	memberRepo circledomain.MemberRepository
}

func (c *circleMemberCheckerForPost) GetMember(ctx context.Context, circleID, userID uuid.UUID) (*postapp.CircleMemberInfo, error) {
	m, err := c.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil {
		if err == circledomain.ErrMemberNotFound {
			return nil, nil
		}
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return &postapp.CircleMemberInfo{
		Role:        m.Role,
		Status:      m.Status,
		MuteEndTime: m.MuteEndTime,
	}, nil
}

// circleStatusCheckerForPost 把 circle CircleRepository 适配为 post 的 CircleStatusChecker。
type circleStatusCheckerForPost struct {
	circleRepo circledomain.CircleRepository
}

func (c *circleStatusCheckerForPost) IsCircleAvailable(ctx context.Context, circleID uuid.UUID) (bool, error) {
	circle, err := c.circleRepo.GetByID(ctx, circleID)
	if err != nil {
		if err == circledomain.ErrCircleNotFound {
			return false, nil
		}
		return false, err
	}
	return circle.Status == circledomain.CircleStatusNormal, nil
}

// circlePostCountPortForPost 把 circle CircleService 适配为 post 的 CirclePostCountPort。
type circlePostCountPortForPost struct {
	svc circleapp.CircleService
}

func (p *circlePostCountPortForPost) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	return p.svc.IncrPostCount(ctx, circleID)
}

// ===== user → comment =====

// commentUserFacade 把 user.application.UserFacade 适配为 comment.application.UserFacade。
type commentUserFacade struct {
	delegate userapp.UserFacade
}

func (f *commentUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]commentapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]commentapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *commentUserFacade) GetBrief(ctx context.Context, userID string) (*commentapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（mention 兜底剔除越圈机器人用，
// 入出参类型一致直接透传）。
func (f *commentUserFacade) GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return f.delegate.GetAgentCircleIDs(ctx, userIDs)
}

// ===== user → notice =====

// noticeUserFacade 把 user.application.UserFacade 适配为 notice.application.UserFacade。
type noticeUserFacade struct {
	delegate userapp.UserFacade
}

func (f *noticeUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]noticeapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]noticeapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = noticeapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// ===== post → comment（帖子元信息 + 评论计数端口）=====

// commentPostLookup 把 post.application.PostService 适配为 comment.application.PostLookup。
//
// comment 领域发评论时需要：
//   - 校验帖子存在性/状态/锁定 → GetPost
//   - 恢复帖子统计缓存 + 递增评论计数 → RestoreStatsAndIncrCommentCount
type commentPostLookup struct {
	delegate postapp.PostService
}

func (l *commentPostLookup) GetPost(ctx context.Context, postID uuid.UUID) (*commentapp.PostInfo, error) {
	meta, err := l.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return nil, err
	}
	return &commentapp.PostInfo{
		ID:       meta.ID,
		Status:   meta.Status,
		IsLock:   meta.IsLock,
		CircleID: meta.CircleID,
	}, nil
}

func (l *commentPostLookup) RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return l.delegate.RestoreStatsAndIncrCommentCount(ctx, postID)
}

// ===== post → like（帖子存在性 + 统计缓存恢复 + 真实点赞状态）=====

// likePostTarget 把 post.application.PostService 适配为 like.application.PostTarget。
type likePostTarget struct {
	delegate postapp.PostService
}

func (t *likePostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return true, nil
}

func (t *likePostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

func (t *likePostTarget) IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	return t.delegate.IsLikedByUser(ctx, userID, postID)
}

// ===== comment → like（评论存在性 + 所属帖子ID + 统计缓存恢复 + 真实点赞状态）=====

// likeCommentTarget 把 comment.application.CommentService 适配为 like.application.CommentTarget。
type likeCommentTarget struct {
	delegate commentapp.CommentService
}

func (t *likeCommentTarget) ExistsWithPostID(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, bool, error) {
	postID, err := t.delegate.GetCommentMeta(ctx, commentID)
	if err != nil {
		return nil, false, err
	}
	if postID == nil {
		return nil, false, nil
	}
	return postID, true, nil
}

func (t *likeCommentTarget) RestoreStats(ctx context.Context, commentID uuid.UUID) error {
	return t.delegate.RestoreCommentStats(ctx, commentID)
}

func (t *likeCommentTarget) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	return t.delegate.IsLikedByUser(ctx, userID, commentID)
}

// ===== post → collect（帖子存在性 + 统计缓存恢复 + 列表组装）=====

// collectPostTarget 把 post.application.PostService 适配为 collect.domain.PostTarget。
//
// 与 likePostTarget 行为完全一致（collect.PostTarget 与 like.PostTarget 签名相同），
// 独立定义以保持命名清晰、避免跨领域类型耦合。
type collectPostTarget struct {
	delegate postapp.PostService
}

func (t *collectPostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return true, nil
}

func (t *collectPostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

// collectPostFetcher 把 post.application.PostService 适配为 collect.application.PostFetcher。
type collectPostFetcher struct {
	delegate postapp.PostService
}

func (f *collectPostFetcher) GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.GetPostsByIDs(ctx, postIDs)
}

// ===== history ↔ post =====

// postHistoryRecorder 把 history.application.HistoryService 适配为 post.domain.HistoryRecorder。
// 供 post 域详情页浏览时 async 回调记录浏览历史。
type postHistoryRecorder struct {
	delegate historyapp.HistoryService
}

func (r *postHistoryRecorder) RecordView(ctx context.Context, userID, postID uuid.UUID) error {
	return r.delegate.RecordView(ctx, userID, postID)
}

// historyPostFetcher 把 post.application.PostService 适配为 history.application.PostFetcher(ES 来源)。
// 供 history「最近浏览」列表按 postID 从 ES 批量取已组装帖子。
type historyPostFetcher struct {
	delegate postapp.PostService
}

func (f *historyPostFetcher) SearchByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.SearchPostsByIDs(ctx, postIDs)
}

func (f *historyPostFetcher) SearchByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) ([]postapp.PostListItem, int64, error) {
	return f.delegate.SearchPostsByIDsAndKeyword(ctx, postIDs, keyword, size, offset)
}

// ===== recommend ← (post, circle) =====

// recommendCircleLookup 把 circle.application.CircleService 适配为 recommend.domain.CircleLookup。
type recommendCircleLookup struct {
	delegate circleapp.CircleService
}

func (l *recommendCircleLookup) ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return l.delegate.ListJoinedCircleIDs(ctx, userID, limit)
}

// recommendPostMetaReader 把 post.application.PostService 适配为 recommend.domain.PostMetaReader。
type recommendPostMetaReader struct {
	delegate postapp.PostService
}

func (r *recommendPostMetaReader) ListCircleIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	return r.delegate.ListCircleIDsByPostIDs(ctx, postIDs)
}

// recommendPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// recommend.domain.PostHydrator（[]PostListItem → []FeedPostItem 字段拷贝，不含交互态）。
type recommendPostHydrator struct {
	delegate postapp.PostService
}

func (h *recommendPostHydrator) Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	out := make([]recommenddomain.FeedPostItem, 0, len(items))
	for _, p := range items {
		out = append(out, recommenddomain.FeedPostItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: p.AuthorName, AuthorAvatar: p.AuthorAvatar,
			CircleName: p.CircleName, CircleAvatar: p.CircleAvatar,
			Images: p.Images,
		})
	}
	return out, nil
}

// ===== trending ← (post, circle, user) =====
//
// trending 是跨域编排器（无聚合根），4 个只读端口：post hydrate + 交互态、circle GetByIDs、user GetBriefs。
// 与 recommend 桥接器同款风格：DTO 字段拷贝（trending.domain 与生产者域 DTO 结构同形但名义不同）。

// trendingPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// trending.domain.PostHydrator（[]PostListItem → []recommend.domain.FeedPostItem 字段拷贝，不含交互态）。
//
// 复用 recommend.domain.FeedPostItem 作为 trending.domain.TrendingPostItem 的内嵌展示 DTO（纯值对象）。
type trendingPostHydrator struct {
	delegate postapp.PostService
}

func (h *trendingPostHydrator) Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	out := make([]recommenddomain.FeedPostItem, 0, len(items))
	for _, p := range items {
		out = append(out, recommenddomain.FeedPostItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: p.AuthorName, AuthorAvatar: p.AuthorAvatar,
			CircleName: p.CircleName, CircleAvatar: p.CircleAvatar,
			Images: p.Images,
		})
	}
	return out, nil
}

// trendingCircleLookup 把 circle.domain.CircleRepository 适配为 trending.domain.CircleLookup。
// 返回完整 Circle 实体（含 member_count/post_count/hot），由 trending service 组装 TrendingCircleItem。
type trendingCircleLookup struct {
	repo circledomain.CircleRepository
}

func (l *trendingCircleLookup) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error) {
	return l.repo.GetByIDs(ctx, circleIDs)
}

// trendingUserLookup 把 user.application.UserFacade 适配为 trending.domain.UserLookup。
type trendingUserLookup struct {
	delegate userapp.UserFacade
}

func (l *trendingUserLookup) GetBriefs(ctx context.Context, userIDs []string) (map[string]trendingdomain.UserBrief, error) {
	briefs, err := l.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]trendingdomain.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = trendingdomain.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// 编译期保证桥接器满足 trending.domain 端口（与项目其它大适配器的 guard 惯例一致）。
var (
	_ trendingdomain.PostHydrator       = (*trendingPostHydrator)(nil)
	_ trendingdomain.InteractionChecker = (*postInteractionChecker)(nil)
	_ trendingdomain.CircleLookup       = (*trendingCircleLookup)(nil)
	_ trendingdomain.UserLookup         = (*trendingUserLookup)(nil)
)

// ===== discover ← (post, circle) =====
//
// discover 是跨域编排器（无聚合根），5 个只读端口：post hydrate + 交互态、circle GetByIDs +
// joined IDs、seed 已交互帖。与 recommend/trending 桥接器同款风格。

// discoverPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// discover.domain.PostHydrator（[]PostListItem → []recommend.domain.FeedPostItem 字段拷贝，不含交互态）。
// 与 trendingPostHydrator 同款实现；discover.domain.PostHydrator 返回类型复用 recommend.domain.FeedPostItem。
type discoverPostHydrator struct {
	delegate postapp.PostService
}

func (h *discoverPostHydrator) Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	out := make([]recommenddomain.FeedPostItem, 0, len(items))
	for _, p := range items {
		out = append(out, recommenddomain.FeedPostItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: p.AuthorName, AuthorAvatar: p.AuthorAvatar,
			CircleName: p.CircleName, CircleAvatar: p.CircleAvatar,
			Images: p.Images,
		})
	}
	return out, nil
}

// discoverCircleLookup 把 circle.domain.CircleRepository 适配为 discover.domain.CircleLookup。
// 与 trendingCircleLookup 同款实现（返回完整 Circle 实体）。
type discoverCircleLookup struct {
	repo circledomain.CircleRepository
}

func (l *discoverCircleLookup) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error) {
	return l.repo.GetByIDs(ctx, circleIDs)
}

// discoverSeedReader 把 redispkg ZSET 适配为 discover.domain.SeedReader（反气泡已交互帖）。
// 与 recommend 的 seedReaderRedis 同款实现（无状态，委托全局 Client）。
type discoverSeedReader struct{}

func (r *discoverSeedReader) LikedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostLikedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDsForDiscover(raw), nil
}

func (r *discoverSeedReader) CollectedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostCollectedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDsForDiscover(raw), nil
}

func (r *discoverSeedReader) ViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	entries, _, err := redispkg.ListPostViews(userID, 0, int64(limit))
	if err != nil {
		return nil, err
	}
	raw := make([]string, 0, len(entries))
	for _, e := range entries {
		raw = append(raw, e.ID)
	}
	return parseIDsForDiscover(raw), nil
}

// parseIDsForDiscover 把 string ID 列表转成 []uuid.UUID（跳过非法）。
func parseIDsForDiscover(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, perr := uuid.Parse(s); perr == nil {
			out = append(out, id)
		}
	}
	return out
}

// discoverJoinedCircleLookup 把 circle.application.CircleService 适配为 discover.domain.JoinedCircleLookup。
// 与 recommendCircleLookup 同款实现。
type discoverJoinedCircleLookup struct {
	delegate circleapp.CircleService
}

func (l *discoverJoinedCircleLookup) ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return l.delegate.ListJoinedCircleIDs(ctx, userID, limit)
}

// 编译期保证桥接器满足 discover.domain 端口。
var (
	_ discoverdomain.PostHydrator       = (*discoverPostHydrator)(nil)
	_ discoverdomain.InteractionChecker = (*postInteractionChecker)(nil)
	_ discoverdomain.CircleLookup       = (*discoverCircleLookup)(nil)
	_ discoverdomain.SeedReader         = (*discoverSeedReader)(nil)
	_ discoverdomain.JoinedCircleLookup = (*discoverJoinedCircleLookup)(nil)
)

// ===== post → recommend / trending / discover（信息流 is_liked / is_collected 回显）=====

// postInteractionChecker 把 post.application.PostService.BatchCheckInteractions 适配为
// recommend / trending / discover 三个 InteractionChecker 端口（签名一致，共用一个桥接器）。
//
// 旧实现各自直接调 redispkg ZSET，miss 一律当 false：ZSET 有 TTL 与容量上限，
// 已赞帖被显示为未赞会诱发重复点赞（#46）。现统一走 post 的"缓存 → miss 回源 DB → 回填"。
type postInteractionChecker struct {
	delegate postapp.PostService
}

func (c *postInteractionChecker) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error) {
	return c.delegate.BatchCheckInteractions(ctx, userID, postIDs)
}

var _ recommenddomain.InteractionChecker = (*postInteractionChecker)(nil)
