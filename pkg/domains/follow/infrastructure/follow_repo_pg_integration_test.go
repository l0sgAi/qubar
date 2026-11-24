//go:build integration

// follow_repo_pg_integration_test.go follow 仓储对开发栈 PostgreSQL 的集成测试（#217）：
// 关注 / 取关幂等与计数、取关后再关注复用行并刷新 follow_time、并发重复关注只 +1、
// 并发关注不突破上限、互相并发关注不死锁、按明细重算计数。
// 用户 ID 全部随机生成（仓储不读 users 表），测试之间互不影响。
package infrastructure_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/domains/follow/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

type followEnv struct {
	db        *gorm.DB
	relations domain.RelationRepository
	stats     domain.StatRepository
}

func newFollowEnv(t *testing.T) *followEnv {
	t.Helper()
	db := testkit.PG(t)
	return &followEnv{db: db, relations: infrastructure.NewRelationRepository(db), stats: infrastructure.NewStatRepository(db)}
}

// newUsers 生成 n 个随机用户 ID，并注册清理它们的关系行与计数行。
// 测试里的每条关系两端都来自 newUsers，所以按 follower_id 清理即可覆盖全部关系行。
func (e *followEnv) newUsers(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	vals := make([]any, n)
	for i := range ids {
		ids[i] = sharedomain.NewID()
		vals[i] = ids[i]
	}
	testkit.CleanupRows(t, "domains.user_follow", "follower_id", vals...)
	testkit.CleanupRows(t, "domains.user_follow_stat", "user_id", vals...)
	return ids
}

func (e *followEnv) follow(t *testing.T, follower, followee uuid.UUID, limit int) bool {
	t.Helper()
	changed, err := e.relations.Follow(t.Context(), follower, followee, limit)
	if err != nil {
		t.Fatalf("follow %s -> %s: %v", follower, followee, err)
	}
	return changed
}

func (e *followEnv) unfollow(t *testing.T, follower, followee uuid.UUID) bool {
	t.Helper()
	changed, err := e.relations.Unfollow(t.Context(), follower, followee)
	if err != nil {
		t.Fatalf("unfollow %s -> %s: %v", follower, followee, err)
	}
	return changed
}

// assertStat 断言计数表里的值，并断言它与关系明细一致。
func (e *followEnv) assertStat(t *testing.T, userID uuid.UUID, wantFollowing, wantFollower int) {
	t.Helper()
	stat, err := e.stats.GetStat(t.Context(), userID)
	if err != nil {
		t.Fatalf("GetStat: %v", err)
	}
	if stat.FollowingCount != wantFollowing || stat.FollowerCount != wantFollower {
		t.Errorf("stat of %s = following %d / follower %d, want %d / %d",
			userID, stat.FollowingCount, stat.FollowerCount, wantFollowing, wantFollower)
	}
	following, follower := e.detailCounts(t, userID)
	if following != wantFollowing || follower != wantFollower {
		t.Errorf("detail of %s = following %d / follower %d, want %d / %d", userID, following, follower, wantFollowing, wantFollower)
	}
}

// detailCounts 直接数关系明细（deleted=0）。
func (e *followEnv) detailCounts(t *testing.T, userID uuid.UUID) (following, follower int) {
	t.Helper()
	row := e.db.WithContext(t.Context()).Raw(`
SELECT (SELECT count(*) FROM domains.user_follow WHERE follower_id = ? AND deleted = 0),
       (SELECT count(*) FROM domains.user_follow WHERE followee_id = ? AND deleted = 0)`, userID, userID).Row()
	if err := row.Scan(&following, &follower); err != nil {
		t.Fatalf("count detail: %v", err)
	}
	return following, follower
}

func (e *followEnv) rows(t *testing.T, follower, followee uuid.UUID) []domain.UserFollow {
	t.Helper()
	var rows []domain.UserFollow
	if err := e.db.WithContext(t.Context()).
		Where("follower_id = ? AND followee_id = ?", follower, followee).Find(&rows).Error; err != nil {
		t.Fatalf("load relation rows: %v", err)
	}
	return rows
}

