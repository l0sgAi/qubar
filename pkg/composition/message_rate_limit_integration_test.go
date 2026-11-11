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

// 幂等重放（#248）与频率限制：重放不消耗配额；已达每分钟上限时用同一 client_msg_id 重试返回原消息而不是 429，新键仍 429。
func TestMessage_ReplayBypassesSendRate(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	a := e.insertUser(t, "it242 replay a", 0, 1, 0)
	b := e.insertUser(t, "it242 replay b", 0, 1, 0)
	perMinute := conf.Config.Message.SendRate.PerMinuteOrDefault()
	key := sharedomain.NewID().String()

	orig := e.mustSendKeyed(t, a, b, "first", key)
	e.mustSend(t, b, a) // b 回复过：a 不受 #239 未关注限制
	// 重放 3 次：不占配额。
	for i := 0; i < 3; i++ {
		if vo := e.mustSendKeyed(t, a, b, "retry", key); vo.ID != orig.ID {
			t.Fatalf("replay #%d id = %s, want %s", i+1, vo.ID, orig.ID)
		}
	}
	// 重放没消耗配额：一分钟内还能再发满 perMinute-1 条新消息。
	for i := 1; i < perMinute; i++ {
		e.mustSendKeyed(t, a, b, "new", sharedomain.NewID().String())
	}
	if _, err := e.svc.SendWithClientMsgID(t.Context(), a, b, "over", sharedomain.NewID().String()); !errors.Is(err, messagedomain.ErrSendRateLimited) {
		t.Fatalf("new key at limit: err = %v, want ErrSendRateLimited", err)
	}
	// 已达上限时重放：返回原消息，不报 429。
	if vo := e.mustSendKeyed(t, a, b, "retry at limit", key); vo.ID != orig.ID || vo.Content != "first" {
		t.Fatalf("replay at limit = %+v, want original %s", vo.MessageVO, orig.ID)
	}
	var sent int
	if err := e.db.Raw(`SELECT sent_count FROM domains.conversation_member WHERE user_id = ?`, a).Scan(&sent).Error; err != nil || sent != perMinute {
		t.Errorf("sent_count = %d (err %v), want %d", sent, err, perMinute)
	}
}
