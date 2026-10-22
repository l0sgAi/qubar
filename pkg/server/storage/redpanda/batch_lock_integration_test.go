//go:build integration

// batch_lock_integration_test.go 批量 UPDATE 统一取锁顺序（#124）的集成测试：多个批量写入方以不同行序并发更新同一批行，
// 不应出现死锁（40P01），且增量全部落库。
package redpanda_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

const (
	deadlockRows  = 40 // 每组行数：足够让不同行序的批量更新交叉
	deadlockIters = 25 // 每个写入方的轮数
)

func newIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = sharedomain.NewID()
	}
	return ids
}

func reversed(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.Reverse(out)
	return out
}

// runConcurrently 并发运行各写入方，每个写入方执行 deadlockIters 轮；返回全部错误。
func runConcurrently(writers map[string]func(iter int) error) []error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	start := make(chan struct{})
	for name, w := range writers {
		wg.Go(func() {
			<-start
			for i := range deadlockIters {
				if err := w(i); err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("%s iter %d: %w", name, i, err))
					mu.Unlock()
				}
			}
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func reportErrors(t *testing.T, errs []error) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Error(), "40P01") { // pgconn 错误文本含 (SQLSTATE 40P01)
			t.Errorf("deadlock detected: %v", err)
			continue
		}
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBatchPostUpdates_ConcurrentNoDeadlock(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)

	ids := newIDs(deadlockRows)
	circleID := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post", "id", toAny(ids)...)
	testkit.CleanupRows(t, "domains.post_like", "post_id", toAny(ids)...)
	for _, id := range ids {
		if err := db.Exec("INSERT INTO domains.post (id, circle_id, user_id) VALUES (?, ?, ?)",
			id, circleID, sharedomain.NewID()).Error; err != nil {
			t.Fatalf("insert post: %v", err)
		}
	}
	rev := reversed(ids)

	// 帖子统计 / 收藏用 map（行序随机），热度按相反顺序，点赞按正序；四方同时批量更新同一批帖子。
	errs := runConcurrently(map[string]func(int) error{
		"post stats": func(int) error { return redpanda.PersistPostStatsForTest(ids, 1) },
		"post hot": func(i int) error {
			if i%2 == 0 {
				return redpanda.UpdatePostHotForTest(rev, 1)
			}
			return redpanda.UpdatePostHotForTest(ids, 1)
		},
		"post collect": func(int) error { return redpanda.UpdatePostCollectsForTest(rev, 1) },
		"post like":    func(int) error { return redpanda.PersistPostLikesForTest(sharedomain.NewID(), ids) },
	})
	reportErrors(t, errs)

	type counts struct {
		ID           uuid.UUID
		ViewCount    int64
		LikeCount    int64
		CollectCount int64
		Hot          int64
	}
	var got []counts
	if err := db.Raw("SELECT id, view_count, like_count, collect_count, hot FROM domains.post WHERE id IN ?", ids).
		Scan(&got).Error; err != nil {
		t.Fatalf("query posts: %v", err)
	}
	if len(got) != len(ids) {
		t.Fatalf("got %d posts, want %d", len(got), len(ids))
	}
	for _, c := range got {
		// like_count 由帖子统计（+1/轮）与点赞落库（每轮新用户点赞 +1）共同累加
		if c.ViewCount != deadlockIters || c.LikeCount != 2*deadlockIters || c.CollectCount != deadlockIters || c.Hot != deadlockIters {
			t.Errorf("post %s counts = view %d like %d collect %d hot %d, want %d / %d / %d / %d",
				c.ID, c.ViewCount, c.LikeCount, c.CollectCount, c.Hot,
				deadlockIters, 2*deadlockIters, deadlockIters, deadlockIters)
		}
	}
}

func TestBatchCircleUpdates_ConcurrentNoDeadlock(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)

	ids := newIDs(deadlockRows)
	testkit.CleanupRows(t, "domains.circle", "id", toAny(ids)...)
	for _, id := range ids {
		if err := db.Exec("INSERT INTO domains.circle (id, name, description, rule, creator_id) VALUES (?, ?, '', '', ?)",
			id, "it-"+id.String(), sharedomain.NewID()).Error; err != nil {
			t.Fatalf("insert circle: %v", err)
		}
	}

	// 圈子统计（成员数 + 帖子数两条 UPDATE）与圈子热度同步器同时批量更新同一批圈子。
	errs := runConcurrently(map[string]func(int) error{
		"circle stats":   func(int) error { return redpanda.PersistCircleStatsForTest(ids, 1) },
		"circle hot":     func(int) error { return redpanda.UpdateCircleHotForTest(reversed(ids), 1) },
		"circle stats 2": func(int) error { return redpanda.PersistCircleStatsForTest(reversed(ids), 1) },
	})
	reportErrors(t, errs)

	type counts struct {
		ID          uuid.UUID
		MemberCount int64
		PostCount   int64
		Hot         int64
	}
	var got []counts
	if err := db.Raw("SELECT id, member_count, post_count, hot FROM domains.circle WHERE id IN ?", ids).
		Scan(&got).Error; err != nil {
		t.Fatalf("query circles: %v", err)
	}
	if len(got) != len(ids) {
		t.Fatalf("got %d circles, want %d", len(got), len(ids))
	}
	for _, c := range got {
		if c.MemberCount != 2*deadlockIters || c.PostCount != 2*deadlockIters || c.Hot != deadlockIters {
			t.Errorf("circle %s counts = member %d post %d hot %d, want %d / %d / %d",
				c.ID, c.MemberCount, c.PostCount, c.Hot, 2*deadlockIters, 2*deadlockIters, deadlockIters)
		}
	}
}

func toAny(ids []uuid.UUID) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
