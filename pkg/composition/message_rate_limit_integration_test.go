//go:build integration

// message_rate_limit_integration_test.go 私信发送频率限制与发送方状态（#242）经 composition 装配后的端到端集成测试：
// 真实 PG + Redis，上限读配置 message.send_rate（configs/config.dev.yaml），发送方状态走 messageRecipientChecker。
package composition

import (
	"errors"
	"testing"

	"interestBar/pkg/conf"
	messagedomain "interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"
)

func TestMessage_SenderMustBeActive(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	peer := e.insertUser(t, "it242 peer", 0, 1, 0)
	for _, tc := range []struct {
		name            string
		status, deleted int
	}{
		{"disabled", 0, 0},
		{"deleted", 1, 1},
	} {
		sender := e.insertUser(t, "it242 "+tc.name, 0, tc.status, tc.deleted)
		if _, err := e.svc.Send(t.Context(), sender, peer, "hi"); !errors.Is(err, messagedomain.ErrSenderUnavailable) {
			t.Errorf("%s sender: err = %v, want ErrSenderUnavailable", tc.name, err)
		}
	}
	if _, err := e.svc.Send(t.Context(), sharedomain.NewID(), peer, "hi"); !errors.Is(err, messagedomain.ErrSenderUnavailable) {
		t.Errorf("missing sender: err = %v, want ErrSenderUnavailable", err)
	}
	if list, err := e.svc.ListConversations(t.Context(), peer, "", 20); err != nil || len(list.Items) != 0 {
		t.Fatalf("peer conversations = %+v (err %v), want none", list, err)
	}
}

func TestMessage_SendRatePerMinuteFromConfig(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	a := e.insertUser(t, "it242 a", 0, 1, 0)
	b := e.insertUser(t, "it242 b", 0, 1, 0)
	perMinute := conf.Config.Message.SendRate.PerMinuteOrDefault()

	// b 回复过之后不受 #239 未关注 5 条限制，只剩频率限制。
	e.mustSend(t, a, b)
	e.mustSend(t, b, a)
	for i := 1; i < perMinute; i++ {
		e.mustSend(t, a, b)
	}
	_, err := e.svc.Send(t.Context(), a, b, "one too many")
	if !errors.Is(err, messagedomain.ErrSendRateLimited) {
		t.Fatalf("send #%d err = %v, want ErrSendRateLimited", perMinute+1, err)
	}
	var sent int
	if err := e.db.Raw(`SELECT sent_count FROM domains.conversation_member WHERE user_id = ?`, a).Scan(&sent).Error; err != nil || sent != perMinute {
		t.Errorf("sent_count = %d (err %v), want %d", sent, err, perMinute)
	}
	// 频率按发送者计：b 不受 a 的计数影响。
	e.mustSend(t, b, a)
}
