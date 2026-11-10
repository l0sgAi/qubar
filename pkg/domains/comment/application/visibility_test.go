// visibility_test.go 评论可见性谓词在读路径上的单元测试（#208，fake repo，不触 DB）。
//
// 覆盖：审核中 / 隐藏的评论在详情、定位、回复列表中按「评论不存在」处理；
// 根评论不可见时整楼不可见（回复的详情 / 定位、回复列表都 404）。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// 不可见的评论统一当作不存在：每组用一个新 repo，按状态改写指定评论。
var invisibleStates = []struct {
	name    string
	status  int16
	deleted int16
}{
	{"review", domain.CommentStatusReview, 0},
	{"hidden", domain.CommentStatusHidden, 0},
	{"deleted", domain.CommentStatusNormal, 1},
}

func repoWithState(id uuid.UUID, status, deleted int16) *fakeLocateRepo {
	repo := newLocateRepo()
	c := *repo.byID[id]
	c.Status, c.Deleted = status, deleted
	repo.byID[id] = &c
	return repo
}

// TestVisibility_InvisibleTarget 目标本身不可见：详情、定位 → ErrCommentNotFound。
func TestVisibility_InvisibleTarget(t *testing.T) {
	ctx := context.Background()
	for _, st := range invisibleStates {
		for _, id := range []uuid.UUID{locTopID, locReplyID} {
			t.Run(st.name, func(t *testing.T) {
				svc := &commentServiceImpl{repo: repoWithState(id, st.status, st.deleted)}
				if _, err := svc.GetCommentDetail(ctx, uuid.Nil, id); !errors.Is(err, domain.ErrCommentNotFound) {
					t.Fatalf("detail of %s comment: want ErrCommentNotFound, got %v", st.name, err)
				}
				if _, err := svc.LocateComment(ctx, uuid.Nil, id, 0, 1); !errors.Is(err, domain.ErrCommentNotFound) {
					t.Fatalf("locate %s comment: want ErrCommentNotFound, got %v", st.name, err)
				}
			})
		}
	}
}

// TestVisibility_InvisibleRootHidesThread 根评论不可见：整楼不可见（回复详情 / 回复定位 / 回复列表 → ErrCommentNotFound）。
func TestVisibility_InvisibleRootHidesThread(t *testing.T) {
	ctx := context.Background()
	for _, st := range invisibleStates {
		t.Run(st.name, func(t *testing.T) {
			svc := &commentServiceImpl{repo: repoWithState(locRootID, st.status, st.deleted)}
			if _, err := svc.GetCommentDetail(ctx, uuid.Nil, locReplyID); !errors.Is(err, domain.ErrCommentNotFound) {
				t.Fatalf("detail of reply under %s root: want ErrCommentNotFound, got %v", st.name, err)
			}
			if _, err := svc.LocateComment(ctx, uuid.Nil, locReplyID, 0, 1); !errors.Is(err, domain.ErrCommentNotFound) {
				t.Fatalf("locate reply under %s root: want ErrCommentNotFound, got %v", st.name, err)
			}
			if _, err := svc.GetReplies(ctx, uuid.Nil, locRootID, 0, 1, ""); !errors.Is(err, domain.ErrCommentNotFound) {
				t.Fatalf("replies of %s root: want ErrCommentNotFound, got %v", st.name, err)
			}
		})
	}
}

// TestVisibility_VisibleThread 对照组：根评论与回复都可见时详情 / 回复列表正常返回。
func TestVisibility_VisibleThread(t *testing.T) {
	ctx := context.Background()
	svc := &commentServiceImpl{repo: newLocateRepo()}
	vo, err := svc.GetCommentDetail(ctx, uuid.Nil, locReplyID)
	if err != nil || vo.ID != locReplyID {
		t.Fatalf("detail of visible reply = %+v, %v", vo, err)
	}
	if _, err := svc.GetReplies(ctx, uuid.Nil, locRootID, 0, 1, ""); err != nil {
		t.Fatalf("replies of visible root: %v", err)
	}
}