func (e *followEnv) assertRelation(t *testing.T, viewer, target uuid.UUID, want domain.Relation) {
	t.Helper()
	got, err := e.relations.GetRelation(t.Context(), viewer, target)
	if err != nil {
		t.Fatalf("GetRelation: %v", err)
	}
	if got != want {
		t.Errorf("relation %s -> %s = %d, want %d", viewer, target, got, want)
	}
}

// 关注 / 取关幂等：重复操作 changed=false，不产生重复行，计数不变；四种关系状态。
func TestFollowRepo_IdempotentCountsAndRelation(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	// 从没出现过的用户：计数为 0，关系为未关注
	e.assertStat(t, a, 0, 0)
	e.assertRelation(t, a, b, domain.RelationNone)
	if e.unfollow(t, a, b) {
		t.Error("unfollow without relation: changed = true, want false")
	}

	if !e.follow(t, a, b, 10) {
		t.Fatal("first follow: changed = false, want true")
	}
	if e.follow(t, a, b, 10) {
		t.Error("repeat follow: changed = true, want false")
	}
	if rows := e.rows(t, a, b); len(rows) != 1 || rows[0].Deleted != 0 || rows[0].ID == uuid.Nil {
		t.Fatalf("relation rows after repeat follow = %+v, want exactly one active row", rows)
	}
	e.assertStat(t, a, 1, 0)
	e.assertStat(t, b, 0, 1)
	e.assertRelation(t, a, b, domain.RelationFollowing)
	e.assertRelation(t, b, a, domain.RelationFollowedBy)

	e.follow(t, b, a, 10)
	e.assertRelation(t, a, b, domain.RelationMutual)
	e.assertRelation(t, b, a, domain.RelationMutual)
	e.assertStat(t, a, 1, 1)
	e.assertStat(t, b, 1, 1)

	if !e.unfollow(t, a, b) {
		t.Fatal("unfollow: changed = false, want true")
	}
	if e.unfollow(t, a, b) {
		t.Error("repeat unfollow: changed = true, want false")
	}
	if rows := e.rows(t, a, b); len(rows) != 1 || rows[0].Deleted != 1 {
		t.Fatalf("relation rows after unfollow = %+v, want one row with deleted=1", rows)
	}
	e.assertStat(t, a, 0, 1)
	e.assertStat(t, b, 1, 0)
	e.assertRelation(t, a, b, domain.RelationFollowedBy)
}

// 取关后再关注：复用同一行，follow_time 更新为最近一次关注的时间，create_time 不变。
func TestFollowRepo_RefollowReusesRowAndRefreshesFollowTime(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	e.follow(t, a, b, 10)
	first := e.rows(t, a, b)[0]
	e.unfollow(t, a, b)
	time.Sleep(20 * time.Millisecond)
	if !e.follow(t, a, b, 10) {
		t.Fatal("re-follow: changed = false, want true")
	}

	rows := e.rows(t, a, b)
	if len(rows) != 1 {
		t.Fatalf("rows after re-follow = %d, want 1", len(rows))
	}
	second := rows[0]
	if second.ID != first.ID || second.Deleted != 0 {
		t.Errorf("re-follow row = id %s deleted %d, want reused id %s active", second.ID, second.Deleted, first.ID)
	}
	if !second.FollowTime.After(first.FollowTime) {
		t.Errorf("follow_time %v not after first follow %v", second.FollowTime, first.FollowTime)
	}
	if !second.CreateTime.Equal(first.CreateTime) {
		t.Errorf("create_time changed: %v -> %v", first.CreateTime, second.CreateTime)
	}
	e.assertStat(t, a, 1, 0)
	e.assertStat(t, b, 0, 1)
}

