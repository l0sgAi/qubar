//go:build integration

// comment_visibility_integration_test.go 评论可见性谓词（#208）对开发栈 PostgreSQL 的集成测试：
// 审核中（status=2）/ 隐藏（status=3）的评论不出现在顶层列表与楼内回复里，定位的 rank 计数与取页起点
// 与列表同一口径（定位游标与顺序翻页的页起始游标逐字节一致），GetByID 读出的 status 让详情 / 定位判为不可见。
package infrastructure_test

import (
	"testing"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/domains/comment/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setCommentStatus(t *testing.T, db *gorm.DB, id uuid.UUID, status int16) {
	t.Helper()
	if err := db.Exec("UPDATE domains.comment SET status = ? WHERE id = ?", status, id).Error; err != nil {
		t.Fatalf("set comment status: %v", err)
	}
}

// pageStarts 顺序翻完列表，返回每条评论所在页的起始游标（首页为 ""）与按序的 ID。
func pageStarts(t *testing.T, size int, fetch func(cursor string) ([]domain.Comment, string, bool, error)) (map[uuid.UUID]string, []uuid.UUID) {
	t.Helper()
	starts := map[uuid.UUID]string{}
	var ids []uuid.UUID
	cursor := ""
	for pages := 1; ; pages++ {
		items, next, hasMore, err := fetch(cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, c := range items {
			starts[c.ID] = cursor
			ids = append(ids, c.ID)
		}
		if !hasMore {
			return starts, ids
		}
		if pages > 100 {
			t.Fatal("pagination does not terminate")
		}
		cursor = next
	}
}

func TestCommentRepo_VisibleScope(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	ctx := t.Context()
	postID := sharedomain.NewID()
	const size = 3

	// 顶层：可见与不可见交错，不可见的点赞更多 / 更新，若计入 rank 会把目标挤到后面的页。
	visibleRoots := map[uuid.UUID]bool{}
	invisible := map[uuid.UUID]bool{}
	var thread *domain.Comment
	for i := range 18 {
		c := newComment(t, repo, postID, nil, i%4)
		switch i % 3 {
		case 1:
			setCommentStatus(t, db, c.ID, domain.CommentStatusReview)
			invisible[c.ID] = true
		case 2:
			setCommentStatus(t, db, c.ID, domain.CommentStatusHidden)
			invisible[c.ID] = true
		default:
			visibleRoots[c.ID] = true
			thread = c
		}
	}
	// 楼内回复同样交错。
	visibleReplies := map[uuid.UUID]bool{}
	for i := range 15 {
		c := newComment(t, repo, postID, &thread.ID, i%3)
		switch i % 3 {
		case 0:
			setCommentStatus(t, db, c.ID, domain.CommentStatusReview)
			invisible[c.ID] = true
		case 1:
			setCommentStatus(t, db, c.ID, domain.CommentStatusHidden)
			invisible[c.ID] = true
		default:
			visibleReplies[c.ID] = true
		}
	}
	// 不可见根评论下的可见回复：根评论不出现在顶层列表（整楼不可见由 application 层判定）。
	hiddenRoot := newComment(t, repo, postID, nil, 99)
	newComment(t, repo, postID, &hiddenRoot.ID, 0)
	setCommentStatus(t, db, hiddenRoot.ID, domain.CommentStatusHidden)
	invisible[hiddenRoot.ID] = true

	for _, sort := range []int{sortByLikes, sortByTime} {
		rootStarts, rootIDs := pageStarts(t, size, func(cursor string) ([]domain.Comment, string, bool, error) {
			return repo.GetRootCommentsByCursor(ctx, postID, size, sort, cursor)
		})
		assertExactly(t, "root list", sort, rootIDs, visibleRoots)
		for id := range visibleRoots {
			target, err := repo.GetByID(ctx, id)
			if err != nil {
				t.Fatalf("GetByID: %v", err)
			}
			cursor, err := repo.LocateRootCursor(ctx, postID, sort, target, size)
			if err != nil {
				t.Fatalf("LocateRootCursor(sort=%d): %v", sort, err)
			}
			if cursor != rootStarts[id] {
				t.Fatalf("sort=%d: located root cursor %q != list page start %q", sort, cursor, rootStarts[id])
			}
		}

		replyStarts, replyIDs := pageStarts(t, size, func(cursor string) ([]domain.Comment, string, bool, error) {
			return repo.GetRepliesByCursor(ctx, thread.ID, size, sort, cursor)
		})
		assertExactly(t, "reply list", sort, replyIDs, visibleReplies)
		for id := range visibleReplies {
			target, err := repo.GetByID(ctx, id)
			if err != nil {
				t.Fatalf("GetByID: %v", err)
			}
			cursor, _, err := repo.LocateReplyCursor(ctx, thread.ID, sort, target, size)
			if err != nil {
				t.Fatalf("LocateReplyCursor(sort=%d): %v", sort, err)
			}
			if cursor != replyStarts[id] {
				t.Fatalf("sort=%d: located reply cursor %q != list page start %q", sort, cursor, replyStarts[id])
			}
		}
	}

	// 详情 / 定位的判定依据：GetByID 读出真实 status，Visible 为 false。
	for id := range invisible {
		c, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID(invisible): %v", err)
		}
		if c.Visible() || c.Status == domain.CommentStatusNormal {
			t.Fatalf("comment %s with status %d reported visible", id, c.Status)
		}
	}
	for id := range visibleRoots {
		c, err := repo.GetByID(ctx, id)
		if err != nil || !c.Visible() {
			t.Fatalf("visible root %s: Visible()=false, err=%v", id, err)
		}
	}
}

// assertExactly 列表恰好是 want 集合（无重复、无不可见评论、不漏）。
func assertExactly(t *testing.T, what string, sort int, got []uuid.UUID, want map[uuid.UUID]bool) {
	t.Helper()
	seen := map[uuid.UUID]bool{}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("sort=%d: %s contains invisible or foreign comment %s", sort, what, id)
		}
		if seen[id] {
			t.Fatalf("sort=%d: %s repeats %s", sort, what, id)
		}
		seen[id] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("sort=%d: %s has %d comments, want %d", sort, what, len(seen), len(want))
	}
}
