//go:build integration

// comment_auto_reply_integration_test.go 机器人自动回复评论候选查询（#99）的集成测试：
// 点赞 / 回复门槛（任一）、时间窗口（UUIDv7 主键下界 + create_time）、评论 / 帖子状态、
// 机器人作者剔除、所属帖子圈子范围、排除名单、热度降序。
package infrastructure_test

import (
	"fmt"
	"testing"
	"time"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/domains/comment/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// insertUser 插入 users 行（role=2 为机器人账号），测试结束删除。
func insertUser(t *testing.T, db *gorm.DB, role int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec("INSERT INTO domains.users (id, username, email, role) VALUES (?, ?, ?, ?)",
		id, "it-user", fmt.Sprintf("%s@it.qubar.local", id), role).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	return id
}

// insertPost 插入帖子行（只填评论候选查询用到的列），测试结束删除。
func insertPost(t *testing.T, db *gorm.DB, circleID uuid.UUID, status, isLock int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec("INSERT INTO domains.post (id, circle_id, user_id, status, is_lock) VALUES (?, ?, ?, ?, ?)",
		id, circleID, sharedomain.NewID(), status, isLock).Error; err != nil {
		t.Fatalf("insert post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", id)
	return id
}

func TestCommentRepo_ListHotForAutoReply(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCommentRepository(db)
	ctx := t.Context()

	circle := sharedomain.NewID()
	post := insertPost(t, db, circle, 1, 0)
	lockedPost := insertPost(t, db, circle, 1, 1)
	otherCirclePost := insertPost(t, db, sharedomain.NewID(), 1, 0)
	human := insertUser(t, db, 0)
	bot := insertUser(t, db, 2)

	mk := func(postID uuid.UUID, root *uuid.UUID, likes int, author uuid.UUID) *domain.Comment {
		c := newComment(t, repo, postID, root, likes)
		if err := db.Exec("UPDATE domains.comment SET user_id = ? WHERE id = ?", author, c.ID).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	liked := mk(post, nil, 20, human)  // 点赞达标
	replied := mk(post, nil, 1, human) // 回复数达标（下面挂 3 条回复）
	for i := 0; i < 3; i++ {
		mk(post, &replied.ID, 0, human)
	}
	_ = mk(post, nil, 2, human)                        // 都不达标
	_ = mk(post, nil, 50, bot)                         // 机器人账号
	_ = mk(lockedPost, nil, 50, human)                 // 帖子锁定
	otherCircle := mk(otherCirclePost, nil, 50, human) // 他圈
	hidden := mk(post, nil, 50, human)
	if err := db.Exec("UPDATE domains.comment SET status = ? WHERE id = ?", domain.CommentStatusHidden, hidden.ID).Error; err != nil {
		t.Fatal(err)
	}
	deleted := mk(post, nil, 50, human)
	softDeleteComment(t, db, deleted.ID)
	old := mk(post, nil, 50, human)
	if err := db.Exec("UPDATE domains.comment SET create_time = now() - interval '2 days' WHERE id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}

	since := time.Now().Add(-6 * time.Hour)
	got, err := repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{
		CircleID: circle, LikeMin: 10, ReplyMin: 3, Since: since, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListHotForAutoReply: %v", err)
	}
	if len(got) != 2 || got[0].ID != liked.ID || got[1].ID != replied.ID {
		t.Fatalf("want [liked replied] by like+reply desc, got %+v", got)
	}
	if got[0].PostCircleID != circle || got[0].PostID != post || got[0].UserID != human || got[0].Content == "" {
		t.Fatalf("row fields not filled: %+v", got[0])
	}
	if got[1].ReplyCount != 3 {
		t.Fatalf("reply_count = %d, want 3", got[1].ReplyCount)
	}

	// 只用点赞门槛：回复数达标的不再命中。
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{CircleID: circle, LikeMin: 10, Since: since, Limit: 10})
	if err != nil || len(got) != 1 || got[0].ID != liked.ID {
		t.Fatalf("like-only: got %+v err %v", got, err)
	}
	// 排除名单。
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{
		CircleID: circle, LikeMin: 10, ReplyMin: 3, Since: since, ExcludeIDs: []uuid.UUID{liked.ID}, Limit: 10,
	})
	if err != nil || len(got) != 1 || got[0].ID != replied.ID {
		t.Fatalf("exclude: got %+v err %v", got, err)
	}
	// 两个门槛都 <=0：不返回任何候选（防"窗口内全部评论"）。
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{CircleID: circle, Since: since, Limit: 10})
	if err != nil || len(got) != 0 {
		t.Fatalf("no threshold must return nothing: got %+v err %v", got, err)
	}
	// 全站：包含他圈评论。
	got, err = repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{LikeMin: 50, Since: since, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range got {
		if r.ID == otherCircle.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("whole-site query must include comments of every circle")
	}
}
