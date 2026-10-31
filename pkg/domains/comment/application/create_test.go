// create_test.go CreateComment 盖楼校验的单元测试（fake repo，不触 DB，#185）。
//
// 覆盖：顶层评论 / 直接回复根评论 / 回复楼内回复的正常路径（含 reply_to_user_id 推导与
// 通知类型），以及 root_id 指向回复、只传 reply_to_id、跨帖 / 跨楼 / 不存在的引用被拒且不落库。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// fakeCreateRepo 在 fakeLocateRepo 基础上记录 Create 入参。
type fakeCreateRepo struct {
	*fakeLocateRepo
	created []*domain.Comment
}

func (f *fakeCreateRepo) Create(ctx context.Context, comment *domain.Comment) error {
	comment.ID = uuid.New()
	f.created = append(f.created, comment)
	return nil
}

// fakeCreatePostLookup 帖子恒可见、已发布、未锁定；记录评论数递增次数。
type fakeCreatePostLookup struct {
	incr int
}

func (f *fakeCreatePostLookup) GetPost(ctx context.Context, postID uuid.UUID) (*PostInfo, error) {
	return &PostInfo{ID: postID, Status: 1}, nil
}

func (f *fakeCreatePostLookup) RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	f.incr++
	return nil
}

func (f *fakeCreatePostLookup) CanView(ctx context.Context, viewerID, postID uuid.UUID) (bool, error) {
	return true, nil
}

// fakeCreatePublisher 记录评论通知的 isReply 标记。
type fakeCreatePublisher struct {
	notices []bool
}

func (f *fakeCreatePublisher) PublishCommentHot(ctx context.Context, postID uuid.UUID, dir int) error {
	return nil
}

func (f *fakeCreatePublisher) PublishCommentInteraction(ctx context.Context, userID, postID uuid.UUID) error {
	return nil
}

func (f *fakeCreatePublisher) PublishCommentNotice(ctx context.Context, userID, postID, commentID uuid.UUID, isReply bool, snippet string) error {
	f.notices = append(f.notices, isReply)
	return nil
}

