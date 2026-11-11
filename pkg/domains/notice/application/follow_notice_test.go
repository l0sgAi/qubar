// follow_notice_test.go 被关注通知（notice_type = 12，#219）在列表侧的单元测试：
// type 过滤接受 12、actor 照常展示、没有帖子 / 评论字段。
package application

import (
	"context"
	"testing"

	"interestBar/pkg/domains/notice/domain"

	"github.com/google/uuid"
)

// fakeUserFacade 每个被查的用户都返回一份展示信息。
type fakeUserFacade struct{}

func (fakeUserFacade) GetBriefs(_ context.Context, userIDs []string) (map[string]UserBrief, error) {
	out := make(map[string]UserBrief, len(userIDs))
	for _, id := range userIDs {
		out[id] = UserBrief{ID: id, Username: "u-" + id, AvatarURL: "https://example.test/" + id}
	}
	return out, nil
}

func TestListNotifications_FollowNotice(t *testing.T) {
	follower := uuid.New()
	repo := &fakeNoticeRepo{page: []domain.Notification{
		{ID: uuid.New(), ActorID: follower, NoticeType: domain.NoticeTypeFollow},
	}}
	posts := &fakePostFacade{}
	svc := NewNoticeService(repo, nil)
	svc.SetUserFacade(fakeUserFacade{})
	svc.SetPostFacade(posts)

	res, err := svc.ListNotifications(context.Background(), uuid.New(), []int16{domain.NoticeTypeFollow}, 20, "")
	if err != nil {
		t.Fatalf("ListNotifications(type=12): %v", err)
	}
	if len(res.Notices) != 1 {
		t.Fatalf("notices = %+v, want 1 item", res.Notices)
	}
	item := res.Notices[0]
	if item.NoticeType != domain.NoticeTypeFollow || item.PostID != "" || item.CommentID != "" || item.CircleID != "" || item.Snippet != "" {
		t.Errorf("item = %+v, want type 12 without post / comment / circle / snippet", item)
	}
	// 关注者照常展示（前端跳关注者主页用 actor.id）
	if item.Actor == nil || item.Actor.ID != follower.String() {
		t.Errorf("actor = %+v, want follower %s", item.Actor, follower)
	}
	if len(posts.calls) != 0 {
		t.Errorf("post facade calls = %v, want none for a follow notice", posts.calls)
	}
}

func TestNoticeTypeFollow_Bounds(t *testing.T) {
	if domain.NoticeTypeMax != domain.NoticeTypeFollow {
		t.Fatalf("NoticeTypeMax = %d, want %d (follow)", domain.NoticeTypeMax, domain.NoticeTypeFollow)
	}
	if domain.HidesActor(domain.NoticeTypeFollow) || domain.IsCircleReviewNotice(domain.NoticeTypeFollow) {
		t.Error("follow notice must show its actor and is not a circle review notice")
	}
	svc := NewNoticeService(&fakeNoticeRepo{}, nil)
	_, err := svc.ListNotifications(context.Background(), uuid.New(), []int16{domain.NoticeTypeMax + 1}, 20, "")
	if !IsInvalidNoticeTypeErr(err) {
		t.Errorf("type %d: err = %v, want invalid notice type", domain.NoticeTypeMax+1, err)
	}
}
