//go:build integration

// dm_permission_integration_test.go 「我能否给对方发私信」所用的只读限制状态在仓储层的集成测试（#240）：
// GetSendLimitState 与 SendDirect 事务内的未关注发送限制同一口径（无会话为零值、按方向取发送方条数、对方回复后 peer_replied）。
// 四个权限选项 × 关注关系的端到端组合见 pkg/composition/message_dm_permission_integration_test.go。
package infrastructure_test

import (
	"testing"

	"interestBar/pkg/domains/message/domain"
)

func TestGetSendLimitState(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	state := func(from, to int) domain.SendLimitState {
		t.Helper()
		st, err := e.repo.GetSendLimitState(t.Context(), u[from], u[to])
		if err != nil || st == nil {
			t.Fatalf("GetSendLimitState: %+v err=%v", st, err)
		}
		return *st
	}

	// 没有会话：零值，不建会话。
	if st := state(0, 1); st != (domain.SendLimitState{}) {
		t.Fatalf("no conversation: %+v", st)
	}
	var n int64
	if err := e.db.Raw(`SELECT count(*) FROM domains.conversation WHERE user_low IN (?, ?)`, a, b).Scan(&n).Error; err != nil || n != 0 {
		t.Fatalf("conversation created by a read: n=%d err=%v", n, err)
	}

	// a 发 3 条：a→b 计 3 条、b 没回复；b→a 方向 b 发了 0 条、a 发过消息即 peer_replied。
	for i := 0; i < 3; i++ {
		if _, err := e.sendLimited(t, a, b, testLimit); err != nil {
			t.Fatal(err)
		}
	}
	if st := state(0, 1); st != (domain.SendLimitState{SentCount: 3}) {
		t.Errorf("a->b after 3 sends: %+v", st)
	}
	if st := state(1, 0); st != (domain.SendLimitState{SentCount: 0, PeerReplied: true}) {
		t.Errorf("b->a after a's sends: %+v", st)
	}

	// b 回复一条后：a→b peer_replied。
	if _, err := e.sendLimited(t, b, a, 0); err != nil {
		t.Fatal(err)
	}
	if st := state(0, 1); st != (domain.SendLimitState{SentCount: 3, PeerReplied: true}) {
		t.Errorf("a->b after b replied: %+v", st)
	}
}
