// Package http 的发帖 handler 单元测试（#110 状态服务端收口）。
//
// handler 接真实 PostService（依赖全为 fake），请求体带任意 status（含越界值）或不带：
// 均 200，落库状态一律已发布，圈子计数 +1，@提及通知照常发出。
// 不启动 hertz server，直接构造 *app.RequestContext（对齐 hertzadapter 测试范式）。
package http

import (
	"context"
	"net/http"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	conf.Config = &conf.AppConfig{}
	conf.Config.Notice.MentionMax = 10
	logger.Log = zap.NewNop()
	m.Run()
}

// 测试固定标识。
var (
	testCircleID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	testAuthorID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	testMentioned = uuid.MustParse("0192a0d0-0000-7000-8000-000000000010")
)

// fakeRepo 仅实现发帖路径用到的 Create / CreateMentions。
type fakeRepo struct {
	domain.PostRepository

	created  []*domain.Post
	mentions int
}

func (r *fakeRepo) Create(_ context.Context, post *domain.Post) error {
	if post.ID == uuid.Nil {
		post.ID = uuid.New()
	}
	r.created = append(r.created, post)
	return nil
}

func (r *fakeRepo) CreateMentions(context.Context, uuid.UUID, []uuid.UUID) error {
	r.mentions++
	return nil
}

// fakePublisher 记录 @提及 通知次数。
type fakePublisher struct {
	domain.PostEventPublisher

	notices int
}

func (p *fakePublisher) PublishMentionNotice(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID, string) error {
	p.notices++
	return nil
}

// fakeUsers 被 @ 用户存在且不是圈内机器人。
type fakeUsers struct{}

func (fakeUsers) GetBriefs(_ context.Context, ids []string) (map[string]application.UserBrief, error) {
	out := make(map[string]application.UserBrief, len(ids))
	for _, id := range ids {
		out[id] = application.UserBrief{ID: id, Username: "u"}
	}
	return out, nil
}

func (fakeUsers) GetBrief(_ context.Context, id string) (*application.UserBrief, error) {
	return &application.UserBrief{ID: id, Username: "u"}, nil
}

func (fakeUsers) GetAgentCircleIDs(context.Context, []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return map[uuid.UUID]uuid.UUID{}, nil
}

type fakeMember struct{}

func (fakeMember) GetMember(context.Context, uuid.UUID, uuid.UUID) (*application.CircleMemberInfo, error) {
	return &application.CircleMemberInfo{Status: 1}, nil
}

type fakeCircleStatus struct{}

func (fakeCircleStatus) IsCircleAvailable(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

type fakeCount struct{ incr int }

func (f *fakeCount) IncrPostCount(context.Context, uuid.UUID) error {
	f.incr++
	return nil
}

// TestCreatePost_IgnoresClientStatus 客户端 status 为 0/2/3/4/越界值或不传，
// 均落库为已发布、圈子计数 +1、@提及通知照常发出。
func TestCreatePost_IgnoresClientStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string // 请求体里 status 的 JSON 片段；空串表示不传
	}{
		{"absent", ""},
		{"draft", `"status":0,`},
		{"reviewing", `"status":2,`},
		{"rejected", `"status":3,`},
		{"blocked", `"status":4,`},
		{"out of range", `"status":99,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			pub := &fakePublisher{}
			count := &fakeCount{}
			svc := application.NewPostService(repo, nil, nil, nil, nil, pub)
			svc.SetUserFacade(fakeUsers{})
			svc.SetMemberChecker(fakeMember{})
			svc.SetStatusChecker(fakeCircleStatus{})
			svc.SetPostCountPort(count)
			h := NewHandler(svc)

			body := `{` + tc.status + `"circle_id":"` + testCircleID.String() + `","title":"标题","content":"正文",` +
				`"mention_user_ids":["` + testMentioned.String() + `"]}`
			rc := &app.RequestContext{}
			rc.Request.SetMethod(http.MethodPost)
			rc.Request.SetBody([]byte(body))
			rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
			ac := hertzadapter.New(context.Background(), rc)
			ac.SetLoginID(testAuthorID.String())

			h.CreatePost(ac)

			if rc.Response.StatusCode() != http.StatusOK {
				t.Fatalf("status code = %d, want 200; body=%s", rc.Response.StatusCode(), rc.Response.Body())
			}
			if len(repo.created) != 1 {
				t.Fatalf("created %d posts, want 1", len(repo.created))
			}
			if got := repo.created[0].Status; got != domain.PostStatusPublished {
				t.Fatalf("post status = %d, want %d (published)", got, domain.PostStatusPublished)
			}
			if count.incr != 1 {
				t.Fatalf("circle post count incr = %d, want 1", count.incr)
			}
			if repo.mentions != 1 || pub.notices != 1 {
				t.Fatalf("mentions saved=%d notices=%d, want 1/1", repo.mentions, pub.notices)
			}
		})
	}
}
