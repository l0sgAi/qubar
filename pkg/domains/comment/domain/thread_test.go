// thread_test.go 盖楼不变量（NewRootComment / NewReply）单元测试（#185）。
package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

var (
	tPostID      = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001b0")
	tOtherPostID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001b1")
	tActorID     = uuid.MustParse("0192a0d0-0000-7000-8000-000000000101")
	tRootUserID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000102")
	tReplyUserID = uuid.MustParse("0192a0d0-0000-7000-8000-000000000103")
	tRootID      = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001a1")
	tOtherRootID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001a2")
	tReplyID     = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001a3")
)

func tParams() NewCommentParams {
	return NewCommentParams{PostID: tPostID, UserID: tActorID, Content: "hi", ExtraData: []byte(`{}`)}
}

func tRoot() *Comment {
	return &Comment{ID: tRootID, PostID: tPostID, UserID: tRootUserID}
}

func tReply(rootID uuid.UUID) *Comment {
	return &Comment{ID: tReplyID, PostID: tPostID, UserID: tReplyUserID, RootID: &rootID}
}

func TestNewRootComment(t *testing.T) {
	c := NewRootComment(tParams())
	if c.PostID != tPostID || c.UserID != tActorID || c.Content != "hi" || string(c.ExtraData) != `{}` {
		t.Fatalf("bad fields: %+v", c)
	}
	if c.RootID != nil || c.ReplyToID != nil || c.ReplyToUserID != nil {
		t.Fatalf("root comment must not carry thread refs: %+v", c)
	}
	if c.Status != CommentStatusNormal || c.Deleted != 0 {
		t.Fatalf("bad status/deleted: %+v", c)
	}
}

// TestNewReply_Valid 合法回复：直接回复根评论 / 显式回复根评论 / 回复楼内回复。
func TestNewReply_Valid(t *testing.T) {
	cases := []struct {
		name          string
		replyTo       *Comment
		wantReplyToID *uuid.UUID
		wantReplyUser uuid.UUID
	}{
		{"direct to root", nil, nil, tRootUserID},
		{"explicit root as reply_to", tRoot(), &tRootID, tRootUserID},
		{"reply to reply in thread", tReply(tRootID), &tReplyID, tReplyUserID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewReply(tParams(), tRoot(), tc.replyTo)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.RootID == nil || *c.RootID != tRootID {
				t.Fatalf("root_id = %v, want %v", c.RootID, tRootID)
			}
			switch {
			case tc.wantReplyToID == nil && c.ReplyToID != nil:
				t.Fatalf("reply_to_id = %v, want nil", *c.ReplyToID)
			case tc.wantReplyToID != nil && (c.ReplyToID == nil || *c.ReplyToID != *tc.wantReplyToID):
				t.Fatalf("reply_to_id = %v, want %v", c.ReplyToID, *tc.wantReplyToID)
			}
			// 回复通知依赖 reply_to_user_id，直接回复根评论时也必须是根评论作者。
			if c.ReplyToUserID == nil || *c.ReplyToUserID != tc.wantReplyUser {
				t.Fatalf("reply_to_user_id = %v, want %v", c.ReplyToUserID, tc.wantReplyUser)
			}
			if c.PostID != tPostID || c.UserID != tActorID || c.Status != CommentStatusNormal {
				t.Fatalf("bad fields: %+v", c)
			}
		})
	}
}

// TestNewReply_Invalid 违规回复各分支返回对应哨兵错误。
func TestNewReply_Invalid(t *testing.T) {
	deletedRoot := tRoot()
	deletedRoot.Deleted = 1
	otherPostRoot := tRoot()
	otherPostRoot.PostID = tOtherPostID
	deletedReply := tReply(tRootID)
	deletedReply.Deleted = 1
	otherPostReply := tReply(tRootID)
	otherPostReply.PostID = tOtherPostID
	otherThreadRoot := &Comment{ID: tOtherRootID, PostID: tPostID, UserID: tReplyUserID}

	cases := []struct {
		name    string
		root    *Comment
		replyTo *Comment
		want    error
	}{
		{"nil root", nil, nil, ErrRootCommentNotFound},
		{"deleted root", deletedRoot, nil, ErrRootCommentNotFound},
		{"root in other post", otherPostRoot, nil, ErrRootCommentMismatch},
		{"root is a reply (third-level thread)", tReply(tRootID), nil, ErrRootCommentNotTopLevel},
		{"root is a reply with reply_to", tReply(tRootID), tRoot(), ErrRootCommentNotTopLevel},
		{"deleted reply target", tRoot(), deletedReply, ErrReplyTargetNotFound},
		{"reply target in other thread", tRoot(), tReply(tOtherRootID), ErrReplyTargetNotInThread},
		{"reply target is another root", tRoot(), otherThreadRoot, ErrReplyTargetNotInThread},
		{"reply target in other post", tRoot(), otherPostReply, ErrReplyTargetNotInThread},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewReply(tParams(), tc.root, tc.replyTo)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if c != nil {
				t.Fatalf("expected nil comment on error, got %+v", c)
			}
		})
	}
}
