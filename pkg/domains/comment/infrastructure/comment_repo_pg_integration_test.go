//go:build integration

// comment_repo_pg_integration_test.go 评论仓储对开发栈 PostgreSQL 的集成测试（make test-integration）：
// 回复计数事务、逻辑删除过滤、两种排序的 keyset 翻页、定位游标与游标 v2 的作用域校验。
package infrastructure_test

import (
	"encoding/base64"
	"errors"
	"strconv"
	"testing"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/domains/comment/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

const (
	sortByLikes = 0
	sortByTime  = 1
)

// newComment 插入一条评论（root 为 nil 即顶层评论），测试结束删除。
func newComment(t *testing.T, repo domain.CommentRepository, postID uuid.UUID, root *uuid.UUID, likes int) *domain.Comment {
	t.Helper()
	c := &domain.Comment{
		PostID:    postID,
		UserID:    sharedomain.NewID(),
		RootID:    root,
		ReplyToID: root,
		Content:   "集成测试评论",
		ExtraData: []byte("{}"),
		LikeCount: likes,
		Status:    domain.CommentStatusNormal,
	}
	if err := repo.Create(t.Context(), c); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	testkit.CleanupRows(t, "domains.comment", "id", c.ID)
	return c
}

func softDeleteComment(t *testing.T, db *gorm.DB, id uuid.UUID) {
	t.Helper()
	if err := db.Exec("UPDATE domains.comment SET deleted = 1 WHERE id = ?", id).Error; err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}
}

// pageAll 按游标翻完整个列表，返回顺序 ID 与页数；顺带校验每页 hasMore / cursor 一致。
func pageAll(t *testing.T, size int, fetch func(cursor string) ([]domain.Comment, string, bool, error)) ([]uuid.UUID, int) {
	t.Helper()
	var ids []uuid.UUID
	cursor, pages := "", 0
	for {
		items, next, hasMore, err := fetch(cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages+1, err)
		}
		pages++
		if len(items) > size {
			t.Fatalf("page %d has %d items, size %d", pages, len(items), size)
		}
		for _, c := range items {
			ids = append(ids, c.ID)
		}
		if hasMore != (next != "") {
			t.Fatalf("page %d: hasMore=%v but cursor=%q", pages, hasMore, next)
		}
		if !hasMore {
			return ids, pages
		}
		if pages > 100 {
			t.Fatal("pagination does not terminate")
		}
		cursor = next
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCommentRepo_CreateReplyCountAndSoftDelete(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	ctx := t.Context()
	postID := sharedomain.NewID()

	root := newComment(t, repo, postID, nil, 0)
	if root.ID.Version() != 7 {
		t.Fatalf("Create should assign a UUIDv7 primary key, got version %d", root.ID.Version())
	}
	newComment(t, repo, postID, &root.ID, 0)
	reply := newComment(t, repo, postID, &root.ID, 0)

	got, err := repo.GetByID(ctx, root.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ReplyCount != 2 {
		t.Fatalf("root reply_count = %d, want 2 (incremented in the create transaction)", got.ReplyCount)
	}
	if got.RootID != nil {
		t.Fatalf("root comment has root_id %v", got.RootID)
	}

	softDeleteComment(t, db, reply.ID)
	if _, err := repo.GetByID(ctx, reply.ID); !errors.Is(err, domain.ErrCommentNotFound) {
		t.Fatalf("GetByID after soft delete: want ErrCommentNotFound, got %v", err)
	}
	replies, _, hasMore, err := repo.GetRepliesByCursor(ctx, root.ID, 10, sortByTime, "")
	if err != nil {
		t.Fatalf("GetRepliesByCursor: %v", err)
	}
	if len(replies) != 1 || hasMore || replies[0].ID == reply.ID {
		t.Fatalf("replies after soft delete = %d items (hasMore=%v), want 1 without the deleted reply", len(replies), hasMore)
	}
}

// 顶层评论按时间倒序（id DESC，UUIDv7 即时间序）翻页：不重不漏、顺序严格、排除回复与已删评论。
func TestCommentRepo_RootKeysetByTime(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	postID := sharedomain.NewID()

	var created []uuid.UUID
	for range 7 {
		created = append(created, newComment(t, repo, postID, nil, 0).ID)
	}
	newComment(t, repo, postID, &created[0], 0) // 回复不出现在顶层列表
	softDeleteComment(t, db, created[3])        // 已删不出现
	newComment(t, repo, sharedomain.NewID(), nil, 0)

	var want []uuid.UUID
	for i := len(created) - 1; i >= 0; i-- {
		if i != 3 {
			want = append(want, created[i])
		}
	}

	got, pages := pageAll(t, 2, func(cursor string) ([]domain.Comment, string, bool, error) {
		return repo.GetRootCommentsByCursor(t.Context(), postID, 2, sortByTime, cursor)
	})
	if !equalIDs(got, want) {
		t.Fatalf("time-desc pages = %v\nwant %v", got, want)
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (6 items, size 2)", pages)
	}

	if _, _, _, err := repo.GetRootCommentsByCursor(t.Context(), postID, 2, sortByTime, "not-base64!"); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("invalid cursor: want ErrInvalidCursor, got %v", err)
	}
}

// 按点赞倒序：复合键 (like_count DESC, id DESC)，同赞数跨页不重不漏。
func TestCommentRepo_RootKeysetByLikesWithTies(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	postID := sharedomain.NewID()

	likes := []int{5, 1, 5, 3, 5, 1, 0}
	type row struct {
		id    uuid.UUID
		likes int
	}
	rows := make([]row, 0, len(likes))
	for _, n := range likes {
		rows = append(rows, row{newComment(t, repo, postID, nil, n).ID, n})
	}

	// 期望顺序：like_count DESC，同赞 id DESC（后创建的在前）
	var want []uuid.UUID
	for _, n := range []int{5, 3, 1, 0} {
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].likes == n {
				want = append(want, rows[i].id)
			}
		}
	}

	for _, size := range []int{1, 2, 3, 10} {
		got, _ := pageAll(t, size, func(cursor string) ([]domain.Comment, string, bool, error) {
			return repo.GetRootCommentsByCursor(t.Context(), postID, size, sortByLikes, cursor)
		})
		if !equalIDs(got, want) {
			t.Fatalf("size=%d like-desc pages = %v\nwant %v", size, got, want)
		}
	}
}

