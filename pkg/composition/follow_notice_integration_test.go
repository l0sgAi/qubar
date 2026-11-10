//go:build integration

// follow_notice_integration_test.go 被关注通知（#219）端到端集成测试（make test-integration）：
// composition 装配的 FollowService（真实 PG + Redpanda 发布器）→ notification_events → 通知 consumer
// → domains.notification → notice 域列表 / 未读数 → SSE hub 推送（按生产方式接在 consumer 的 flush 回调上）。
//
// 覆盖：首次关注生成 1 行未读；重复关注与取关不产生事件；取关后再关注复用同一行并重新置为未读；
// 列表按 type=12 过滤并带关注者信息；接收人是 AI 机器人账号时照常落库（现有类型都不过滤机器人接收人）。
package composition

import (
	"context"
	"testing"
	"time"

	followdomain "interestBar/pkg/domains/follow/domain"
	noticeapp "interestBar/pkg/domains/notice/application"
	noticedomain "interestBar/pkg/domains/notice/domain"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// noFollowEvents 不发通知事件的发布器：给不关心通知的 follow 用例用（见 setupFollow）。
type noFollowEvents struct{}

func (noFollowEvents) PublishFollowed(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type followNoticeEnv struct {
	*followEnv
	notice noticeapp.NoticeService
}

// setupFollowNotice 与生产装配一致：newFollowService（Redpanda 发布器）+ newNoticeService，并启动进程内唯一的通知 consumer。
func setupFollowNotice(t *testing.T) *followNoticeEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	startNotificationConsumer(t)

	deps := NewDeps()
	userFacade := userapp.NewUserFacade(newUserService(deps))
	noticeSvc := newNoticeService(deps)
	noticeSvc.SetUserFacade(&noticeUserFacade{delegate: userFacade})
	return &followNoticeEnv{
		followEnv: &followEnv{db: db, svc: newFollowService(deps, userFacade)},
		notice:    noticeSvc,
	}
}

// insertRecipient 插入一个会收到通知的用户，并注册它的通知行与未读计数的清理。
func (e *followNoticeEnv) insertRecipient(t *testing.T, role int) uuid.UUID {
	t.Helper()
	id := e.insertUser(t, role, 1, 0)
	testkit.CleanupRows(t, "domains.notification", "recipient_id", id)
	testkit.CleanupKeys(t, redispkg.GetNoticeUnreadKey(id))
	return id
}

type followNoticeRow struct {
	ID         uuid.UUID
	ActorID    uuid.UUID
	PostID     *uuid.UUID
	CommentID  *uuid.UUID
	Snippet    string
	IsRead     int16
	CreateTime time.Time
}

// followNotices 接收人名下全部被关注通知（按 actor 取）。
func (e *followNoticeEnv) followNotices(t *testing.T, recipient uuid.UUID) map[uuid.UUID][]followNoticeRow {
	t.Helper()
	var rows []followNoticeRow
	if err := e.db.Raw(`SELECT id, actor_id, post_id, comment_id, snippet, is_read, create_time FROM domains.notification
		WHERE recipient_id = ? AND notice_type = ? AND deleted = 0`, recipient, noticedomain.NoticeTypeFollow).
		Scan(&rows).Error; err != nil {
		t.Fatalf("query follow notices: %v", err)
	}
	byActor := make(map[uuid.UUID][]followNoticeRow, len(rows))
	for _, r := range rows {
		byActor[r.ActorID] = append(byActor[r.ActorID], r)
	}
	return byActor
}

// waitFollowNotice 等 actor → recipient 的通知行满足 cond，返回该行；同一对用户出现多行直接失败。
func (e *followNoticeEnv) waitFollowNotice(t *testing.T, recipient, actor uuid.UUID, msg string, cond func(followNoticeRow) bool) followNoticeRow {
	t.Helper()
	var got followNoticeRow
	testkit.Eventually(t, reviewNoticeWait, 200*time.Millisecond, msg, func() bool {
		rows := e.followNotices(t, recipient)[actor]
		if len(rows) > 1 {
			t.Fatalf("%s: %d follow notices for one follower, want at most 1: %+v", msg, len(rows), rows)
		}
		if len(rows) == 1 && cond(rows[0]) {
			got = rows[0]
			return true
		}
		return false
	})
	return got
}

func (e *followNoticeEnv) assertUnread(t *testing.T, userID uuid.UUID, want int64) {
	t.Helper()
	got, err := e.notice.GetUnreadCount(t.Context(), userID)
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if got != want {
		t.Errorf("unread count = %d, want %d", got, want)
	}
}

// waitUnreadPush 等 SSE 连接收到未读数 = want 的推送（推送值是全量，中间值跳过）。
func waitUnreadPush(t *testing.T, conn noticeapp.StreamConn, want int64) {
	t.Helper()
	timeout := time.After(reviewNoticeWait)
	last := int64(-1)
	for {
		select {
		case ev := <-conn.Events:
			if ev.Count == want {
				return
			}
			last = ev.Count
		case <-timeout:
			t.Fatalf("no unread-count push of %d within %v (last pushed value: %d)", want, reviewNoticeWait, last)
		}
	}
}

func unread(r followNoticeRow) bool { return r.IsRead == noticedomain.NoticeUnread }

// 首次关注 1 行未读 → 标已读 → 重复关注 / 取关不产生事件 → 取关后再关注复用同一行并重新未读。
// 每一步的未读数都经 SSE hub 推给接收人的连接（与点赞通知同一条推送路径）。
func TestFollowNotice_FollowUnfollowRefollow(t *testing.T) {
	t.Parallel()
	e := setupFollowNotice(t)
	ctx := t.Context()
	a, c := e.insertUser(t, 0, 1, 0), e.insertUser(t, 0, 1, 0)
	b := e.insertRecipient(t, 0)

	// 进程内只有这个用例装配 SSE hub（生产函数 newNoticeStreamHub：service ← hub、未读数读取器、consumer flush 回调）。
	hub := newNoticeStreamHub(e.notice)
	t.Cleanup(func() {
		redpanda.SetNoticeUnreadHook(nil)
		hub.Stop()
	})
	conn, ok := hub.Add(b)
	if !ok {
		t.Fatal("stream hub rejected the connection")
	}
	defer hub.Remove(b, conn.ID)
	if first, err := hub.Snapshot(ctx, b); err != nil || first.Count != 0 {
		t.Fatalf("initial snapshot = %+v err=%v, want unread 0", first, err)
	}

	// 1. 首次关注：1 行未读，recipient = 被关注者，actor = 关注者，没有帖子 / 评论 / 快照
	if vo, err := e.svc.Follow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("first follow: vo=%+v err=%v", vo, err)
	}
	first := e.waitFollowNotice(t, b, a, "first follow notice written", unread)
	if first.PostID != nil || first.CommentID != nil || first.Snippet != "" {
		t.Errorf("follow notice = %+v, want NULL post_id / comment_id and empty snippet", first)
	}
	waitUnreadPush(t, conn, 1)
	e.assertUnread(t, b, 1)

	// 列表：type=12 过滤命中，带关注者信息（前端跳关注者主页），没有帖子 / 评论字段
	list, err := e.notice.ListNotifications(ctx, b, []int16{noticedomain.NoticeTypeFollow}, 20, "")
	if err != nil {
		t.Fatalf("list notices type=12: %v", err)
	}
	if len(list.Notices) != 1 {
		t.Fatalf("list type=12 = %+v, want 1 item", list.Notices)
	}
	item := list.Notices[0]
	if item.ID != first.ID.String() || item.NoticeType != noticedomain.NoticeTypeFollow || item.IsRead ||
		item.PostID != "" || item.CommentID != "" || item.Snippet != "" {
		t.Errorf("list item = %+v, want unread follow notice %s without post / comment / snippet", item, first.ID)
	}
	if item.Actor == nil || item.Actor.ID != a.String() {
		t.Errorf("list item actor = %+v, want follower %s", item.Actor, a)
	}
	if other, err := e.notice.ListNotifications(ctx, b, []int16{noticedomain.NoticeTypeLikePost}, 20, ""); err != nil || len(other.Notices) != 0 {
		t.Errorf("list type=1: %+v err=%v, want no items", other, err)
	}

	// 2. 标已读，之后才能观察到「重新置为未读」
	if err := e.notice.MarkRead(ctx, b, []uuid.UUID{first.ID}); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	waitUnreadPush(t, conn, 0)

	// 3. 重复关注（幂等命中）与取关都不发事件。testkit 建的 topic 只有 1 个分区，事件按发布顺序消费：
	//    等后发的 c → b 落库后，a → b 的行仍是已读，说明前面两步没有产生事件。
	if _, err := e.svc.Follow(ctx, a, b); err != nil {
		t.Fatalf("repeat follow: %v", err)
	}
	if vo, err := e.svc.Unfollow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationNone {
		t.Fatalf("unfollow: vo=%+v err=%v", vo, err)
	}
	if _, err := e.svc.Follow(ctx, c, b); err != nil {
		t.Fatalf("follow by another user: %v", err)
	}
	e.waitFollowNotice(t, b, c, "second follower's notice written", unread)
	waitUnreadPush(t, conn, 1)
	notices := e.followNotices(t, b)
	if rows := notices[a]; len(notices) != 2 || len(rows) != 1 || rows[0].ID != first.ID || rows[0].IsRead != noticedomain.NoticeRead {
		t.Fatalf("after repeat follow + unfollow: notices = %+v, want a's row %s untouched (read) plus c's row", notices, first.ID)
	}

	// 4. 取关后再关注：复用同一行（id / create_time 不变）并重新置为未读，不新增
	if vo, err := e.svc.Follow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("follow again: vo=%+v err=%v", vo, err)
	}
	again := e.waitFollowNotice(t, b, a, "follow notice unread again", unread)
	if again.ID != first.ID || !again.CreateTime.Equal(first.CreateTime) {
		t.Errorf("re-follow notice id=%s create_time=%v, want the original row %s / %v", again.ID, again.CreateTime, first.ID, first.CreateTime)
	}
	waitUnreadPush(t, conn, 2)
	e.assertUnread(t, b, 2)
	if notices = e.followNotices(t, b); len(notices) != 2 || len(notices[a]) != 1 || len(notices[c]) != 1 {
		t.Errorf("final notices = %+v, want exactly one row per follower", notices)
	}
}

// 接收人是 AI 机器人账号（role=2）：与现有类型一致，不过滤，照常落库并计入未读。
func TestFollowNotice_BotRecipient(t *testing.T) {
	t.Parallel()
	e := setupFollowNotice(t)
	a := e.insertUser(t, 0, 1, 0)
	bot := e.insertRecipient(t, 2)

	if vo, err := e.svc.Follow(t.Context(), a, bot); err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("follow bot: vo=%+v err=%v", vo, err)
	}
	e.waitFollowNotice(t, bot, a, "follow notice for bot recipient written", unread)
}
