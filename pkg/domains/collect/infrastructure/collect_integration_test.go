//go:build integration

// collect_integration_test.go 收藏流水仓储（PG）与收藏 Lua 缓存（Redis）对开发栈的集成测试（make test-integration）。
package infrastructure_test

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"interestBar/pkg/domains/collect/domain"
	"interestBar/pkg/domains/collect/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// insertPost 直接插一条帖子（collect 领域不持有 post 仓储，keyword 过滤会 JOIN domains.post）。
func insertPost(t *testing.T, db *gorm.DB, title string, status, deleted int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec(`INSERT INTO domains.post (id, circle_id, user_id, title, summary, status, deleted)
		VALUES (?, ?, ?, ?, '', ?, ?)`, id, sharedomain.NewID(), sharedomain.NewID(), title, status, deleted).Error; err != nil {
		t.Fatalf("insert post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", id)
	return id
}

// 行状态迁移：changed 只在 0→1 / 1→0 时为 true（计数与事件依赖它，必须幂等）。
func TestCollectRepo_SetCollectedTransitions(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostCollectRepository(db)
	ctx := t.Context()
	user, postID := sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_collect", "user_id", user)

	for i, s := range []struct {
		active      bool
		wantChanged bool
	}{
		{false, false}, // 无行时取消：no-op
		{true, true},   // 新建
		{true, false},  // 已收藏：no-op
		{false, true},  // 取消
		{false, false}, // 已取消：no-op
		{true, true},   // 复活同一行
	} {
		changed, err := repo.SetCollected(ctx, user, postID, s.active)
		if err != nil {
			t.Fatalf("step %d SetCollected(%v): %v", i, s.active, err)
		}
		if changed != s.wantChanged {
			t.Fatalf("step %d SetCollected(%v) changed = %v, want %v", i, s.active, changed, s.wantChanged)
		}
		if ok, err := repo.IsCollected(ctx, user, postID); err != nil || ok != s.active {
			t.Fatalf("step %d IsCollected = %v, %v; want %v", i, ok, err, s.active)
		}
	}

	var rows int64
	if err := db.Table("domains.post_collect").Where("user_id = ? AND post_id = ?", user, postID).Count(&rows).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("post_collect rows = %d, want 1 (revive must reuse the row)", rows)
	}
}

// 并发收藏同一帖：唯一索引 + ON CONFLICT 保证只有一次 changed=true。
func TestCollectRepo_SetCollectedConcurrent(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostCollectRepository(db)
	user, postID := sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_collect", "user_id", user)

	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	changedCount := 0
	var firstErr error
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, err := repo.SetCollected(t.Context(), user, postID, true)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if changed {
				changedCount++
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("SetCollected: %v", firstErr)
	}
	if changedCount != 1 {
		t.Fatalf("changed=true %d times, want exactly 1", changedCount)
	}
}

