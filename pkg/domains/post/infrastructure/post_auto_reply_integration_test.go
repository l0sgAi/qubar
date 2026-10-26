//go:build integration

// post_auto_reply_integration_test.go 机器人自动回复帖子候选查询（#99）的集成测试：
// 热度门槛 / 时间窗口 / 已发布未锁定 / 机器人作者剔除 / 圈子范围 / 排除名单 / 热度降序。
package infrastructure_test

import (
	"fmt"
	"testing"
	"time"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// newUser 插入一个 users 行（role=2 为机器人账号），测试结束删除。
func newUser(t *testing.T, db *gorm.DB, role int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	email := fmt.Sprintf("%s@it.qubar.local", id)
	if err := db.Exec("INSERT INTO domains.users (id, username, email, role) VALUES (?, ?, ?, ?)", id, "it-user", email, role).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	return id
}

func TestPostRepo_ListHotForAutoReply(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	circle := sharedomain.NewID()
	human := newUser(t, db, 0)
	bot := newUser(t, db, 2)
	mk := func(hot int, mutate func(*domain.Post)) *domain.Post {
		return newPost(t, repo, func(p *domain.Post) {
			p.CircleID = circle
			p.UserID = human
			p.Hot = hot
			if mutate != nil {
				mutate(p)
			}
		})
	}
	hot1 := mk(500, nil)
	hot2 := mk(300, nil)
	_ = mk(50, nil) // 门槛以下
	_ = mk(900, func(p *domain.Post) { p.UserID = bot })
	_ = mk(900, func(p *domain.Post) { p.IsLock = 1 })
	_ = mk(900, func(p *domain.Post) { p.Status = domain.PostStatusReviewing })
	deleted := mk(900, nil)
	softDelete(t, db, "domains.post", deleted.ID)
	old := mk(900, nil)
	if err := db.Exec("UPDATE domains.post SET create_time = now() - interval '3 days' WHERE id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	other := newPost(t, repo, func(p *domain.Post) { p.UserID = human; p.Hot = 800 }) // 他圈

	since := time.Now().Add(-24 * time.Hour)
	got, err := repo.ListHotForAutoReply(ctx, domain.AutoReplyPostFilter{CircleID: circle, HotMin: 100, Since: since, Limit: 10})
	if err != nil {
		t.Fatalf("ListHotForAutoReply: %v", err)
	}
	if len(got) != 2 || got[0].ID != hot1.ID || got[1].ID != hot2.ID {
		t.Fatalf("want [hot1 hot2] by hot desc, got %v", postIDs(got))
	}
	if got[0].CircleID != circle || got[0].UserID != human || got[0].Hot != 500 {
		t.Fatalf("selected columns not filled: %+v", got[0])
	}

	// 排除名单 + limit
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyPostFilter{
		CircleID: circle, HotMin: 100, Since: since, ExcludeIDs: []uuid.UUID{hot1.ID}, Limit: 1,
	})
	if err != nil || len(got) != 1 || got[0].ID != hot2.ID {
		t.Fatalf("exclude/limit: got %v err %v", postIDs(got), err)
	}

	// 全站（CircleID=Nil）包含他圈帖子。
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyPostFilter{
		HotMin: 100, Since: since, Limit: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := postIDs(got)
	if !ids[other.ID] || !ids[hot1.ID] {
		t.Fatal("whole-site query must include posts of every circle")
	}
}

func postIDs(posts []domain.Post) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(posts))
	for _, p := range posts {
		m[p.ID] = true
	}
	return m
}
