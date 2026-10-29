//go:build integration

// private_circle_visibility_integration_test.go 私密圈内容隔离（#170）端到端集成测试（make test-integration）：
// 真实 PG（circle / circle_member / post / comment / post_collect / post_report）+ Redis（私密圈 ID 集合缓存、
// 计数缓存）+ Redpanda（成员侧写操作的事件）。按 composition 的桥接装配 circle → post → comment / like / collect。
//
// 覆盖访客、非成员、正常成员、禁言成员、已移出成员五种身份：帖子详情、评论读、写操作（评论 / 点赞 / 收藏 / 举报）、
// 圈子详情精简视图、圈内帖子 403；以及建圈、修改 join_type 时私密圈集合缓存失效。
package composition

import (
	"context"
	"errors"
	"testing"
	"time"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	collectapp "interestBar/pkg/domains/collect/application"
	collectdomain "interestBar/pkg/domains/collect/domain"
	commentapp "interestBar/pkg/domains/comment/application"
	commentdomain "interestBar/pkg/domains/comment/domain"
	likeapp "interestBar/pkg/domains/like/application"
	likedomain "interestBar/pkg/domains/like/domain"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type visibilityEnv struct {
	db         *gorm.DB
	circleSvc  circleapp.CircleService
	visibility circleapp.CircleVisibility
	postSvc    postapp.PostService
	commentSvc commentapp.CommentService
	likeSvc    likeapp.LikeService
	collectSvc collectapp.CollectService
}

// setupVisibility 按 RegisterDomainRoutes 的桥接方式装配 circle / post / comment / like / collect。
func setupVisibility(t *testing.T) *visibilityEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)

	deps := NewDeps()
	circleRepo := circleinfra.NewCircleRepository(db)
	memberRepo := circleinfra.NewMemberRepository(db)
	privateCache := circleinfra.NewPrivateCircleCache()
	circleSvc := circleapp.NewCircleService(circleRepo, memberRepo, circleinfra.NewCircleBaseCache(),
		circleinfra.NewCircleStatsCache(), circleinfra.NewJoinedCirclesCache(), emptyCircleSearcher{},
		circleinfra.NewCircleEventPublisher(), privateCache)
	vis := circleapp.NewCircleVisibility(circleRepo, memberRepo, privateCache)

	postSvc := newPostService(deps)
	postSvc.SetUserFacade(anyUserFacade{})
	postSvc.SetCircleVisibility(vis)

	commentSvc := newCommentService(deps)
	commentSvc.SetPostLookup(&commentPostLookup{delegate: postSvc})

	likeSvc := newLikeService(deps)
	likeSvc.SetPostTarget(&likePostTarget{delegate: postSvc})
	likeSvc.SetCommentTarget(&likeCommentTarget{delegate: commentSvc})

	collectSvc := newCollectService(deps)
	collectSvc.SetPostTarget(&collectPostTarget{delegate: postSvc})

	return &visibilityEnv{
		db: db, circleSvc: circleSvc, visibility: vis, postSvc: postSvc,
		commentSvc: commentSvc, likeSvc: likeSvc, collectSvc: collectSvc,
	}
}

// emptyCircleSearcher 集成栈不含 ES：圈内帖子列表返回空结果，只验证 403 拦截发生在检索之前；
// SearchMy 直接回显 ID（「TA 加入的圈子」过滤断言用）。
type emptyCircleSearcher struct{ circleapp.CircleSearcher }

func (emptyCircleSearcher) SearchCirclePosts(context.Context, uuid.UUID, int, int, []interface{}) (*circleapp.RawCirclePostResult, error) {
	return &circleapp.RawCirclePostResult{}, nil
}

// SearchMy 原样返回传入的圈子（视为正常、未删除、命中），供 /circle/user 过滤断言。
func (emptyCircleSearcher) SearchMy(_ context.Context, circleIDs []uuid.UUID, _ string, size int, _ []interface{}) (*circleapp.MyCircleSearchResult, error) {
	docs := make([]circleapp.MyCircleDoc, 0, len(circleIDs))
	for _, id := range circleIDs {
		docs = append(docs, circleapp.MyCircleDoc{ID: id.String()})
	}
	return &circleapp.MyCircleSearchResult{Circles: docs, Size: size}, nil
}