// 「我的收藏」keyset 翻页：按收藏时间倒序、不重不漏；keyword 过滤只算已发布未删帖。
func TestCollectRepo_ListCollectedPostIDsKeyset(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostCollectRepository(db)
	ctx := t.Context()
	user := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_collect", "user_id", user)

	const keyword = "集成测试关键字"
	var collected []uuid.UUID
	for i := range 5 {
		title := "普通帖子 " + strconv.Itoa(i)
		if i%2 == 0 {
			title = keyword + " " + strconv.Itoa(i)
		}
		collected = append(collected, insertPost(t, db, title, 1, 0))
	}
	deletedPost := insertPost(t, db, keyword+" deleted", 1, 1)
	canceled := insertPost(t, db, keyword+" canceled", 1, 0)
	for _, id := range append(append([]uuid.UUID{}, collected...), deletedPost, canceled) {
		if _, err := repo.SetCollected(ctx, user, id, true); err != nil {
			t.Fatalf("SetCollected: %v", err)
		}
	}
	if _, err := repo.SetCollected(ctx, user, canceled, false); err != nil {
		t.Fatalf("cancel collect: %v", err)
	}

	// 无关键字：有效收藏 6 条（含已删帖，失效帖由 post 侧 ListByIDs 过滤），最近收藏在前
	want := []uuid.UUID{deletedPost}
	for i := len(collected) - 1; i >= 0; i-- {
		want = append(want, collected[i])
	}
	var got []uuid.UUID
	cursor, pages := "", 0
	for {
		ids, total, next, err := repo.ListCollectedPostIDs(ctx, user, "", 2, cursor)
		if err != nil {
			t.Fatalf("ListCollectedPostIDs page %d: %v", pages+1, err)
		}
		if total != int64(len(want)) {
			t.Fatalf("total = %d, want %d", total, len(want))
		}
		got = append(got, ids...)
		pages++
		if next == "" {
			break
		}
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
		cursor = next
	}
	if !equalIDs(got, want) {
		t.Fatalf("collect pages = %v\nwant %v", got, want)
	}

	// 关键字：仅标题命中、已发布、未删的帖子（collected[0,2,4]）
	ids, total, next, err := repo.ListCollectedPostIDs(ctx, user, keyword, 10, "")
	if err != nil {
		t.Fatalf("ListCollectedPostIDs(keyword): %v", err)
	}
	wantKw := []uuid.UUID{collected[4], collected[2], collected[0]}
	if total != 3 || next != "" || !equalIDs(ids, wantKw) {
		t.Fatalf("keyword result = %v total=%d next=%q; want %v total=3", ids, total, next, wantKw)
	}

	if _, _, _, err := repo.ListCollectedPostIDs(ctx, user, "", 2, "%%%"); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("invalid cursor: want ErrInvalidCursor, got %v", err)
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

// 收藏 Lua：设值语义 + collect_count 增减 + BatchCheck / Backfill 读回。
func TestCollectCache_SetAndReadBack(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewPostCollectCache()
	user, postID, other := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()
	statsKey := redispkg.GetPostStatsKey(postID)
	testkit.CleanupKeys(t, statsKey, redispkg.GetPostStatsKey(other), redispkg.GetUserPostCollectListKey(user))

	if err := redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{CollectCount: 1, LikeCount: 9}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	for i, s := range []struct {
		collected bool
		want      domain.ToggleResult
		wantCount int
	}{
		{true, domain.ToggleResultCollected, 2},
		{true, domain.ToggleResultUnchanged, 2},
		{false, domain.ToggleResultUncollected, 1},
		{true, domain.ToggleResultCollected, 2},
	} {
		got, err := cache.Set(ctx, user, postID, s.collected)
		if err != nil || got != s.want {
			t.Fatalf("step %d Set(%v) = %d, %v; want %d", i, s.collected, got, err, s.want)
		}
		stats, err := redispkg.GetPostStatistics(postID)
		if err != nil || stats == nil {
			t.Fatalf("step %d GetPostStatistics = %v, %v", i, stats, err)
		}
		if stats.CollectCount != s.wantCount || stats.LikeCount != 9 {
			t.Fatalf("step %d stats = %+v, want collect_count %d and like_count untouched 9", i, stats, s.wantCount)
		}
	}

	// other 不在 ZSET：miss，由调用方回源 DB 后 Backfill
	got, missed, err := cache.BatchCheck(ctx, user, []uuid.UUID{postID, other})
	if err != nil {
		t.Fatalf("BatchCheck: %v", err)
	}
	if !got[postID] || got[other] || len(missed) != 1 || missed[0] != other {
		t.Fatalf("BatchCheck = %v missed %v", got, missed)
	}
	if err := cache.Backfill(ctx, user, []uuid.UUID{other}); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	got, missed, err = cache.BatchCheck(ctx, user, []uuid.UUID{postID, other})
	if err != nil || !got[other] || len(missed) != 0 {
		t.Fatalf("BatchCheck after backfill = %v missed %v err %v", got, missed, err)
	}
}