// 定位游标：作为列表 cursor 传入时返回页必须包含目标，且与顺序翻页的分页边界一致。
func TestCommentRepo_LocateCursorMatchesListPages(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	ctx := t.Context()
	postID := sharedomain.NewID()
	const size = 3

	root := newComment(t, repo, postID, nil, 0)
	var roots []*domain.Comment
	for i := range 8 {
		roots = append(roots, newComment(t, repo, postID, nil, i%3))
	}
	var replies []*domain.Comment
	for i := range 7 {
		replies = append(replies, newComment(t, repo, postID, &root.ID, i%2))
	}

	for _, sort := range []int{sortByLikes, sortByTime} {
		rootCursors := pageStartCursors(t, func(cursor string) ([]domain.Comment, string, bool, error) {
			return repo.GetRootCommentsByCursor(ctx, postID, size, sort, cursor)
		})
		for _, target := range roots {
			cursor, err := repo.LocateRootCursor(ctx, postID, sort, target, size)
			if err != nil {
				t.Fatalf("LocateRootCursor(sort=%d): %v", sort, err)
			}
			page, _, _, err := repo.GetRootCommentsByCursor(ctx, postID, size, sort, cursor)
			if err != nil {
				t.Fatalf("GetRootCommentsByCursor(located cursor): %v", err)
			}
			if !containsID(page, target.ID) {
				t.Fatalf("sort=%d: located root page does not contain target %s", sort, target.ID)
			}
			// 逐字节一致：定位游标 == 顺序翻页时拿到的、目标所在页的起始游标（上一页的 next_cursor）
			if want := rootCursors[target.ID]; cursor != want {
				t.Fatalf("sort=%d: located root cursor %q != list cursor %q", sort, cursor, want)
			}
		}
		replyCursors := pageStartCursors(t, func(cursor string) ([]domain.Comment, string, bool, error) {
			return repo.GetRepliesByCursor(ctx, root.ID, size, sort, cursor)
		})
		for _, target := range replies {
			cursor, pageNo, err := repo.LocateReplyCursor(ctx, root.ID, sort, target, size)
			if err != nil {
				t.Fatalf("LocateReplyCursor(sort=%d): %v", sort, err)
			}
			page, _, _, err := repo.GetRepliesByCursor(ctx, root.ID, size, sort, cursor)
			if err != nil {
				t.Fatalf("GetRepliesByCursor(located cursor): %v", err)
			}
			if !containsID(page, target.ID) {
				t.Fatalf("sort=%d: located reply page %d does not contain target %s", sort, pageNo, target.ID)
			}
			if (pageNo == 1) != (cursor == "") {
				t.Fatalf("sort=%d: page %d with cursor %q", sort, pageNo, cursor)
			}
			if want := replyCursors[target.ID]; cursor != want {
				t.Fatalf("sort=%d: located reply cursor %q != list cursor %q", sort, cursor, want)
			}
		}
	}
}

