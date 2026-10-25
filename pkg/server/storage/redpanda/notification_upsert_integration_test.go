//go:build integration

// notification_upsert_integration_test.go 通知 upsert 去重冲突分支（#156）：
// 举报 / 疑似自伤（9 / 10）冲突时换新 id 并刷新 create_time（列表 id DESC 排到最前），其他类型保留原 id / create_time。
package redpanda_test

import (
	"testing"
	"time"

	noticedomain "interestBar/pkg/domains/notice/domain"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

type upsertedNotice struct {
	ID         uuid.UUID
	IsRead     int16
	Snippet    string
	CreateTime time.Time
}

func TestUpsertNotifications_ResurfacesReviewNotices(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	recipient := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.notification", "recipient_id", recipient)

	load := func(noticeType int16, postID uuid.UUID) []upsertedNotice {
		t.Helper()
		var rows []upsertedNotice
		if err := db.Raw(`SELECT id, is_read, snippet, create_time FROM domains.notification
			WHERE recipient_id = ? AND notice_type = ? AND post_id = ? AND deleted = 0`, recipient, noticeType, postID).
			Scan(&rows).Error; err != nil {
			t.Fatalf("query notification: %v", err)
		}
		return rows
	}

	cases := []struct {
		name       string
		noticeType int16
		resurface  bool
	}{
		{"reported", noticedomain.NoticeTypePostReported, true},
		{"self harm", noticedomain.NoticeTypePostSelfHarm, true},
		{"like post", noticedomain.NoticeTypeLikePost, false},
		{"post removed", noticedomain.NoticeTypePostRemoved, false},
	}
	for _, tc := range cases {
		postID, actor := sharedomain.NewID(), uuid.Nil
		first := sharedomain.NewID()
		if err := redpanda.UpsertNotificationForTest(first, recipient, actor, tc.noticeType, postID, "第一轮"); err != nil {
			t.Fatalf("%s: first upsert: %v", tc.name, err)
		}
		if err := db.Exec(`UPDATE domains.notification SET is_read = 1, create_time = create_time - interval '1 hour'
			WHERE id = ?`, first).Error; err != nil {
			t.Fatalf("%s: mark read: %v", tc.name, err)
		}
		before := load(tc.noticeType, postID)[0]

		second := sharedomain.NewID()
		if err := redpanda.UpsertNotificationForTest(second, recipient, actor, tc.noticeType, postID, "第二轮"); err != nil {
			t.Fatalf("%s: second upsert: %v", tc.name, err)
		}
		rows := load(tc.noticeType, postID)
		if len(rows) != 1 {
			t.Fatalf("%s: rows = %d, want 1 (dedup)", tc.name, len(rows))
		}
		got := rows[0]
		if got.IsRead != 0 || got.Snippet != "第二轮" {
			t.Fatalf("%s: row = %+v, want unread with refreshed snippet", tc.name, got)
		}
		if tc.resurface {
			if got.ID != second || !got.CreateTime.After(before.CreateTime) {
				t.Fatalf("%s: id = %s create_time = %v, want new id %s and refreshed create_time (was %v)",
					tc.name, got.ID, got.CreateTime, second, before.CreateTime)
			}
		} else if got.ID != first || !got.CreateTime.Equal(before.CreateTime) {
			t.Fatalf("%s: id = %s create_time = %v, want original id %s / create_time %v",
				tc.name, got.ID, got.CreateTime, first, before.CreateTime)
		}
	}
}