// visCircle 一个随机圈子：圈主 + 正常成员 + 禁言成员 + 已移出成员（status=3）+ 已退出成员（status=4）+ 非成员。
type visCircle struct {
	id                                  uuid.UUID
	owner, member, muted, removed, left uuid.UUID
	outsider                            uuid.UUID
	postID, commentID                   uuid.UUID
}

// newVisCircle 经 CircleService.CreateCircle 建圈（走真实的私密圈集合失效路径），落成员与一篇已发布帖子，
// 并由圈主发一条评论。
func newVisCircle(t *testing.T, env *visibilityEnv, joinType int16) visCircle {
	t.Helper()
	ctx := t.Context()
	c := visCircle{
		owner: sharedomain.NewID(), member: sharedomain.NewID(), muted: sharedomain.NewID(),
		removed: sharedomain.NewID(), left: sharedomain.NewID(), outsider: sharedomain.NewID(),
	}
	name := "私密隔离-" + sharedomain.NewID().String()[24:]
	if err := env.circleSvc.CreateCircle(ctx, c.owner, circleapp.CreateCircleInput{
		Name: name, Description: "可见性测试", CategoryID: sharedomain.NewID(), JoinType: joinType,
	}); err != nil {
		t.Fatalf("create circle: %v", err)
	}
	if err := env.db.Raw(`SELECT id FROM domains.circle WHERE name = ?`, name).Row().Scan(&c.id); err != nil {
		t.Fatalf("load circle id: %v", err)
	}
	testkit.CleanupRows(t, "domains.circle", "id", c.id)
	testkit.CleanupRows(t, "domains.circle_member", "circle_id", c.id)

	muteEnd := time.Now().Add(time.Hour)
	for _, m := range []struct {
		user    uuid.UUID
		status  int16
		muteEnd *time.Time
	}{
		{c.member, circledomain.MemberStatusNormal, nil},
		{c.muted, circledomain.MemberStatusMuted, &muteEnd},
		{c.removed, circledomain.MemberStatusBanned, nil},
		{c.left, circledomain.MemberStatusLeft, nil},
	} {
		if err := env.db.Exec(`INSERT INTO domains.circle_member (id, circle_id, user_id, role, status, mute_end_time)
			VALUES (?, ?, ?, 10, ?, ?)`, sharedomain.NewID(), c.id, m.user, m.status, m.muteEnd).Error; err != nil {
			t.Fatalf("insert circle member: %v", err)
		}
	}

	p := &postdomain.Post{
		CircleID: c.id, UserID: c.owner, Type: postdomain.PostTypeTextImage,
		Title: "私密圈帖子-" + sharedomain.NewID().String(), Content: "仅成员可见",
		MediaExtra: postdomain.MediaExtraJSON{}, Status: postdomain.PostStatusPublished,
	}
	if err := postinfra.NewPostRepository(env.db).Create(ctx, p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	c.postID = p.ID
	for _, table := range []string{"domains.post_mention", "domains.post_report", "domains.post_collect", "domains.comment", "domains.comment_like"} {
		testkit.CleanupRows(t, table, "post_id", p.ID)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)

	commentID, err := env.commentSvc.CreateComment(ctx, c.owner, commentapp.CreateCommentInput{PostID: p.ID, Content: "圈主的评论"})
	if err != nil {
		t.Fatalf("owner create comment: %v", err)
	}
	c.commentID = commentID
	return c
}

type visViewer struct {
	name    string
	id      uuid.UUID
	visible bool
}

func (c visCircle) viewers() []visViewer {
	return []visViewer{
		{"guest", uuid.Nil, false},
		{"outsider", c.outsider, false},
		{"member", c.member, true},
		{"muted", c.muted, true},
		{"removed", c.removed, false},
		{"left", c.left, false},
	}
}

func TestPrivateCircle_ReadVisibility(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()

	for _, v := range c.viewers() {
		t.Run(v.name, func(t *testing.T) {
			want := func(op string, err, hidden error) {
				t.Helper()
				if v.visible && err != nil {
					t.Fatalf("%s: want ok, got %v", op, err)
				}
				if !v.visible && !errors.Is(err, hidden) {
					t.Fatalf("%s: want %v, got %v", op, hidden, err)
				}
			}
			_, err := env.postSvc.GetPostDetail(ctx, v.id, c.postID)
			want("post detail", err, postdomain.ErrPostNotFound)
			_, err = env.commentSvc.GetRootComments(ctx, v.id, c.postID, 0, "")
			want("comment list", err, commentdomain.ErrPostNotFound)
			_, err = env.commentSvc.GetReplies(ctx, v.id, c.commentID, 0, 0, "")
			want("comment replies", err, commentdomain.ErrCommentNotFound)
			_, err = env.commentSvc.LocateComment(ctx, v.id, c.commentID, 0, 1)
			want("comment locate", err, commentdomain.ErrCommentNotFound)
			_, err = env.commentSvc.GetCommentDetail(ctx, v.id, c.commentID)
			want("comment detail", err, commentdomain.ErrCommentNotFound)

			detail, err := env.circleSvc.GetCircleDetail(ctx, v.id, c.id)
			if err != nil {
				t.Fatalf("circle detail: %v", err)
			}
			if detail.Restricted == v.visible {
				t.Fatalf("circle detail restricted=%v, want %v", detail.Restricted, !v.visible)
			}
			if detail.Restricted && (detail.Description != "" || detail.CreatorID != uuid.Nil || detail.Name == "" || detail.MemberCount == 0) {
				t.Fatalf("restricted detail leaks or misses fields: %+v", detail)
			}
			if !detail.Restricted && detail.Description == "" {
				t.Fatalf("member should see full detail: %+v", detail)
			}

			_, err = env.circleSvc.GetCirclePosts(ctx, v.id, c.id, 1, 10, nil)
			if v.visible && err != nil || !v.visible && !circleapp.IsCircleContentForbiddenErr(err) {
				t.Fatalf("circle posts: visible=%v err=%v", v.visible, err)
			}
		})
	}
}

func TestPrivateCircle_WritesRejectedForNonMembers(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()

	for _, v := range c.viewers() {
		if v.visible || v.id == uuid.Nil {
			continue // 写接口要求登录；成员的写入见 TestPrivateCircle_MemberWrites
		}
		t.Run(v.name, func(t *testing.T) {
			_, err := env.commentSvc.CreateComment(ctx, v.id, commentapp.CreateCommentInput{PostID: c.postID, Content: "越权评论"})
			if !errors.Is(err, commentdomain.ErrPostNotFound) {
				t.Fatalf("create comment: %v", err)
			}
			_, err = env.likeSvc.Toggle(ctx, v.id, likeapp.ToggleInput{Type: "post", TargetID: c.postID})
			if !errors.Is(err, likedomain.ErrPostNotFound) {
				t.Fatalf("like post: %v", err)
			}
			_, err = env.likeSvc.Toggle(ctx, v.id, likeapp.ToggleInput{Type: "comment", TargetID: c.commentID})
			if !errors.Is(err, likedomain.ErrCommentNotFound) {
				t.Fatalf("like comment: %v", err)
			}
			_, err = env.collectSvc.Toggle(ctx, v.id, collectapp.ToggleInput{PostID: c.postID})
			if !errors.Is(err, collectdomain.ErrPostNotFound) {
				t.Fatalf("collect: %v", err)
			}
			err = env.postSvc.ReportPost(ctx, v.id, postapp.ReportPostInput{PostID: c.postID, ReasonType: postdomain.ReportReasonSpamAd})
			if !postapp.IsReportPostNotFoundErr(err) {
				t.Fatalf("report: %v", err)
			}

			liked, err := env.postSvc.IsLikedByUser(ctx, v.id, c.postID)
			if err != nil || liked {
				t.Fatalf("rejected like must not be recorded: liked=%v err=%v", liked, err)
			}
			commentLiked, err := env.commentSvc.IsLikedByUser(ctx, v.id, c.commentID)
			if err != nil || commentLiked {
				t.Fatalf("rejected comment like must not be recorded: liked=%v err=%v", commentLiked, err)
			}
			assertRowCount(t, env.db, `SELECT count(*) FROM domains.comment WHERE post_id = ? AND user_id = ?`, 0, c.postID, v.id)
			assertRowCount(t, env.db, `SELECT count(*) FROM domains.post_collect WHERE post_id = ? AND user_id = ?`, 0, c.postID, v.id)
			assertRowCount(t, env.db, `SELECT count(*) FROM domains.post_report WHERE post_id = ? AND reporter_id = ?`, 0, c.postID, v.id)
		})
	}
	// 只有圈主那条评论计入评论数
	assertRowCount(t, env.db, `SELECT comment_count FROM domains.post WHERE id = ?`, 1, c.postID)
}

func TestPrivateCircle_MemberWrites(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()

	for _, uid := range []uuid.UUID{c.member, c.muted} {
		if _, err := env.likeSvc.Toggle(ctx, uid, likeapp.ToggleInput{Type: "post", TargetID: c.postID, Action: "like"}); err != nil {
			t.Fatalf("member like post: %v", err)
		}
		if _, err := env.likeSvc.Toggle(ctx, uid, likeapp.ToggleInput{Type: "comment", TargetID: c.commentID, Action: "like"}); err != nil {
			t.Fatalf("member like comment: %v", err)
		}
		if _, err := env.collectSvc.Toggle(ctx, uid, collectapp.ToggleInput{PostID: c.postID, Action: "collect"}); err != nil {
			t.Fatalf("member collect: %v", err)
		}
	}
	if _, err := env.commentSvc.CreateComment(ctx, c.member, commentapp.CreateCommentInput{PostID: c.postID, Content: "成员评论"}); err != nil {
		t.Fatalf("member comment: %v", err)
	}
	if err := env.postSvc.ReportPost(ctx, c.member, postapp.ReportPostInput{PostID: c.postID, ReasonType: postdomain.ReportReasonSpamAd}); err != nil {
		t.Fatalf("member report: %v", err)
	}
	assertRowCount(t, env.db, `SELECT count(*) FROM domains.post_collect WHERE post_id = ? AND deleted = 0`, 2, c.postID)

	// 系统身份（AI 机器人回复链路）不受私密圈可见性约束（#170 范围外，保持现状）。
	if _, err := env.commentSvc.CreateComment(ctx, c.outsider, commentapp.CreateCommentInput{
		PostID: c.postID, Content: "机器人回复", SystemActor: true,
	}); err != nil {
		t.Fatalf("system actor comment: %v", err)
	}
}

// 修改 join_type 后私密圈集合缓存立即失效：公开 → 私密，非成员立刻看不到；改回公开立刻恢复。
func TestPrivateCircle_JoinTypeChangeInvalidatesCache(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypeDirect)
	ctx := t.Context()

	canView := func() bool {
		t.Helper()
		ok, err := env.visibility.CanView(ctx, c.outsider, c.id)
		if err != nil {
			t.Fatalf("can view: %v", err)
		}
		return ok
	}
	if !canView() {
		t.Fatal("public circle must be visible to outsiders")
	}
	if _, err := env.postSvc.GetPostDetail(ctx, uuid.Nil, c.postID); err != nil {
		t.Fatalf("guest reads public post: %v", err)
	}

	setJoinType := func(jt int16) {
		t.Helper()
		if err := env.circleSvc.UpdateCircleProfile(ctx, c.owner, c.id, circleapp.UpdateCircleProfileInput{JoinType: &jt}); err != nil {
			t.Fatalf("update join_type: %v", err)
		}
	}
	setJoinType(circledomain.CircleJoinTypePrivate)
	if canView() {
		t.Fatal("circle switched to private must be hidden from outsiders immediately")
	}
	if _, err := env.postSvc.GetPostDetail(ctx, c.outsider, c.postID); !errors.Is(err, postdomain.ErrPostNotFound) {
		t.Fatalf("outsider reads now-private post: %v", err)
	}
	hidden, err := env.visibility.HiddenCircleIDs(ctx, c.outsider)
	if err != nil || !containsID(hidden, c.id) {
		t.Fatalf("hidden ids for outsider must contain circle: err=%v", err)
	}
	hidden, err = env.visibility.HiddenCircleIDs(ctx, c.muted)
	if err != nil || containsID(hidden, c.id) {
		t.Fatalf("hidden ids for muted member must not contain circle: err=%v", err)
	}

	setJoinType(circledomain.CircleJoinTypeDirect)
	if !canView() {
		t.Fatal("circle switched back to public must be visible again")
	}
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func assertRowCount(t *testing.T, db *gorm.DB, query string, want int64, args ...any) {
	t.Helper()
	var got int64
	if err := db.Raw(query, args...).Scan(&got).Error; err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", query, got, want)
	}
}