// pageStartCursors 顺序翻完整个列表，返回 评论ID → 它所在页的起始游标（首页为 ""）。
func pageStartCursors(t *testing.T, fetch func(cursor string) ([]domain.Comment, string, bool, error)) map[uuid.UUID]string {
	t.Helper()
	starts := make(map[uuid.UUID]string)
	cursor := ""
	for range 100 {
		items, next, hasMore, err := fetch(cursor)
		if err != nil {
			t.Fatalf("list with cursor %q: %v", cursor, err)
		}
		for _, c := range items {
			starts[c.ID] = cursor
		}
		if !hasMore {
			return starts
		}
		cursor = next
	}
	t.Fatal("pagination does not terminate")
	return nil
}

// 游标 v2 的作用域校验（#209）：游标只在生成它的帖子 / 楼、同一排序下有效；v1 游标（无作用域信息）仍可用。
func TestCommentRepo_CursorScopeAndV1Compat(t *testing.T) {
	t.Parallel()
	repo := infrastructure.NewCommentRepository(testkit.PG(t))
	ctx := t.Context()
	postA, postB := sharedomain.NewID(), sharedomain.NewID()
	const size = 2

	var rootsA []*domain.Comment
	for i := range 5 {
		rootsA = append(rootsA, newComment(t, repo, postA, nil, i))
	}
	for range 3 {
		newComment(t, repo, postB, nil, 0)
	}
	rootA, otherRootA := rootsA[0], rootsA[1]
	for i := range 4 {
		newComment(t, repo, postA, &rootA.ID, i)
		newComment(t, repo, postA, &otherRootA.ID, i)
	}

	for _, sort := range []int{sortByLikes, sortByTime} {
		otherSort := 1 - sort
		page1, rootCursor, hasMore, err := repo.GetRootCommentsByCursor(ctx, postA, size, sort, "")
		if err != nil || !hasMore || rootCursor == "" {
			t.Fatalf("sort=%d: root first page: cursor=%q hasMore=%v err=%v", sort, rootCursor, hasMore, err)
		}
		_, replyCursor, hasMore, err := repo.GetRepliesByCursor(ctx, rootA.ID, size, sort, "")
		if err != nil || !hasMore || replyCursor == "" {
			t.Fatalf("sort=%d: reply first page: cursor=%q hasMore=%v err=%v", sort, replyCursor, hasMore, err)
		}

		// 同作用域同排序：正常翻页
		page2, _, _, err := repo.GetRootCommentsByCursor(ctx, postA, size, sort, rootCursor)
		if err != nil || len(page2) != size {
			t.Fatalf("sort=%d: root second page: %d items, err=%v", sort, len(page2), err)
		}
		if _, _, _, err := repo.GetRepliesByCursor(ctx, rootA.ID, size, sort, replyCursor); err != nil {
			t.Fatalf("sort=%d: reply second page: %v", sort, err)
		}

		// 跨帖 / 跨楼 / 顶层与楼内混用 / 换排序：一律 ErrInvalidCursor
		rejected := map[string]error{}
		_, _, _, rejected["跨帖"] = repo.GetRootCommentsByCursor(ctx, postB, size, sort, rootCursor)
		_, _, _, rejected["跨楼"] = repo.GetRepliesByCursor(ctx, otherRootA.ID, size, sort, replyCursor)
		_, _, _, rejected["顶层游标用于楼内"] = repo.GetRepliesByCursor(ctx, rootA.ID, size, sort, rootCursor)
		_, _, _, rejected["楼内游标用于顶层"] = repo.GetRootCommentsByCursor(ctx, postA, size, sort, replyCursor)
		_, _, _, rejected["顶层换排序"] = repo.GetRootCommentsByCursor(ctx, postA, size, otherSort, rootCursor)
		_, _, _, rejected["楼内换排序"] = repo.GetRepliesByCursor(ctx, rootA.ID, size, otherSort, replyCursor)
		for name, err := range rejected {
			if !errors.Is(err, domain.ErrInvalidCursor) {
				t.Fatalf("sort=%d %s: want ErrInvalidCursor, got %v", sort, name, err)
			}
		}

		// v1 游标（v2 上线前的格式，前端可能还持有）：与 v2 游标翻到同一页
		last := page1[len(page1)-1]
		v1 := `{"id":"` + last.ID.String() + `"}`
		if sort == sortByLikes {
			v1 = `{"id":"` + last.ID.String() + `","like_count":` + strconv.Itoa(last.LikeCount) + `}`
		}
		v1Page, _, _, err := repo.GetRootCommentsByCursor(ctx, postA, size, sort, base64.StdEncoding.EncodeToString([]byte(v1)))
		if err != nil {
			t.Fatalf("sort=%d: v1 cursor: %v", sort, err)
		}
		if !equalIDs(commentIDs(v1Page), commentIDs(page2)) {
			t.Fatalf("sort=%d: v1 cursor page = %v, want %v", sort, commentIDs(v1Page), commentIDs(page2))
		}
	}
}