// 同一对用户 20 个并发重复关注：恰好 1 个 changed，只有 1 行，计数只 +1。
func TestFollowRepo_ConcurrentDuplicateFollowCountsOnce(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]
	ctx := context.WithoutCancel(t.Context())

	var changed atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := e.relations.Follow(ctx, a, b, 10)
			if err != nil {
				t.Errorf("concurrent follow: %v", err)
			}
			if ok {
				changed.Add(1)
			}
		}()
	}
	wg.Wait()

	if changed.Load() != 1 {
		t.Errorf("changed count = %d, want 1", changed.Load())
	}
	if rows := e.rows(t, a, b); len(rows) != 1 {
		t.Errorf("relation rows = %d, want 1", len(rows))
	}
	e.assertStat(t, a, 1, 0)
	e.assertStat(t, b, 0, 1)
}

// 上限 5、20 个并发关注不同目标：恰好 5 个成功，其余返回 ErrFollowLimitReached 且不留关系行。
// 取关一个后可以再关注。
func TestFollowRepo_ConcurrentFollowNeverExceedsLimit(t *testing.T) {
	t.Parallel()
	const limit, attempts = 5, 20
	e := newFollowEnv(t)
	u := e.newUsers(t, attempts+2)
	a, targets, extra := u[0], u[1:attempts+1], u[attempts+1]
	ctx := context.WithoutCancel(t.Context())

	results := make([]error, attempts)
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = e.relations.Follow(ctx, a, target, limit)
		}()
	}
	wg.Wait()

	var followed []uuid.UUID
	for i, err := range results {
		switch {
		case err == nil:
			followed = append(followed, targets[i])
			e.assertStat(t, targets[i], 0, 1)
		case errors.Is(err, domain.ErrFollowLimitReached):
			// 超限的事务整体回滚：没有关系行，对方粉丝数不变
			if rows := e.rows(t, a, targets[i]); len(rows) != 0 {
				t.Errorf("rejected follow left %d relation rows", len(rows))
			}
			e.assertStat(t, targets[i], 0, 0)
		default:
			t.Errorf("unexpected err %v", err)
		}
	}
	if len(followed) != limit {
		t.Fatalf("successful follows = %d, want %d", len(followed), limit)
	}
	e.assertStat(t, a, limit, 0)

	// 已达上限：新目标被拒，重复关注已关注的人仍然幂等成功
	if _, err := e.relations.Follow(ctx, a, extra, limit); !errors.Is(err, domain.ErrFollowLimitReached) {
		t.Fatalf("follow at limit: err = %v, want ErrFollowLimitReached", err)
	}
	if e.follow(t, a, followed[0], limit) {
		t.Error("repeat follow at limit: changed = true, want false")
	}
	// 取关后可再关注
	e.unfollow(t, a, followed[0])
	if !e.follow(t, a, extra, limit) {
		t.Error("follow after unfollow: changed = false, want true")
	}
	e.assertStat(t, a, limit, 0)
}

