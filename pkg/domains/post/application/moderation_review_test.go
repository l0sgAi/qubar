// moderation_review_test.go 帖子人工审核（#118）应用层单元测试。
//
// 覆盖：圈主 / 管理员鉴权（403）、帖子不在该圈 / 不是审核中（404）、通过执行发布副作用且只一次、
// 驳回不执行副作用、人工审计行与审核结果通知、待审列表游标翻页与 AI 结论组装。
// 真实 PG 上的并发条件更新见 composition 包的集成测试。
package application

import (
	"context"
	"sort"
	"testing"
	"time"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

var (
	reviewOwner  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000101")
	reviewAdmin  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000102")
	reviewMember = uuid.MustParse("0192a0d0-0000-7000-8000-000000000103")
	reviewMuted  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000104")
	reviewStrang = uuid.MustParse("0192a0d0-0000-7000-8000-000000000105")
)

// fakeRoleMembers 按用户返回圈内角色 / 状态；只认 postCircleID，其他圈一律非成员。
// 发帖人 mentionActor 是正常普通成员（让 CreatePost 通过）。
type fakeRoleMembers map[uuid.UUID]CircleMemberInfo

func (m fakeRoleMembers) GetMember(_ context.Context, circleID, userID uuid.UUID) (*CircleMemberInfo, error) {
	if circleID != postCircleID {
		return nil, nil
	}
	if userID == mentionActor {
		return &CircleMemberInfo{Role: 10, Status: 1}, nil
	}
	info, ok := m[userID]
	if !ok {
		return nil, nil
	}
	return &info, nil
}

func reviewMembers() fakeRoleMembers {
	return fakeRoleMembers{
		reviewOwner:  {Role: 30, Status: 1},
		reviewAdmin:  {Role: 20, Status: 1},
		reviewMember: {Role: 10, Status: 1},
		reviewMuted:  {Role: 20, Status: 2},
	}
}

func (r *fakeModerationRepo) ResolveReview(_ context.Context, id uuid.UUID, status int16, log *domain.PostModerationLog) (bool, error) {
	for _, p := range r.created {
		if p.ID == id && p.Status == domain.PostStatusReviewing {
			p.Status = status
			r.updates++
			r.reviewLogs = append(r.reviewLogs, log)
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeModerationRepo) ListReviewingByCircle(_ context.Context, circleID uuid.UUID, after *domain.PostKeyset, limit int) ([]domain.Post, error) {
	var out []domain.Post
	for _, p := range r.created {
		if p.CircleID == circleID && p.Status == domain.PostStatusReviewing {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreateTime.Equal(out[j].CreateTime) {
			return out[i].CreateTime.After(out[j].CreateTime)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	res := out[:0]
	for _, p := range out {
		if after != nil && !keysetBefore(p, after) {
			continue
		}
		res = append(res, p)
	}
	if len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

// keysetBefore p 是否排在翻页位置 after 之后（create_time, id 倒序）。
func keysetBefore(p domain.Post, after *domain.PostKeyset) bool {
	if p.CreateTime.Equal(after.CreateTime) {
		return p.ID.String() < after.ID.String()
	}
	return p.CreateTime.Before(after.CreateTime)
}

func (p *fakeModerationPublisher) PublishReviewResultNotice(_ context.Context, operatorID, postID uuid.UUID, approved bool) error {
	p.reviewNotices = append(p.reviewNotices, reviewNotice{operator: operatorID, post: postID, approved: approved})
	return nil
}

type reviewNotice struct {
	operator, post uuid.UUID
	approved       bool
}

// newReviewFixture 审核开启、成员表带角色；发一篇审核中的帖子并模拟 AI 判为人工待审。
func newReviewFixture(t *testing.T) (*moderationFixture, uuid.UUID) {
	t.Helper()
	f := newModerationFixture(moderationCfg(true, "key", false))
	f.svc.SetMemberChecker(reviewMembers())
	id := f.createPost(t)
	f.repo.created[0].CreateTime = time.Now()
	sev, conf := 2.1, 0.9
	f.log.rows = append(f.log.rows, &domain.PostModerationLog{
		PostID: id, Decision: domain.ModerationReview, Source: domain.ModerationSourceAI,
		Hazards:  domain.HazardScores{domain.HazardSelfHarm: 0.91, domain.HazardSpamAd: 0.4, domain.HazardSexual: 0.01},
		Severity: &sev, SeverityConfidence: &conf, Model: "jev-test",
	})
	return f, id
}

func TestReviewPost_Approve(t *testing.T) {
	for _, op := range []uuid.UUID{reviewOwner, reviewAdmin} {
		f, id := newReviewFixture(t)
		res, err := f.svc.ReviewPost(context.Background(), op, ReviewPostInput{
			CircleID: postCircleID, PostID: id, Action: ReviewApprove, Reason: "  误判  ",
		})
		if err != nil {
			t.Fatalf("approve by %s: %v", op, err)
		}
		if res.Status != domain.PostStatusPublished || f.repo.created[0].Status != domain.PostStatusPublished {
			t.Fatalf("status = %d / %d, want published", res.Status, f.repo.created[0].Status)
		}
		f.assertPublishedOnce(t, id)

		if len(f.repo.reviewLogs) != 1 {
			t.Fatalf("manual logs = %d, want 1", len(f.repo.reviewLogs))
		}
		l := f.repo.reviewLogs[0]
		if l.Source != domain.ModerationSourceManual || l.Decision != domain.ModerationPublish ||
			l.OperatorID == nil || *l.OperatorID != op || l.Reason == nil || *l.Reason != "误判" {
			t.Fatalf("unexpected manual log: %+v", l)
		}
		if n := f.pub.reviewNotices; len(n) != 1 || !n[0].approved || n[0].post != id || n[0].operator != op {
			t.Fatalf("review notices = %+v", n)
		}

		// 再处理一次（重复点击 / 另一个管理员）：已不是审核中 → 404，不重复副作用、不多写日志
		_, err = f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{
			CircleID: postCircleID, PostID: id, Action: ReviewReject,
		})
		if !IsReviewPostNotFoundErr(err) {
			t.Fatalf("second review err = %v, want not found", err)
		}
		f.assertPublishedOnce(t, id)
		if len(f.repo.reviewLogs) != 1 || len(f.pub.reviewNotices) != 1 {
			t.Fatalf("second review wrote logs=%d notices=%d", len(f.repo.reviewLogs), len(f.pub.reviewNotices))
		}
	}
}

func TestReviewPost_Reject(t *testing.T) {
	f, id := newReviewFixture(t)
	res, err := f.svc.ReviewPost(context.Background(), reviewAdmin, ReviewPostInput{
		CircleID: postCircleID, PostID: id, Action: ReviewReject,
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if res.Status != domain.PostStatusRejected || f.repo.created[0].Status != domain.PostStatusRejected {
		t.Fatalf("status = %d, want rejected", res.Status)
	}
	f.assertNoSideEffects(t)
	l := f.repo.reviewLogs[0]
	if l.Decision != domain.ModerationReject || l.Source != domain.ModerationSourceManual || l.Reason != nil {
		t.Fatalf("unexpected manual log: %+v", l)
	}
	if n := f.pub.reviewNotices; len(n) != 1 || n[0].approved {
		t.Fatalf("review notices = %+v, want one rejection", n)
	}
}

// 非管理员（普通成员 / 禁言中的管理员 / 非成员 / 他圈）一律 403，且不读帖子、不改状态。
func TestReviewPost_Forbidden(t *testing.T) {
	f, id := newReviewFixture(t)
	other := uuid.MustParse("0192a0d0-0000-7000-8000-0000000000cc")
	cases := map[string]struct {
		op     uuid.UUID
		circle uuid.UUID
	}{
		"member":       {reviewMember, postCircleID},
		"muted admin":  {reviewMuted, postCircleID},
		"stranger":     {reviewStrang, postCircleID},
		"author":       {mentionActor, postCircleID},
		"other circle": {reviewOwner, other},
	}
	for name, c := range cases {
		_, err := f.svc.ReviewPost(context.Background(), c.op, ReviewPostInput{CircleID: c.circle, PostID: id, Action: ReviewApprove})
		if !IsNotCircleModeratorErr(err) {
			t.Fatalf("%s: review err = %v, want forbidden", name, err)
		}
		if _, err := f.svc.ListReviewQueue(context.Background(), c.op, c.circle, 20, ""); !IsNotCircleModeratorErr(err) {
			t.Fatalf("%s: list err = %v, want forbidden", name, err)
		}
	}
	if f.repo.created[0].Status != domain.PostStatusReviewing || len(f.repo.reviewLogs) != 0 {
		t.Fatal("forbidden requests must not change the post")
	}

	// 成员校验端口未注入：fail-closed
	f.svc.SetMemberChecker(nil)
	if _, err := f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{CircleID: postCircleID, PostID: id, Action: ReviewApprove}); !IsNotCircleModeratorErr(err) {
		t.Fatalf("nil member checker err = %v, want forbidden", err)
	}
}

// 帖子不存在 / 不在该圈 / 不是审核中 → 404；动作非法 → 400。
func TestReviewPost_NotFoundAndBadAction(t *testing.T) {
	f, id := newReviewFixture(t)
	if _, err := f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{
		CircleID: postCircleID, PostID: uuid.New(), Action: ReviewApprove,
	}); !IsReviewPostNotFoundErr(err) {
		t.Fatalf("missing post err = %v", err)
	}

	f.repo.created[0].CircleID = otherCircleID // 帖子属于别的圈
	if _, err := f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{
		CircleID: postCircleID, PostID: id, Action: ReviewApprove,
	}); !IsReviewPostNotFoundErr(err) {
		t.Fatalf("other circle post err = %v", err)
	}
	f.repo.created[0].CircleID = postCircleID

	f.repo.created[0].Status = domain.PostStatusPublished
	if _, err := f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{
		CircleID: postCircleID, PostID: id, Action: ReviewReject,
	}); !IsReviewPostNotFoundErr(err) {
		t.Fatalf("published post err = %v", err)
	}

	for _, action := range []ReviewAction{"", "publish", "APPROVE"} {
		if _, err := f.svc.ReviewPost(context.Background(), reviewOwner, ReviewPostInput{
			CircleID: postCircleID, PostID: id, Action: action,
		}); !IsInvalidReviewActionErr(err) {
			t.Fatalf("action %q err = %v", action, err)
		}
	}
	f.assertNoSideEffects(t)
}

func TestListReviewQueue_PaginatesWithVerdict(t *testing.T) {
	f, first := newReviewFixture(t)
	base := time.Now()
	f.repo.created[0].CreateTime = base.Add(-3 * time.Minute)
	ids := []uuid.UUID{first}
	for i := 1; i < 3; i++ {
		id := f.createPost(t)
		f.repo.created[i].CreateTime = base.Add(time.Duration(i-3) * time.Minute)
		ids = append(ids, id)
	}
	// 已发布的帖子不在队列里
	f.repo.created = append(f.repo.created, &domain.Post{ID: uuid.New(), CircleID: postCircleID, Status: domain.PostStatusPublished, CreateTime: base})

	page1, err := f.svc.ListReviewQueue(context.Background(), reviewAdmin, postCircleID, 2, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Posts) != 2 || page1.Cursor == "" || page1.Posts[0].ID != ids[2] || page1.Posts[1].ID != ids[1] {
		t.Fatalf("page1 = %+v", page1)
	}
	if page1.Posts[0].AIVerdict != nil {
		t.Fatalf("post without AI log should have null verdict: %+v", page1.Posts[0].AIVerdict)
	}

	page2, err := f.svc.ListReviewQueue(context.Background(), reviewAdmin, postCircleID, 2, page1.Cursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Posts) != 1 || page2.Cursor != "" || page2.Posts[0].ID != first {
		t.Fatalf("page2 = %+v", page2)
	}
	v := page2.Posts[0].AIVerdict
	if v == nil || v.Decision != "review" || v.Model != "jev-test" || v.Severity == nil || *v.Severity != 2.1 || len(v.Hazards) != 3 {
		t.Fatalf("verdict = %+v", v)
	}
	// 默认转人工阈值 0.35：self_harm 0.91 > spam_ad 0.4，sexual 0.01 不算命中
	if len(v.HitHazards) != 2 || v.HitHazards[0] != "self_harm" || v.HitHazards[1] != "spam_ad" {
		t.Fatalf("hit hazards = %v", v.HitHazards)
	}
	if p := page2.Posts[0]; p.UserID != mentionActor || p.Title != "标题" || p.Content == "" || p.MediaExtra == nil {
		t.Fatalf("post fields not assembled: %+v", p)
	}

	for _, bad := range []string{"!!!", "e30=", "eyJ0IjoxfQ=="} { // 非 base64 / {} / 缺 id
		if _, err := f.svc.ListReviewQueue(context.Background(), reviewAdmin, postCircleID, 2, bad); !IsInvalidReviewCursorErr(err) {
			t.Fatalf("cursor %q err = %v, want invalid cursor", bad, err)
		}
	}
}

func TestReviewCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 23, 12, 0, 0, 123456000, time.UTC)
	id := uuid.MustParse("0192a0d0-0000-7000-8000-000000000999")
	k, err := decodeReviewCursor(encodeReviewCursor(ts, id))
	if err != nil || !k.CreateTime.Equal(ts) || k.ID != id {
		t.Fatalf("round trip = %+v, %v", k, err)
	}
}
