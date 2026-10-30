//go:build integration

// post_repo_pg_integration_test.go 帖子仓储对开发栈 PostgreSQL 的集成测试（make test-integration）。
package infrastructure_test

import (
	"errors"
	"testing"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// newPost 插入一条已发布帖子（随机作者 / 圈子），测试结束删除。mutate 可改默认字段。
func newPost(t *testing.T, repo domain.PostRepository, mutate func(*domain.Post)) *domain.Post {
	t.Helper()
	p := &domain.Post{
		CircleID:   sharedomain.NewID(),
		UserID:     sharedomain.NewID(),
		Type:       domain.PostTypeTextImage,
		Title:      "集成测试帖子",
		Summary:    "summary",
		Content:    "content",
		MediaExtra: domain.MediaExtraJSON{},
		Status:     domain.PostStatusPublished,
	}
	if mutate != nil {
		mutate(p)
	}
	if err := repo.Create(t.Context(), p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)
	return p
}

// softDelete 模拟删帖：项目统一用 deleted=1 逻辑删除。
func softDelete(t *testing.T, db *gorm.DB, table string, id uuid.UUID) {
	t.Helper()
	if err := db.Exec("UPDATE "+table+" SET deleted = 1 WHERE id = ?", id).Error; err != nil {
		t.Fatalf("soft delete %s: %v", table, err)
	}
}

func TestPostRepo_CreateGetAndSoftDelete(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	p := newPost(t, repo, func(p *domain.Post) {
		p.MediaExtra = domain.MediaExtraJSON{"https://img.example/1.png", "https://img.example/2.png"}
	})
	if p.ID.Version() != 7 {
		t.Fatalf("Create should assign a UUIDv7 primary key, got version %d", p.ID.Version())
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != p.Title || got.CircleID != p.CircleID || got.UserID != p.UserID {
		t.Fatalf("GetByID mismatch: got %+v", got)
	}
	if len(got.MediaExtra) != 2 || got.MediaExtra[1] != "https://img.example/2.png" {
		t.Fatalf("media_extra jsonb round trip: got %v", got.MediaExtra)
	}
	if got.Deleted != 0 || got.LikeCount != 0 || got.CreateTime.IsZero() {
		t.Fatalf("unexpected defaults: deleted=%d like=%d create=%v", got.Deleted, got.LikeCount, got.CreateTime)
	}

	media, err := repo.GetMediaByPostIDs(ctx, []uuid.UUID{p.ID, sharedomain.NewID()})
	if err != nil {
		t.Fatalf("GetMediaByPostIDs: %v", err)
	}
	if len(media) != 1 || len(media[p.ID]) != 2 {
		t.Fatalf("GetMediaByPostIDs: got %v", media)
	}

	softDelete(t, db, "domains.post", p.ID)
	if _, err := repo.GetByID(ctx, p.ID); !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("GetByID after soft delete: want ErrPostNotFound, got %v", err)
	}
	if _, err := repo.GetByID(ctx, sharedomain.NewID()); !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("GetByID unknown id: want ErrPostNotFound, got %v", err)
	}
}

func TestPostRepo_ListByIDsFiltersDeletedAndUnpublished(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)

	visible := newPost(t, repo, nil)
	deleted := newPost(t, repo, nil)
	softDelete(t, db, "domains.post", deleted.ID)
	// status=0（草稿）不能经 Create 写入：Status 带 gorm default:1，零值被省略、落成 DB 默认 1，故用 UPDATE 构造
	draft := newPost(t, repo, nil)
	if err := db.Exec("UPDATE domains.post SET status = ? WHERE id = ?", domain.PostStatusDraft, draft.ID).Error; err != nil {
		t.Fatalf("set draft: %v", err)
	}
	blocked := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusBlocked })

	posts, err := repo.ListByIDs(t.Context(), []uuid.UUID{visible.ID, deleted.ID, draft.ID, blocked.ID})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != visible.ID {
		ids := make([]uuid.UUID, 0, len(posts))
		for _, p := range posts {
			ids = append(ids, p.ID)
		}
		t.Fatalf("ListByIDs should return only the published, undeleted post %s; got %v", visible.ID, ids)
	}

	if posts, err := repo.ListByIDs(t.Context(), nil); err != nil || posts != nil {
		t.Fatalf("ListByIDs(nil) = %v, %v; want nil, nil", posts, err)
	}
}