// A 关注 B 与 B 关注 A 并发（多对用户、多轮关注 / 取关）：不死锁，每轮后计数与关系正确。
func TestFollowRepo_MutualConcurrentFollowNoDeadlock(t *testing.T) {
	t.Parallel()
	const pairs, rounds = 4, 15
	e := newFollowEnv(t)
	u := e.newUsers(t, pairs*2)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()

	// both 让 a→b 与 b→a 两个操作同时起跑。
	both := func(a, b uuid.UUID, op func(follower, followee uuid.UUID) error) error {
		start := make(chan struct{})
		errs := make(chan error, 2)
		for _, p := range [][2]uuid.UUID{{a, b}, {b, a}} {
			go func() {
				<-start
				errs <- op(p[0], p[1])
			}()
		}
		close(start)
		return errors.Join(<-errs, <-errs)
	}

	var wg sync.WaitGroup
	for i := range pairs {
		a, b := u[2*i], u[2*i+1]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range rounds {
				err := both(a, b, func(follower, followee uuid.UUID) error {
					_, err := e.relations.Follow(ctx, follower, followee, 10)
					return err
				})
				if err != nil {
					t.Errorf("round %d follow: %v", round, err)
					return
				}
				if rel, err := e.relations.GetRelation(ctx, a, b); err != nil || rel != domain.RelationMutual {
					t.Errorf("round %d: relation = %d err = %v, want mutual", round, rel, err)
					return
				}
				if round == rounds-1 {
					return // 最后一轮保持互关，供下面断言
				}
				err = both(a, b, func(follower, followee uuid.UUID) error {
					_, err := e.relations.Unfollow(ctx, follower, followee)
					return err
				})
				if err != nil {
					t.Errorf("round %d unfollow: %v", round, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	for _, id := range u {
		e.assertStat(t, id, 1, 1)
	}
}

// 重算：把计数行改乱（或删掉）后，RecountStat 的结果与明细一致。
func TestFollowRepo_RecountStatMatchesDetail(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 5)
	a := u[0]
	// a 关注 3 人，其中取关 1 人；2 人关注 a
	e.follow(t, a, u[1], 10)
	e.follow(t, a, u[2], 10)
	e.follow(t, a, u[3], 10)
	e.unfollow(t, a, u[3])
	e.follow(t, u[1], a, 10)
	e.follow(t, u[4], a, 10)

	recount := func(step string) {
		t.Helper()
		stat, err := e.stats.RecountStat(t.Context(), a)
		if err != nil {
			t.Fatalf("%s: RecountStat: %v", step, err)
		}
		following, follower := e.detailCounts(t, a)
		if stat.FollowingCount != following || stat.FollowerCount != follower || following != 2 || follower != 2 {
			t.Errorf("%s: recount = %d / %d, detail = %d / %d, want 2 / 2",
				step, stat.FollowingCount, stat.FollowerCount, following, follower)
		}
		e.assertStat(t, a, 2, 2)
	}

	if err := e.db.WithContext(t.Context()).Exec(
		"UPDATE domains.user_follow_stat SET following_count = 99, follower_count = 0 WHERE user_id = ?", a).Error; err != nil {
		t.Fatalf("corrupt stat: %v", err)
	}
	recount("drifted row")

	if err := e.db.WithContext(t.Context()).Exec(
		"DELETE FROM domains.user_follow_stat WHERE user_id = ?", a).Error; err != nil {
		t.Fatalf("delete stat: %v", err)
	}
	recount("missing row")

	// 没有任何关系的用户：重算得到 0 / 0
	lonely := e.newUsers(t, 1)[0]
	stat, err := e.stats.RecountStat(t.Context(), lonely)
	if err != nil || stat.FollowingCount != 0 || stat.FollowerCount != 0 {
		t.Errorf("recount of user without relations = %+v err = %v, want 0 / 0", stat, err)
	}
}

// 重算与关注 / 取关并发：全部结束后计数仍与明细一致（重算锁计数行，不丢并发的 ±1）。
func TestFollowRepo_RecountStatConcurrentWithWrites(t *testing.T) {
	t.Parallel()
	const targets = 12
	e := newFollowEnv(t)
	u := e.newUsers(t, targets+1)
	a := u[0]
	ctx := context.WithoutCancel(t.Context())

	var wg sync.WaitGroup
	for _, target := range u[1:] {
		wg.Add(2)
		go func() { // a 关注 target、target 关注 a
			defer wg.Done()
			for _, op := range []func() (bool, error){
				func() (bool, error) { return e.relations.Follow(ctx, a, target, 100) },
				func() (bool, error) { return e.relations.Follow(ctx, target, a, 100) },
			} {
				if _, err := op(); err != nil {
					t.Errorf("concurrent write: %v", err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := e.stats.RecountStat(ctx, a); err != nil {
				t.Errorf("concurrent recount: %v", err)
			}
		}()
	}
	wg.Wait()

	e.assertStat(t, a, targets, targets)
}
