//go:build integration

// post_moderation_log_repo_pg_integration_test.go 审核日志仓储与帖子条件更新对开发栈 PostgreSQL 的集成测试（#117）。
package infrastructure_test

import (
	"errors"
	"testing"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"
)

func TestPostModerationLogRepo_CreateAndExists(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostModerationLogRepository(db)
	ctx := t.Context()
	postID := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", postID)

	if ok, err := repo.ExistsByPostID(ctx, postID); err != nil || ok {
		t.Fatalf("ExistsByPostID before insert = %v, %v; want false", ok, err)
	}

	sev, conf := 1.25, 0.8
	errMsg := "boom"
	row := &domain.PostModerationLog{
		PostID:             postID,
		Decision:           domain.ModerationReview,
		Hazards:            domain.HazardScores{domain.HazardSpamAd: 0.42, domain.HazardSexual: 0.01},
		Severity:           &sev,
		SeverityConfidence: &conf,
		Model:              "jev-1.13.0",
		InputTokens:        1234,
		LatencyMs:          87,
		ErrorMsg:           &errMsg,
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.ID.Version() != 7 {
		t.Fatalf("Create should assign a UUIDv7 id, got version %d", row.ID.Version())
	}
	// 审核失败的行：无分数，hazards 为 {}
	if err := repo.Create(ctx, &domain.PostModerationLog{PostID: postID, Decision: domain.ModerationFailOpen}); err != nil {
		t.Fatalf("Create failure row: %v", err)
	}

	if ok, err := repo.ExistsByPostID(ctx, postID); err != nil || !ok {
		t.Fatalf("ExistsByPostID after insert = %v, %v; want true", ok, err)
	}

	var got []domain.PostModerationLog
	if err := db.Where("post_id = ?", postID).Order("id").Find(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	r := got[0]
	if r.Decision != domain.ModerationReview || r.Hazards[domain.HazardSpamAd] != 0.42 || len(r.Hazards) != 2 ||
		r.Severity == nil || *r.Severity != sev || r.Model != "jev-1.13.0" || r.InputTokens != 1234 ||
		r.ErrorMsg == nil || *r.ErrorMsg != errMsg || r.CreateTime.IsZero() {
		t.Fatalf("round trip mismatch: %+v", r)
	}
	f := got[1]
	if f.Severity != nil || f.SeverityConfidence != nil || len(f.Hazards) != 0 || f.ErrorMsg != nil {
		t.Fatalf("failure row should have no scores: %+v", f)
	}
}

// 条件更新只从审核中转出一次；非审核中 / 已删除的帖子更新 0 行。
func TestPostRepo_UpdateStatusFromReviewing(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	reviewing := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusReviewing })
	ok, err := repo.UpdateStatusFromReviewing(ctx, reviewing.ID, domain.PostStatusPublished)
	if err != nil || !ok {
		t.Fatalf("first update = %v, %v; want true", ok, err)
	}
	got, err := repo.GetByID(ctx, reviewing.ID)
	if err != nil || got.Status != domain.PostStatusPublished {
		t.Fatalf("status after update = %+v, %v", got, err)
	}
	if !got.UpdateTime.After(reviewing.UpdateTime) && !got.UpdateTime.Equal(reviewing.UpdateTime) {
		t.Fatalf("update_time went backwards: %v -> %v", reviewing.UpdateTime, got.UpdateTime)
	}
	// 重复处理：已不是审核中
	if ok, err := repo.UpdateStatusFromReviewing(ctx, reviewing.ID, domain.PostStatusRejected); err != nil || ok {
		t.Fatalf("second update = %v, %v; want false", ok, err)
	}

	published := newPost(t, repo, nil)
	if ok, err := repo.UpdateStatusFromReviewing(ctx, published.ID, domain.PostStatusRejected); err != nil || ok {
		t.Fatalf("published post update = %v, %v; want false", ok, err)
	}

	deleted := newPost(t, repo, func(p *domain.Post) { p.Status = domain.PostStatusReviewing })
	softDelete(t, db, "domains.post", deleted.ID)
	if ok, err := repo.UpdateStatusFromReviewing(ctx, deleted.ID, domain.PostStatusPublished); err != nil || ok {
		t.Fatalf("deleted post update = %v, %v; want false", ok, err)
	}
	if _, err := repo.GetByID(ctx, deleted.ID); !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("deleted post should stay hidden, got %v", err)
	}
}