func commentIDs(items []domain.Comment) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	return ids
}

func containsID(items []domain.Comment, id uuid.UUID) bool {
	for _, c := range items {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestCommentRepo_LikeLookupsAndMentions(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	ctx := t.Context()
	postID, user := sharedomain.NewID(), sharedomain.NewID()

	liked := newComment(t, repo, postID, nil, 1)
	canceled := newComment(t, repo, postID, nil, 0)
	other := newComment(t, repo, postID, nil, 0)
	for _, l := range []struct {
		comment uuid.UUID
		deleted int
	}{{liked.ID, domain.CommentLikeActive}, {canceled.ID, domain.CommentLikeCanceled}} {
		id := sharedomain.NewID()
		if err := db.Exec("INSERT INTO domains.comment_like (id, user_id, comment_id, post_id, deleted) VALUES (?, ?, ?, ?, ?)",
			id, user, l.comment, postID, l.deleted).Error; err != nil {
			t.Fatalf("insert comment_like: %v", err)
		}
		testkit.CleanupRows(t, "domains.comment_like", "id", id)
	}

	if ok, err := repo.IsLiked(ctx, user, liked.ID); err != nil || !ok {
		t.Fatalf("IsLiked(liked) = %v, %v", ok, err)
	}
	if ok, err := repo.IsLiked(ctx, user, canceled.ID); err != nil || ok {
		t.Fatalf("IsLiked(canceled) = %v, %v; want false", ok, err)
	}
	if likes, err := repo.ListActiveLikes(ctx, user, 10); err != nil || len(likes) != 1 || likes[0].CommentID != liked.ID || likes[0].UpdateTime.IsZero() {
		t.Fatalf("ListActiveLikes = %+v, %v; want only %s", likes, err, liked.ID)
	}
	set, err := repo.BatchCheckLiked(ctx, user, []uuid.UUID{liked.ID, canceled.ID, other.ID})
	if err != nil {
		t.Fatalf("BatchCheckLiked: %v", err)
	}
	if len(set) != 1 || !set[liked.ID] {
		t.Fatalf("BatchCheckLiked = %v, want only %s", set, liked.ID)
	}

	testkit.CleanupRows(t, "domains.comment_mention", "comment_id", liked.ID)
	u1, u2 := sharedomain.NewID(), sharedomain.NewID()
	if err := repo.CreateMentions(ctx, liked.ID, []uuid.UUID{u1, u2}); err != nil {
		t.Fatalf("CreateMentions: %v", err)
	}
	if err := repo.CreateMentions(ctx, liked.ID, []uuid.UUID{u2}); err != nil {
		t.Fatalf("CreateMentions (duplicate): %v", err)
	}
	mentions, err := repo.GetMentionUserIDsByCommentIDs(ctx, []uuid.UUID{liked.ID, other.ID})
	if err != nil {
		t.Fatalf("GetMentionUserIDsByCommentIDs: %v", err)
	}
	if len(mentions) != 1 || !equalIDs(mentions[liked.ID], []uuid.UUID{u1, u2}) {
		t.Fatalf("mentions = %v, want [%s %s]", mentions, u1, u2)
	}
}
