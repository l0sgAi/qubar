package redpanda

import (
	"encoding/json"
	"strings"
	"testing"

	noticedomain "interestBar/pkg/domains/notice/domain"

	"github.com/google/uuid"
)

// 被关注通知（#219）：接收人取事件自带的 RecipientID，行里不带帖子 / 评论 / 快照。
// 只含 follow 事件时不需要反查帖子 / 评论，buildNotificationRows 不访问数据库。
func TestBuildNotificationRows_Follow(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	strayPost, strayComment := uuid.New(), uuid.New()
	follow := func(actor uuid.UUID, recipient *uuid.UUID) NotificationEventMessage {
		return NotificationEventMessage{Type: NoticeTypeFollow, ActorID: actor, RecipientID: recipient}
	}
	stray := follow(a, &d)
	stray.PostID, stray.CommentID, stray.Snippet = &strayPost, &strayComment, "不应该落库"

	rows, unread := buildNotificationRows([]NotificationEventMessage{
		follow(a, &b),
		follow(a, &b), // 同一批里的重复事件（at-least-once 重复投递）：只留一行
		follow(c, &b), // 另一个关注者：各自一行
		follow(b, &b), // 自己关注自己（服务层已拒绝，这里兜底）：R1 丢弃
		follow(a, nil),
		stray, // 带了多余的帖子 / 评论 / 快照：落库前清掉
	})

	type key struct{ recipient, actor uuid.UUID }
	got := make(map[key]noticeRow, len(rows))
	for _, r := range rows {
		got[key{r.RecipientID, r.ActorID}] = r
	}
	if len(rows) != 3 || len(got) != 3 {
		t.Fatalf("rows = %+v, want exactly (b<-a), (b<-c), (d<-a)", rows)
	}
	for _, k := range []key{{b, a}, {b, c}, {d, a}} {
		r, ok := got[k]
		if !ok {
			t.Fatalf("missing row recipient=%s actor=%s in %+v", k.recipient, k.actor, rows)
		}
		if r.NoticeType != noticedomain.NoticeTypeFollow || r.PostID != nil || r.CommentID != nil || r.Snippet != "" {
			t.Errorf("row %+v, want notice_type 12 with nil post / comment and empty snippet", r)
		}
		if r.ID == uuid.Nil {
			t.Errorf("row %+v has no id (every insert needs an explicit UUIDv7)", r)
		}
	}
	if unread[b] != 2 || unread[d] != 1 || len(unread) != 2 {
		t.Errorf("unread deltas = %v, want b:2 d:1", unread)
	}
}

// RecipientID 是新增字段：旧版本发出的消息没有它，解码结果与处理不受影响；非 follow 消息编码后也不带它。
func TestNotificationEventMessage_RecipientIDCompat(t *testing.T) {
	actor, post, recipient := uuid.New(), uuid.New(), uuid.New()

	legacy := `{"type":"like_post","actor_id":"` + actor.String() + `","post_id":"` + post.String() + `","ts":1}`
	var old NotificationEventMessage
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatalf("unmarshal legacy message: %v", err)
	}
	if old.RecipientID != nil || old.Type != NoticeTypeLikePost || old.ActorID != actor || old.PostID == nil || *old.PostID != post {
		t.Errorf("legacy message decoded as %+v", old)
	}

	likeJSON, err := json.Marshal(NotificationEventMessage{Type: NoticeTypeLikePost, ActorID: actor, PostID: &post, Ts: 1})
	if err != nil {
		t.Fatalf("marshal like message: %v", err)
	}
	if strings.Contains(string(likeJSON), "recipient_id") {
		t.Errorf("non-follow message carries recipient_id: %s", likeJSON)
	}

	followJSON, err := json.Marshal(NotificationEventMessage{Type: NoticeTypeFollow, ActorID: actor, RecipientID: &recipient, Ts: 1})
	if err != nil {
		t.Fatalf("marshal follow message: %v", err)
	}
	var decoded NotificationEventMessage
	if err := json.Unmarshal(followJSON, &decoded); err != nil {
		t.Fatalf("unmarshal follow message: %v", err)
	}
	if decoded.RecipientID == nil || *decoded.RecipientID != recipient || decoded.PostID != nil || decoded.CommentID != nil {
		t.Errorf("follow message round trip = %+v (json %s)", decoded, followJSON)
	}
}
