// service_test.go 通知列表组装单元测试：审核类通知（9 / 10）的 circle_id 批量回填（#156）。
package application

import (
	"context"
	"testing"

	"interestBar/pkg/domains/notice/domain"

	"github.com/google/uuid"
)

// fakeNoticeRepo 固定返回一页通知。
type fakeNoticeRepo struct {
	domain.NotificationRepository

	page []domain.Notification
}

func (r *fakeNoticeRepo) ListByCursor(context.Context, uuid.UUID, []int16, int, string) ([]domain.Notification, string, error) {
	return r.page, "", nil
}

// fakePostFacade 记录调用，按 map 返回 帖子 → 圈子。
type fakePostFacade struct {
	circles map[uuid.UUID]uuid.UUID
	calls   [][]uuid.UUID
}

func (f *fakePostFacade) GetCircleIDs(_ context.Context, postIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	f.calls = append(f.calls, postIDs)
	out := make(map[uuid.UUID]uuid.UUID, len(postIDs))
	for _, id := range postIDs {
		if c, ok := f.circles[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func notice(t int16, postID uuid.UUID) domain.Notification {
	return domain.Notification{ID: uuid.New(), ActorID: uuid.New(), NoticeType: t, PostID: &postID}
}

// 一页里的举报 / 疑似自伤通知回填 circle_id（同帖去重、一次批量查询）；查不到的帖子省略字段；其他类型不回填。
func TestListNotifications_FillsReviewCircleID(t *testing.T) {
	reported, selfHarm, liked, missing := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	circleA, circleB := uuid.New(), uuid.New()
	repo := &fakeNoticeRepo{page: []domain.Notification{
		notice(domain.NoticeTypePostReported, reported),
		notice(domain.NoticeTypePostSelfHarm, selfHarm),
		notice(domain.NoticeTypeLikePost, liked),
		notice(domain.NoticeTypePostReported, missing),
		notice(domain.NoticeTypePostSelfHarm, reported),
	}}
	posts := &fakePostFacade{circles: map[uuid.UUID]uuid.UUID{reported: circleA, selfHarm: circleB, liked: circleA}}
	svc := NewNoticeService(repo, nil)
	svc.SetPostFacade(posts)

	res, err := svc.ListNotifications(context.Background(), uuid.New(), nil, 20, "")
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	want := []string{circleA.String(), circleB.String(), "", "", circleA.String()}
	for i, item := range res.Notices {
		if item.CircleID != want[i] {
			t.Fatalf("notice %d (type %d) circle_id = %q, want %q", i, item.NoticeType, item.CircleID, want[i])
		}
	}
	if len(posts.calls) != 1 || len(posts.calls[0]) != 3 {
		t.Fatalf("facade calls = %v, want one batch of 3 distinct review posts", posts.calls)
	}
}

// 没有审核类通知不查 post；未注入 Facade 时照常返回列表、不带 circle_id。
func TestListNotifications_SkipsCircleLookup(t *testing.T) {
	repo := &fakeNoticeRepo{page: []domain.Notification{notice(domain.NoticeTypeLikePost, uuid.New())}}
	posts := &fakePostFacade{}
	svc := NewNoticeService(repo, nil)
	svc.SetPostFacade(posts)
	if _, err := svc.ListNotifications(context.Background(), uuid.New(), nil, 20, ""); err != nil || len(posts.calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want no lookup", err, posts.calls)
	}

	repo.page = []domain.Notification{notice(domain.NoticeTypePostReported, uuid.New())}
	res, err := NewNoticeService(repo, nil).ListNotifications(context.Background(), uuid.New(), nil, 20, "")
	if err != nil || len(res.Notices) != 1 || res.Notices[0].CircleID != "" {
		t.Fatalf("without facade = %+v, %v", res, err)
	}
}
