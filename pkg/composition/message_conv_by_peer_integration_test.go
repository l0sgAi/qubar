//go:build integration

// message_conv_by_peer_integration_test.go 按对方用户 ID 查会话经 composition 装配后的集成测试（#246）：
// 对方信息走 messageUserBriefReader → UserFacade.GetBriefs。覆盖：对方不存在 / 是机器人且没有会话时 conversation 为 null
// （本接口不校验对方用户状态），已有会话的对方被禁用后仍返回会话、peer 为 null（与会话列表一致）。
// 夹具复用 message_integration_test.go 的 setupMessage / insertUser。
package composition

import (
	"testing"

	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

func TestMessage_ConversationWithUser_PeerStatus(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	me := e.insertUser(t, "it246 me", 0, 1, 0)
	peer := e.insertUser(t, "it246 peer", 0, 1, 0)

	// 对方不存在、对方是机器人：没有会话，返回 null（不报 404）。
	for name, target := range map[string]uuid.UUID{
		"missing": sharedomain.NewID(),
		"bot":     e.insertUser(t, "it246 bot", 2, 1, 0),
	} {
		vo, err := e.svc.GetConversationWithUser(t.Context(), me, target)
		if err != nil || vo.Conversation != nil {
			t.Errorf("%s: vo = %+v (err %v), want conversation null", name, vo, err)
		}
	}

	sent, err := e.svc.Send(t.Context(), me, peer, "hello")
	if err != nil {
		t.Fatal(err)
	}
	vo, err := e.svc.GetConversationWithUser(t.Context(), me, peer)
	if err != nil || vo.Conversation == nil || vo.Conversation.Peer == nil || vo.Conversation.Peer.Username != "it246 peer" {
		t.Fatalf("with peer = %+v (err %v), want conversation with peer it246 peer", vo, err)
	}

	// 对方被禁用：会话仍返回，peer 为 null，peer_id 仍在（删用户缓存，让 GetBriefs 读到新状态）。
	if err := e.db.Exec(`UPDATE domains.users SET status = 0 WHERE id = ?`, peer).Error; err != nil {
		t.Fatal(err)
	}
	if err := redispkg.Del(redispkg.GetUserInfoKey(peer)); err != nil {
		t.Fatal(err)
	}
	vo, err = e.svc.GetConversationWithUser(t.Context(), me, peer)
	if err != nil || vo.Conversation == nil {
		t.Fatalf("with disabled peer = %+v (err %v), want conversation", vo, err)
	}
	if c := vo.Conversation; c.ConversationID != sent.ConversationID || c.Peer != nil || c.PeerID != peer.String() || c.LastMessage == nil {
		t.Errorf("with disabled peer = %+v (peer %+v), want conversation %s / peer nil / peer_id %s", c, c.Peer, sent.ConversationID, peer)
	}
}
