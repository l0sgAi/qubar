// on_post_published_test.go 「帖子已发布」副作用入口（#115）单元测试。
//
// 覆盖：已发布时计数 / 通知 / 机器人触发各一次；名单为空只计数；
// 计数失败不影响通知；按已落库名单触发的入口。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeFailPostCount 递增总是失败，并记录调用次数。
type fakeFailPostCount struct{ calls int }

func (f *fakeFailPostCount) IncrPostCount(context.Context, uuid.UUID) error {
	f.calls++
	return errors.New("redis down")
}

// fakeStoredMentionRepo 返回预设的已落库 @提及 名单。
type fakeStoredMentionRepo struct {
	domain.PostRepository

	stored map[uuid.UUID][]uuid.UUID
	err    error
}

func (r *fakeStoredMentionRepo) GetMentionUserIDsByPostIDs(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return r.stored, r.err
}

func newPublishedPost() *domain.Post {
	return &domain.Post{ID: uuid.New(), CircleID: postCircleID, UserID: mentionActor, Title: "标题", Status: domain.PostStatusPublished}
}

func TestOnPostPublished_AllSideEffectsOnce(t *testing.T) {
	pub := &fakeMentionPublisher{}
	count := &fakePostCount{}
	trigger := &fakeAgentTrigger{}
	svc := &postServiceImpl{publisher: pub, postCountPort: count, agentTrigger: trigger}
	post := newPublishedPost()

	svc.onPostPublished(context.Background(), post, []uuid.UUID{normalUserID})

	if count.incr[postCircleID] != 1 {
		t.Fatalf("post count incr = %d, want 1", count.incr[postCircleID])
	}
	if len(pub.notices) != 1 || pub.notices[0] != post.ID {
		t.Fatalf("notices = %v, want [%s]", pub.notices, post.ID)
	}
	if len(trigger.posts) != 1 || trigger.posts[0] != post.ID {
		t.Fatalf("agent trigger = %v, want [%s]", trigger.posts, post.ID)
	}
}

func TestOnPostPublished_EmptyMentionsOnlyCounts(t *testing.T) {
	pub := &fakeMentionPublisher{}
	count := &fakePostCount{}
	trigger := &fakeAgentTrigger{}
	svc := &postServiceImpl{publisher: pub, postCountPort: count, agentTrigger: trigger}

	svc.onPostPublished(context.Background(), newPublishedPost(), nil)

	if count.incr[postCircleID] != 1 {
		t.Fatalf("post count incr = %d, want 1", count.incr[postCircleID])
	}
	if len(pub.notices) != 0 || len(trigger.posts) != 0 {
		t.Fatalf("notices=%v trigger=%v, want none", pub.notices, trigger.posts)
	}
}

func TestOnPostPublished_CountFailureDoesNotBlockNotice(t *testing.T) {
	pub := &fakeMentionPublisher{}
	count := &fakeFailPostCount{}
	trigger := &fakeAgentTrigger{}
	svc := &postServiceImpl{publisher: pub, postCountPort: count, agentTrigger: trigger}

	svc.onPostPublished(context.Background(), newPublishedPost(), []uuid.UUID{normalUserID})

	if count.calls != 1 {
		t.Fatalf("count calls = %d, want 1", count.calls)
	}
	if len(pub.notices) != 1 || len(trigger.posts) != 1 {
		t.Fatalf("notices=%v trigger=%v, want one each", pub.notices, trigger.posts)
	}
}

func TestOnPostPublishedByStored_UsesStoredMentions(t *testing.T) {
	post := newPublishedPost()
	repo := &fakeStoredMentionRepo{stored: map[uuid.UUID][]uuid.UUID{post.ID: {normalUserID, globalBotID}}}
	pub := &fakeMentionPublisher{}
	count := &fakePostCount{}
	trigger := &fakeAgentTrigger{}
	svc := &postServiceImpl{repo: repo, publisher: pub, postCountPort: count, agentTrigger: trigger}

	svc.onPostPublishedByStored(context.Background(), post)

	if count.incr[postCircleID] != 1 || len(trigger.posts) != 1 || len(pub.notices) != 1 {
		t.Fatalf("count=%v notices=%v trigger=%v", count.incr, pub.notices, trigger.posts)
	}
	if len(pub.targets[0]) != 2 {
		t.Fatalf("notice targets = %v, want stored list of 2", pub.targets[0])
	}
}

func TestOnPostPublishedByStored_LoadErrorStillCounts(t *testing.T) {
	repo := &fakeStoredMentionRepo{err: errors.New("db down")}
	pub := &fakeMentionPublisher{}
	count := &fakePostCount{}
	svc := &postServiceImpl{repo: repo, publisher: pub, postCountPort: count}

	svc.onPostPublishedByStored(context.Background(), newPublishedPost())

	if count.incr[postCircleID] != 1 || len(pub.notices) != 0 {
		t.Fatalf("count=%v notices=%v", count.incr, pub.notices)
	}
}