// 点赞 / 收藏状态的 DB 回源查询：只认 deleted=0 的有效行。
func TestPostRepo_LikeAndCollectLookups(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	user := sharedomain.NewID()
	liked, unliked, collected, untouched := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()

	insert := func(table string, postID uuid.UUID, deleted int) {
		t.Helper()
		id := sharedomain.NewID()
		if err := db.Exec("INSERT INTO "+table+" (id, user_id, post_id, deleted) VALUES (?, ?, ?, ?)",
			id, user, postID, deleted).Error; err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
		testkit.CleanupRows(t, table, "id", id)
	}
	insert("domains.post_like", liked, 0)
	insert("domains.post_like", unliked, 1)
	insert("domains.post_collect", collected, 0)

	all := []uuid.UUID{liked, unliked, collected, untouched}

	if ok, err := repo.IsLiked(ctx, user, liked); err != nil || !ok {
		t.Fatalf("IsLiked(liked) = %v, %v", ok, err)
	}
	if ok, err := repo.IsLiked(ctx, user, unliked); err != nil || ok {
		t.Fatalf("IsLiked(canceled like) = %v, %v; want false", ok, err)
	}
	likedSet, err := repo.BatchIsLiked(ctx, user, all)
	if err != nil {
		t.Fatalf("BatchIsLiked: %v", err)
	}
	if len(likedSet) != 1 || !likedSet[liked] {
		t.Fatalf("BatchIsLiked = %v, want only %s", likedSet, liked)
	}

	if ok, err := repo.IsCollected(ctx, user, collected); err != nil || !ok {
		t.Fatalf("IsCollected(collected) = %v, %v", ok, err)
	}
	collectedSet, err := repo.BatchIsCollected(ctx, user, all)
	if err != nil {
		t.Fatalf("BatchIsCollected: %v", err)
	}
	if len(collectedSet) != 1 || !collectedSet[collected] {
		t.Fatalf("BatchIsCollected = %v, want only %s", collectedSet, collected)
	}

	// 全量装载（#129）：只列有效行，带交互时间，受 limit 约束
	likes, err := repo.ListActiveLikes(ctx, user, 10)
	if err != nil || len(likes) != 1 || likes[0].ID != liked || likes[0].At.IsZero() {
		t.Fatalf("ListActiveLikes = %+v, %v; want only %s", likes, err, liked)
	}
	collects, err := repo.ListActiveCollects(ctx, user, 10)
	if err != nil || len(collects) != 1 || collects[0].ID != collected {
		t.Fatalf("ListActiveCollects = %+v, %v; want only %s", collects, err, collected)
	}
	if none, err := repo.ListActiveLikes(ctx, user, 0); err != nil || len(none) != 0 {
		t.Fatalf("ListActiveLikes(limit 0) = %+v, %v", none, err)
	}

	// 其他用户看不到
	if ok, err := repo.IsLiked(ctx, sharedomain.NewID(), liked); err != nil || ok {
		t.Fatalf("IsLiked(other user) = %v, %v; want false", ok, err)
	}
}

func TestPostRepo_MentionsIdempotentAndOrdered(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	p := newPost(t, repo, nil)
	testkit.CleanupRows(t, "domains.post_mention", "post_id", p.ID)
	u1, u2 := sharedomain.NewID(), sharedomain.NewID()

	if err := repo.CreateMentions(ctx, p.ID, []uuid.UUID{u1, u2}); err != nil {
		t.Fatalf("CreateMentions: %v", err)
	}
	// 重复写入同一名单：唯一约束 + ON CONFLICT DO NOTHING，幂等
	if err := repo.CreateMentions(ctx, p.ID, []uuid.UUID{u1}); err != nil {
		t.Fatalf("CreateMentions (duplicate): %v", err)
	}

	got, err := repo.GetMentionUserIDsByPostIDs(ctx, []uuid.UUID{p.ID, sharedomain.NewID()})
	if err != nil {
		t.Fatalf("GetMentionUserIDsByPostIDs: %v", err)
	}
	if len(got) != 1 || len(got[p.ID]) != 2 || got[p.ID][0] != u1 || got[p.ID][1] != u2 {
		t.Fatalf("mentions = %v, want [%s %s] in write order", got, u1, u2)
	}
}
