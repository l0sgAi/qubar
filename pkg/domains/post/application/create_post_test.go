// create_post_test.go 发帖状态服务端收口（#110）单元测试。
//
// 覆盖：新帖一律落库为已发布（入参不再含状态）、圈子帖子计数 +1、
// @提及按最终名单落库并发通知、触发 AI 机器人回调。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeCreatePostRepo 仅实现发帖路径用到的 Create / CreateMentions。
type fakeCreatePostRepo struct {
	domain.PostRepository

	created  []*domain.Post
	mentions map[uuid.UUID][]uuid.UUID
}

func (r *fakeCreatePostRepo) Create(_ context.Context, post *domain.Post) error {
	if post.ID == uuid.Nil {
		post.ID = uuid.New()
	}
	r.created = append(r.created, post)
	return nil
}

func (r *fakeCreatePostRepo) CreateMentions(_ context.Context, postID uuid.UUID, userIDs []uuid.UUID) error {
	if r.mentions == nil {
		r.mentions = make(map[uuid.UUID][]uuid.UUID)
	}
	r.mentions[postID] = append(r.mentions[postID], userIDs...)
	return nil
}

// fakeMentionPublisher 记录 @提及 通知事件。
type fakeMentionPublisher struct {
	domain.PostEventPublisher

	notices []uuid.UUID // 被通知的帖子 ID
	targets [][]uuid.UUID
}

func (p *fakeMentionPublisher) PublishMentionNotice(_ context.Context, _, postID uuid.UUID, mentionUserIDs []uuid.UUID, _ string) error {
	p.notices = append(p.notices, postID)
	p.targets = append(p.targets, mentionUserIDs)
	return nil
}

// fakeNormalMember 永远返回状态正常的成员。
type fakeNormalMember struct{}

func (fakeNormalMember) GetMember(context.Context, uuid.UUID, uuid.UUID) (*CircleMemberInfo, error) {
	return &CircleMemberInfo{Status: 1}, nil
}

// fakeAvailableCircle 圈子永远可用。
type fakeAvailableCircle struct{}

func (fakeAvailableCircle) IsCircleAvailable(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

// fakePostCount 记录圈子帖子计数递增。
type fakePostCount struct{ incr map[uuid.UUID]int }

func (f *fakePostCount) IncrPostCount(_ context.Context, circleID uuid.UUID) error {
	if f.incr == nil {
		f.incr = make(map[uuid.UUID]int)
	}
	f.incr[circleID]++
	return nil
}

// fakeAgentTrigger 记录机器人触发。
type fakeAgentTrigger struct{ posts []uuid.UUID }

func (f *fakeAgentTrigger) OnPostMentioned(postID, _, _ uuid.UUID, _ []uuid.UUID) {
	f.posts = append(f.posts, postID)
}

// TestCreatePost_AlwaysPublished 新帖一律落库为已发布：圈子计数 +1，
// @提及落库并发通知，机器人回调照常触发。
func TestCreatePost_AlwaysPublished(t *testing.T) {
	repo := &fakeCreatePostRepo{}
	pub := &fakeMentionPublisher{}
	count := &fakePostCount{}
	trigger := &fakeAgentTrigger{}

	svc := NewPostService(repo, nil, nil, nil, nil, pub)
	svc.SetUserFacade(newMentionFacade())
	svc.SetMemberChecker(fakeNormalMember{})
	svc.SetStatusChecker(fakeAvailableCircle{})
	svc.SetPostCountPort(count)
	svc.SetAgentTrigger(trigger)

	postID, err := svc.CreatePost(context.Background(), mentionActor, CreatePostInput{
		CircleID:       postCircleID,
		Title:          "标题",
		Content:        "正文 @普通用户",
		MentionUserIDs: []uuid.UUID{normalUserID},
	})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("created %d posts, want 1", len(repo.created))
	}
	if got := repo.created[0].Status; got != domain.PostStatusPublished {
		t.Fatalf("status = %d, want %d (published)", got, domain.PostStatusPublished)
	}
	if repo.created[0].ID != postID {
		t.Fatalf("returned id %s != created id %s", postID, repo.created[0].ID)
	}
	if count.incr[postCircleID] != 1 {
		t.Fatalf("circle post count incr = %d, want 1", count.incr[postCircleID])
	}
	if got := repo.mentions[postID]; len(got) != 1 || got[0] != normalUserID {
		t.Fatalf("saved mentions = %v, want [normalUser]", got)
	}
	if len(pub.notices) != 1 || pub.notices[0] != postID {
		t.Fatalf("mention notices = %v, want [%s]", pub.notices, postID)
	}
	if len(pub.targets[0]) != 1 || pub.targets[0][0] != normalUserID {
		t.Fatalf("notice targets = %v, want [normalUser]", pub.targets[0])
	}
	if len(trigger.posts) != 1 || trigger.posts[0] != postID {
		t.Fatalf("agent trigger = %v, want [%s]", trigger.posts, postID)
	}
}

// TestCreatePost_RequiresCircleAndTitle 不再有草稿分支：缺圈子/标题一律拒绝且不落库、不计数。
func TestCreatePost_RequiresCircleAndTitle(t *testing.T) {
	cases := []struct {
		name  string
		input CreatePostInput
		want  error
	}{
		{"missing circle", CreatePostInput{Title: "标题"}, errCircleIDRequired},
		{"missing title", CreatePostInput{CircleID: postCircleID}, errTitleRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeCreatePostRepo{}
			count := &fakePostCount{}
			svc := NewPostService(repo, nil, nil, nil, nil, &fakeMentionPublisher{})
			svc.SetMemberChecker(fakeNormalMember{})
			svc.SetStatusChecker(fakeAvailableCircle{})
			svc.SetPostCountPort(count)

			if _, err := svc.CreatePost(context.Background(), mentionActor, tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(repo.created) != 0 || len(count.incr) != 0 {
				t.Fatalf("must not persist or count on validation error: created=%d incr=%v", len(repo.created), count.incr)
			}
		})
	}
}