func (f *fakeCreatePublisher) PublishMentionNotice(ctx context.Context, actorID uuid.UUID, postID, commentID *uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error {
	return nil
}

// 测试固定标识：帖子 P 下根评论 A（作者 rootUser）、A 楼内回复 B（作者 replyUser）；
// 另一帖子 Q 下根评论 X。
var (
	crPostID      = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002b0")
	crOtherPostID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002b1")
	crActorID     = uuid.MustParse("0192a0d0-0000-7000-8000-000000000201")
	crRootUserID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000202")
	crReplyUserID = uuid.MustParse("0192a0d0-0000-7000-8000-000000000203")
	crRootA       = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002a1")
	crReplyB      = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002a2")
	crOtherRootX  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002a3")
	crGhostID     = uuid.MustParse("0192a0d0-0000-7000-8000-0000000002a4")
)

func newCreateFixture() (*commentServiceImpl, *fakeCreateRepo, *fakeCreatePostLookup, *fakeCreatePublisher) {
	repo := &fakeCreateRepo{fakeLocateRepo: &fakeLocateRepo{byID: map[uuid.UUID]*domain.Comment{
		crRootA:      {ID: crRootA, PostID: crPostID, UserID: crRootUserID},
		crReplyB:     {ID: crReplyB, PostID: crPostID, UserID: crReplyUserID, RootID: &[]uuid.UUID{crRootA}[0]},
		crOtherRootX: {ID: crOtherRootX, PostID: crOtherPostID, UserID: crReplyUserID},
	}}}
	lookup := &fakeCreatePostLookup{}
	pub := &fakeCreatePublisher{}
	svc := &commentServiceImpl{repo: repo, postLookup: lookup, publisher: pub}
	return svc, repo, lookup, pub
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

// TestCreateComment_Valid 正常路径：落库字段与通知类型与修复前一致。
func TestCreateComment_Valid(t *testing.T) {
	cases := []struct {
		name          string
		rootID        *uuid.UUID
		replyToID     *uuid.UUID
		wantRootID    *uuid.UUID
		wantReplyToID *uuid.UUID
		wantReplyUser *uuid.UUID
		wantIsReply   bool
	}{
		{"top-level", nil, nil, nil, nil, nil, false},
		{"direct reply to root", ptr(crRootA), nil, ptr(crRootA), nil, ptr(crRootUserID), true},
		{"explicit reply to root", ptr(crRootA), ptr(crRootA), ptr(crRootA), ptr(crRootA), ptr(crRootUserID), true},
		{"reply to reply", ptr(crRootA), ptr(crReplyB), ptr(crRootA), ptr(crReplyB), ptr(crReplyUserID), true},
	}
	eq := func(a, b *uuid.UUID) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, lookup, pub := newCreateFixture()
			id, err := svc.CreateComment(context.Background(), crActorID, CreateCommentInput{
				PostID: crPostID, Content: "hello", RootID: tc.rootID, ReplyToID: tc.replyToID,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.created) != 1 || repo.created[0].ID != id {
				t.Fatalf("expected exactly one created comment with returned id, got %+v", repo.created)
			}
			c := repo.created[0]
			if c.PostID != crPostID || c.UserID != crActorID || c.Content != "hello" {
				t.Fatalf("bad fields: %+v", c)
			}
			if !eq(c.RootID, tc.wantRootID) || !eq(c.ReplyToID, tc.wantReplyToID) || !eq(c.ReplyToUserID, tc.wantReplyUser) {
				t.Fatalf("thread refs root=%v reply_to=%v reply_to_user=%v, want %v %v %v",
					c.RootID, c.ReplyToID, c.ReplyToUserID, tc.wantRootID, tc.wantReplyToID, tc.wantReplyUser)
			}
			if lookup.incr != 1 {
				t.Fatalf("post comment count incr = %d, want 1", lookup.incr)
			}
			if len(pub.notices) != 1 || pub.notices[0] != tc.wantIsReply {
				t.Fatalf("notices = %v, want [%v]", pub.notices, tc.wantIsReply)
			}
		})
	}
}

// TestCreateComment_Invalid 违规引用被拒：返回对应错误，且不落库、不计数、不发通知。
func TestCreateComment_Invalid(t *testing.T) {
	cases := []struct {
		name      string
		rootID    *uuid.UUID
		replyToID *uuid.UUID
		want      error
	}{
		{"root_id is a reply (third-level thread)", ptr(crReplyB), nil, domain.ErrRootCommentNotTopLevel},
		{"root_id is a reply with reply_to", ptr(crReplyB), ptr(crRootA), domain.ErrRootCommentNotTopLevel},
		{"reply_to_id without root_id", nil, ptr(crReplyB), domain.ErrReplyTargetWithoutRoot},
		{"cross-post reply_to_id without root_id", nil, ptr(crOtherRootX), domain.ErrReplyTargetWithoutRoot},
		{"nonexistent reply_to_id without root_id", nil, ptr(crGhostID), domain.ErrReplyTargetWithoutRoot},
		{"root_id not found", ptr(crGhostID), nil, domain.ErrRootCommentNotFound},
		{"root_id in other post", ptr(crOtherRootX), nil, domain.ErrRootCommentMismatch},
		{"reply_to_id not found", ptr(crRootA), ptr(crGhostID), domain.ErrReplyTargetNotFound},
		{"reply_to_id in other post/thread", ptr(crRootA), ptr(crOtherRootX), domain.ErrReplyTargetNotInThread},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, lookup, pub := newCreateFixture()
			_, err := svc.CreateComment(context.Background(), crActorID, CreateCommentInput{
				PostID: crPostID, Content: "hello", RootID: tc.rootID, ReplyToID: tc.replyToID,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(repo.created) != 0 || lookup.incr != 0 || len(pub.notices) != 0 {
				t.Fatalf("rejected comment must have no side effects: created=%d incr=%d notices=%v",
					len(repo.created), lookup.incr, pub.notices)
			}
		})
	}
}
