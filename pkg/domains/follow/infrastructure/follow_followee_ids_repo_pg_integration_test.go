//go:build integration

// follow_followee_ids_repo_pg_integration_test.go 关注 ID 读取（#220，首页关注 tab）对开发栈 PostgreSQL 的集成测试：
// 只返回有效关系、按关注时间倒序、limit 截断取最近关注的、取关 / 再关注后的结果、查询走部分索引。
// 复用 follow_repo_pg_integration_test.go 的 followEnv（随机用户 ID + 注册清理）。
package infrastructure_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/domains/follow/infrastructure"

	"github.com/google/uuid"
)

func listFolloweeIDs(t *testing.T, e *followEnv, follower uuid.UUID, limit int) []uuid.UUID {
	t.Helper()
	ids, err := infrastructure.NewFolloweeIDRepository(e.db).ListFolloweeIDs(t.Context(), follower, limit)
	if err != nil {
		t.Fatalf("ListFolloweeIDs(%s, %d): %v", follower, limit, err)
	}
	return ids
}

// setFollowTime 把一条关系的关注时间改成确定值，让排序断言不依赖两次关注之间的时钟间隔。
func (e *followEnv) setFollowTime(t *testing.T, follower, followee uuid.UUID, at time.Time) {
	t.Helper()
	res := e.db.WithContext(t.Context()).Exec(
		`UPDATE domains.user_follow SET follow_time = ? WHERE follower_id = ? AND followee_id = ?`, at, follower, followee)
	if res.Error != nil || res.RowsAffected != 1 {
		t.Fatalf("set follow_time: rows=%d err=%v", res.RowsAffected, res.Error)
	}
}

func TestFolloweeIDRepo_ListFolloweeIDs(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 6)
	me, a, b, c, other, fan := u[0], u[1], u[2], u[3], u[4], u[5]

	// 没有任何关系：空切片（不是 nil），不报错
	if got := listFolloweeIDs(t, e, me, 10); got == nil || len(got) != 0 {
		t.Fatalf("no relation: got %v, want empty non-nil slice", got)
	}

	base := time.Now().UTC().Truncate(time.Second)
	for i, followee := range []uuid.UUID{a, b, c} { // 关注时间 a < b < c
		e.follow(t, me, followee, 10)
		e.setFollowTime(t, me, followee, base.Add(time.Duration(i)*time.Minute))
	}
	// 干扰数据：别人的关注、别人关注我（我是 followee）都不该出现在我的关注列表里
	e.follow(t, other, a, 10)
	e.follow(t, fan, me, 10)

	if got, want := listFolloweeIDs(t, e, me, 10), []uuid.UUID{c, b, a}; !slices.Equal(got, want) {
		t.Fatalf("follow_time desc: got %v, want %v", got, want)
	}
	// limit 截断：只取最近关注的
	if got, want := listFolloweeIDs(t, e, me, 2), []uuid.UUID{c, b}; !slices.Equal(got, want) {
		t.Fatalf("limit 2: got %v, want %v", got, want)
	}
	// limit <= 0：空，不查全表
	for _, limit := range []int{0, -1} {
		if got := listFolloweeIDs(t, e, me, limit); len(got) != 0 {
			t.Fatalf("limit %d: got %v, want empty", limit, got)
		}
	}

	// 取关后立即不再返回（软删除行被过滤）
	e.unfollow(t, me, b)
	if got, want := listFolloweeIDs(t, e, me, 10), []uuid.UUID{c, a}; !slices.Equal(got, want) {
		t.Fatalf("after unfollow: got %v, want %v", got, want)
	}

	// 再次关注复用原行并刷新 follow_time：重新出现，且排到最前
	e.follow(t, me, b, 10)
	e.setFollowTime(t, me, b, base.Add(time.Hour))
	if got, want := listFolloweeIDs(t, e, me, 10), []uuid.UUID{b, c, a}; !slices.Equal(got, want) {
		t.Fatalf("after re-follow: got %v, want %v", got, want)
	}
	if rows := e.rows(t, me, b); len(rows) != 1 {
		t.Fatalf("re-follow must reuse the row, got %d rows", len(rows))
	}

	// 同一关注时间按 id 倒序决胜，结果稳定
	e.setFollowTime(t, me, a, base)
	e.setFollowTime(t, me, c, base)
	want := []uuid.UUID{a, c}
	if e.rows(t, me, a)[0].ID.String() < e.rows(t, me, c)[0].ID.String() {
		want = []uuid.UUID{c, a}
	}
	if got := listFolloweeIDs(t, e, me, 10)[1:]; !slices.Equal(got, want) {
		t.Fatalf("tie on follow_time: got %v, want %v", got, want)
	}
}

// 关注数达到上限（默认 2000）时一次取全量，且查询的过滤与排序能由部分索引 idx_user_follow_following 直接提供（不需要额外排序）。
func TestFolloweeIDRepo_FullListUsesFollowingIndex(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	me := e.newUsers(t, 1)[0]

	const n = 2000
	// 直接批量插入关系行（逐条走 Follow 事务太慢）；followee 用随机 ID，按 follower_id 清理即可。
	err := e.db.WithContext(t.Context()).Exec(`
INSERT INTO domains.user_follow (id, follower_id, followee_id, follow_time, deleted)
SELECT uuidv7(), ?, uuidv7(), now() - make_interval(secs => g), CASE WHEN g % 10 = 0 THEN 1 ELSE 0 END
FROM generate_series(1, ?) AS g`, me, n+n/9+1).Error
	if err != nil {
		t.Fatalf("seed relations: %v", err)
	}

	var active int
	if err := e.db.WithContext(t.Context()).Raw(
		`SELECT count(*) FROM domains.user_follow WHERE follower_id = ? AND deleted = 0`, me).Scan(&active).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if active < n {
		t.Fatalf("seeded %d active relations, want >= %d", active, n)
	}

	got := listFolloweeIDs(t, e, me, n)
	if len(got) != n {
		t.Fatalf("got %d followee ids, want %d", len(got), n)
	}
	seen := make(map[uuid.UUID]struct{}, n)
	for _, id := range got {
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("followee ids must be distinct: %d unique of %d", len(seen), n)
	}

	// 关掉顺序扫描 / 位图扫描并惩罚排序（SET LOCAL，只在本事务内生效），让计划不受开发库统计信息影响：
	// 只要查询的过滤与排序和部分索引对得上，计划就一定是该索引的有序扫描、没有 Sort 节点。
	var plan []string
	tx := e.db.WithContext(t.Context()).Begin()
	defer tx.Rollback()
	for _, guc := range []string{"enable_seqscan", "enable_bitmapscan", "enable_sort"} {
		if err := tx.Exec("SET LOCAL " + guc + " = off").Error; err != nil {
			t.Fatalf("set %s: %v", guc, err)
		}
	}
	if err := tx.Raw(`
EXPLAIN SELECT followee_id FROM domains.user_follow
WHERE follower_id = ? AND deleted = 0 ORDER BY follow_time DESC, id DESC LIMIT ?`, me, n).Scan(&plan).Error; err != nil {
		t.Fatalf("explain: %v", err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "idx_user_follow_following") || strings.Contains(text, "Sort") {
		t.Fatalf("query must be served by idx_user_follow_following without a sort step:\n%s", text)
	}
}
