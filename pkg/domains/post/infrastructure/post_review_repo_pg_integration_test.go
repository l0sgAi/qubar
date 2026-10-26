//go:build integration

// post_review_repo_pg_integration_test.go 人工审核（#118）仓储方法对开发栈 PostgreSQL 的集成测试：
// ResolveReview（条件更新 + 审计行同事务）、ListReviewingByCircle（keyset 翻页 + 计数口径）、LatestAIByPostIDs；
// 以及 GetCircleIDsByIDs（#156 通知回填）。
package infrastructure_test

import (
	"sync"
	"testing"
	"time"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type reviewLogRow struct {
	Decision   string
	Source     int16
	OperatorID *uuid.UUID
	Reason     *string
}

func reviewLogs(t *testing.T, db *gorm.DB, postID uuid.UUID) []reviewLogRow {
	t.Helper()
	var rows []reviewLogRow
	if err := db.Raw(`SELECT decision, source, operator_id, reason FROM domains.post_moderation_log
		WHERE post_id = ? ORDER BY id`, postID).Scan(&rows).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	return rows
}

func manualLog(postID, operator uuid.UUID, decision domain.ModerationDecision) *domain.PostModerationLog {
	return &domain.PostModerationLog{
		PostID: postID, Decision: decision, Hazards: domain.HazardScores{},
		Source: domain.ModerationSourceManual, OperatorID: &operator,
	}
}

// 只从审核中转出一次，且只有成功的一方写审计行；并发处理只有一方生效。
func TestPostRepo_ResolveReview(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	p := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusReviewing })
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", p.ID)

	const workers = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, decision := int16(domain.PostStatusPublished), domain.ModerationPublish
			if i%2 == 1 {
				status, decision = domain.PostStatusRejected, domain.ModerationReject
			}
			reason := "concurrent"
			l := manualLog(p.ID, sharedomain.NewID(), decision)
			l.Reason = &reason
			ok, err := repo.ResolveReview(ctx, p.ID, status, l)
			if err != nil {
				t.Errorf("ResolveReview: %v", err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}

	rows := reviewLogs(t, db, p.ID)
	if len(rows) != 1 {
		t.Fatalf("log rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Source != domain.ModerationSourceManual || r.OperatorID == nil || r.Reason == nil || *r.Reason != "concurrent" {
		t.Fatalf("unexpected manual log: %+v", r)
	}
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	wantStatus := int16(domain.PostStatusPublished)
	if r.Decision == string(domain.ModerationReject) {
		wantStatus = domain.PostStatusRejected
	}
	if got.Status != wantStatus {
		t.Fatalf("status = %d, want %d (decision %s)", got.Status, wantStatus, r.Decision)
	}

	// 已删除的审核中帖子：不更新、不写日志
	deleted := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusReviewing })
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", deleted.ID)
	softDelete(t, db, "domains.post", deleted.ID)
	if ok, err := repo.ResolveReview(ctx, deleted.ID, domain.PostStatusPublished,
		manualLog(deleted.ID, sharedomain.NewID(), domain.ModerationPublish)); err != nil || ok {
		t.Fatalf("deleted post resolve = %v, %v; want false", ok, err)
	}
	if rows := reviewLogs(t, db, deleted.ID); len(rows) != 0 {
		t.Fatalf("deleted post must not get a log row: %+v", rows)
	}
}

// 只列本圈、审核中、未删除的帖子，按 (create_time, id) 倒序 keyset 翻页。
func TestPostRepo_ListReviewingByCircle(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()
	circleID := sharedomain.NewID()
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)

	var want []uuid.UUID // 新 → 旧
	for i := 3; i >= 0; i-- {
		at := base.Add(-time.Duration(i) * time.Minute)
		if i == 0 {
			at = base.Add(-time.Minute) // 与上一条同一时间：后建的 UUIDv7 更大，排在前面
		}
		p := newPost(t, repo, func(p *domain.Post) {
			p.CircleID, p.Status, p.CreateTime = circleID, domain.PostStatusReviewing, at
		})
		want = append([]uuid.UUID{p.ID}, want...)
	}
	newPost(t, repo, func(p *domain.Post) { p.CircleID, p.CreateTime = circleID, base }) // 已发布
	gone := newPost(t, repo, func(p *domain.Post) {
		p.CircleID, p.Status, p.CreateTime = circleID, domain.PostStatusReviewing, base
	})
	softDelete(t, db, "domains.post", gone.ID)
	newPost(t, repo, func(p *domain.Post) { p.Status, p.CreateTime = domain.PostStatusReviewing, base }) // 他圈

	var got []uuid.UUID
	var after *domain.PostKeyset
	for page := 0; page < 5; page++ {
		posts, err := repo.ListReviewingByCircle(ctx, circleID, after, 3)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, p := range posts {
			got = append(got, p.ID)
		}
		if len(posts) < 3 {
			break
		}
		last := posts[len(posts)-1]
		after = &domain.PostKeyset{CreateTime: last.CreateTime, ID: last.ID}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d posts %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch at %d: got %v, want %v", i, got, want)
		}
	}
	// 待处理角标（#156）：计数与列表口径一致
	if n, err := repo.CountReviewingByCircle(ctx, circleID); err != nil || n != int64(len(want)) {
		t.Fatalf("CountReviewingByCircle = %d, %v; want %d", n, err, len(want))
	}
}

// 帖子ID → circle_id：已删除 / 任意状态的帖子也返回，查不到的 ID 不在结果里（#156 通知回填）。
func TestPostRepo_GetCircleIDsByIDs(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	published := newPost(t, repo, nil)
	blocked := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusBlocked })
	gone := newPost(t, repo, nil)
	softDelete(t, db, "domains.post", gone.ID)
	missing := sharedomain.NewID()

	got, err := repo.GetCircleIDsByIDs(ctx, []uuid.UUID{published.ID, blocked.ID, gone.ID, missing})
	if err != nil {
		t.Fatalf("GetCircleIDsByIDs: %v", err)
	}
	if len(got) != 3 || got[published.ID] != published.CircleID || got[blocked.ID] != blocked.CircleID || got[gone.ID] != gone.CircleID {
		t.Fatalf("circle ids = %v", got)
	}
	if empty, err := repo.GetCircleIDsByIDs(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v", empty, err)
	}
}

// 每篇帖子取最近一条 AI 日志，人工审计行不算。
func TestPostModerationLogRepo_LatestAIByPostIDs(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostModerationLogRepository(db)
	ctx := t.Context()
	a, b, none := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", a, b)

	sev := 2.5
	for _, row := range []*domain.PostModerationLog{
		{PostID: a, Decision: domain.ModerationFailOpen, Model: "old"},
		{PostID: a, Decision: domain.ModerationReview, Model: "new", Severity: &sev,
			Hazards: domain.HazardScores{domain.HazardSelfHarm: 0.9}},
		manualLog(a, sharedomain.NewID(), domain.ModerationPublish),
		manualLog(b, sharedomain.NewID(), domain.ModerationReject),
	} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.LatestAIByPostIDs(ctx, []uuid.UUID{a, b, none})
	if err != nil {
		t.Fatalf("LatestAIByPostIDs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want only post a: %+v", len(got), got)
	}
	l := got[a]
	if l == nil || l.Model != "new" || l.Source != domain.ModerationSourceAI || l.Severity == nil ||
		*l.Severity != sev || l.Hazards[domain.HazardSelfHarm] != 0.9 || l.OperatorID != nil {
		t.Fatalf("latest AI log = %+v", l)
	}
}
