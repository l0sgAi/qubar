// locate_test.go LocateComment 的单元测试（fake repo，不触 DB）。
//
// 覆盖：顶层目标（首页/非首页）、回复目标、目标已删、根已删、
// is_root=1 时 reply 字段固定零值。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// fakeLocateRepo 预置评论与定位结果的 fake CommentRepository。
type fakeLocateRepo struct {
	byID map[uuid.UUID]*domain.Comment

	rootCursor  string // LocateRootCursor 返回
	replyCursor string // LocateReplyCursor 返回
	replyPage   int

	// 记录实际入参，验证 sort/size 透传
	gotRootSort, gotRootSize   int
	gotReplySort, gotReplySize int
}

func (f *fakeLocateRepo) Create(ctx context.Context, comment *domain.Comment) error {
	return nil
}

func (f *fakeLocateRepo) GetByID(ctx context.Context, commentID uuid.UUID) (*domain.Comment, error) {
	if c, ok := f.byID[commentID]; ok {
		return c, nil
	}
	return nil, domain.ErrCommentNotFound
}

func (f *fakeLocateRepo) GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	return nil, "", false, nil
}

func (f *fakeLocateRepo) GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	return nil, "", false, nil
}

func (f *fakeLocateRepo) LocateRootCursor(ctx context.Context, postID uuid.UUID, sort int, target *domain.Comment, size int) (string, error) {
	f.gotRootSort, f.gotRootSize = sort, size
	return f.rootCursor, nil
}

func (f *fakeLocateRepo) LocateReplyCursor(ctx context.Context, rootID uuid.UUID, sort int, target *domain.Comment, size int) (string, int, error) {
	f.gotReplySort, f.gotReplySize = sort, size
	return f.replyCursor, f.replyPage, nil
}

func (f *fakeLocateRepo) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeLocateRepo) BatchCheckLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	return map[uuid.UUID]bool{}, nil
}

func (f *fakeLocateRepo) CreateMentions(ctx context.Context, commentID uuid.UUID, userIDs []uuid.UUID) error {
	return nil
}

func (f *fakeLocateRepo) GetMentionUserIDsByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
}

// 测试固定标识。
var (
	locPostID   = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b0")
	locRootID   = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a1")
	locReplyID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a2")
	locTopID    = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a3")
	locGhostID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a4")
	locOrphanID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a5") // 根已删的回复
	locDelRoot  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a6") // 已删根（不在 byID）
)

func newLocateRepo() *fakeLocateRepo {
	return &fakeLocateRepo{
		byID: map[uuid.UUID]*domain.Comment{
			locRootID: {ID: locRootID, PostID: locPostID},
			locTopID:  {ID: locTopID, PostID: locPostID},
			locReplyID: {ID: locReplyID, PostID: locPostID,
				RootID: &[]uuid.UUID{locRootID}[0]},
			locOrphanID: {ID: locOrphanID, PostID: locPostID,
				RootID: &[]uuid.UUID{locDelRoot}[0]},
		},
	}
}

// TestLocateComment_RootFirstPage 顶层目标在首页：list_cursor=null，reply 字段固定零值。
func TestLocateComment_RootFirstPage(t *testing.T) {
	repo := newLocateRepo() // rootCursor 默认 "" = 首页
	svc := &commentServiceImpl{repo: repo}

	res, err := svc.LocateComment(context.Background(), locTopID, 0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsRoot != 1 || res.RootID != locTopID || res.CommentID != locTopID || res.PostID != locPostID {
		t.Fatalf("bad identity fields: %+v", res)
	}
	if res.ListCursor != nil {
		t.Fatalf("first-page target should have nil ListCursor, got %q", *res.ListCursor)
	}
	if res.ReplyCursor != nil || res.ReplyPage != 0 {
		t.Fatalf("is_root=1 must fix ReplyCursor=nil ReplyPage=0, got %+v", res)
	}
	if repo.gotRootSort != 0 || repo.gotRootSize != rootCommentPageSize {
		t.Fatalf("sort/size not passed through: sort=%d size=%d", repo.gotRootSort, repo.gotRootSize)
	}
}

// TestLocateComment_RootLaterPage 顶层目标在非首页：list_cursor 非空。
func TestLocateComment_RootLaterPage(t *testing.T) {
	repo := newLocateRepo()
	repo.rootCursor = "eyJmb28iOiJiYXIifQ=="
	svc := &commentServiceImpl{repo: repo}

	res, err := svc.LocateComment(context.Background(), locTopID, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ListCursor == nil || *res.ListCursor != repo.rootCursor {
		t.Fatalf("want ListCursor=%q, got %+v", repo.rootCursor, res.ListCursor)
	}
}

// TestLocateComment_Reply 回复目标：root_id 指向根，回复游标/页码回传，replySort 透传。
func TestLocateComment_Reply(t *testing.T) {
	repo := newLocateRepo()
	repo.rootCursor = "bGlzdA=="
	repo.replyCursor = "cmVwbHk="
	repo.replyPage = 3
	svc := &commentServiceImpl{repo: repo}

	res, err := svc.LocateComment(context.Background(), locReplyID, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsRoot != 0 || res.RootID != locRootID {
		t.Fatalf("bad root binding: %+v", res)
	}
	if res.ReplyCursor == nil || *res.ReplyCursor != repo.replyCursor || res.ReplyPage != 3 {
		t.Fatalf("bad reply locate: %+v", res)
	}
	if repo.gotReplySort != 1 || repo.gotReplySize != defaultReplyPageSize {
		t.Fatalf("reply sort/size not passed through: sort=%d size=%d", repo.gotReplySort, repo.gotReplySize)
	}
}

// TestLocateComment_ReplyFirstPage 回复在回复首页：reply_cursor=null 且 reply_page=1。
func TestLocateComment_ReplyFirstPage(t *testing.T) {
	repo := newLocateRepo()
	repo.replyPage = 1 // replyCursor 默认 ""
	svc := &commentServiceImpl{repo: repo}

	res, err := svc.LocateComment(context.Background(), locReplyID, 0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ReplyCursor != nil || res.ReplyPage != 1 {
		t.Fatalf("first-page reply must be ReplyCursor=nil ReplyPage=1, got %+v", res)
	}
}

// TestLocateComment_NotFound 目标不存在/已删除 → ErrCommentNotFound。
func TestLocateComment_NotFound(t *testing.T) {
	svc := &commentServiceImpl{repo: newLocateRepo()}
	_, err := svc.LocateComment(context.Background(), locGhostID, 0, 1)
	if !errors.Is(err, domain.ErrCommentNotFound) {
		t.Fatalf("want ErrCommentNotFound, got %v", err)
	}
}

// TestLocateComment_RootDeleted 回复目标的根评论已删除 → ErrCommentNotFound
// （根删则其下回复在列表中不可达）。
func TestLocateComment_RootDeleted(t *testing.T) {
	svc := &commentServiceImpl{repo: newLocateRepo()}
	_, err := svc.LocateComment(context.Background(), locOrphanID, 0, 1)
	if !errors.Is(err, domain.ErrCommentNotFound) {
		t.Fatalf("want ErrCommentNotFound, got %v", err)
	}
}
