//go:build integration

// message_dm_permission_integration_test.go 私信权限设置经 composition 装配后的端到端集成测试（#240）：真实 PG + Redis，
// 接收方设置走 PUT /user/settings 同一个 SettingService → messageDMPermissionReader，关注关系走真实 FollowService。
// 覆盖：四个选项 × 关注关系组合（发送结果与 can_send 一致）、拒绝不建会话、已有会话且对方回复过也受约束、
// 先看权限再看 5 条限制、can_send 计入 5 条限制。
package composition

import (
	"errors"
	"fmt"
	"testing"

	messageapp "interestBar/pkg/domains/message/application"
	messagedomain "interestBar/pkg/domains/message/domain"
	userapp "interestBar/pkg/domains/user/application"
	userdomain "interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
)

// setDMPermission 以 user 本人身份修改私信权限（与 PUT /user/settings 同一服务）。
func (e *messageEnv) setDMPermission(t *testing.T, user uuid.UUID, perm int16) {
	t.Helper()
	v := int(perm)
	if _, err := e.settings.UpdateSettings(t.Context(), user, userapp.UpdateSettingsInput{DMPermission: &v}); err != nil {
		t.Fatalf("set dm_permission=%d: %v", perm, err)
	}
}

// canSend 调 GET /message/conversations/with/:user_id 同一服务方法。
func (e *messageEnv) canSend(t *testing.T, from, to uuid.UUID) *messageapp.PeerConversationVO {
	t.Helper()
	vo, err := e.svc.GetConversationWithUser(t.Context(), from, to)
	if err != nil {
		t.Fatalf("GetConversationWithUser %s -> %s: %v", from, to, err)
	}
	return vo
}

func assertCanSend(t *testing.T, vo *messageapp.PeerConversationVO, want messageapp.CannotSendReason) {
	t.Helper()
	switch {
	case want == "" && (!vo.CanSend || vo.CannotSendReason != nil):
		t.Errorf("can_send=%v reason=%v, want true / null", vo.CanSend, vo.CannotSendReason)
	case want != "" && (vo.CanSend || vo.CannotSendReason == nil || *vo.CannotSendReason != want):
		t.Errorf("can_send=%v reason=%v, want false / %s", vo.CanSend, vo.CannotSendReason, want)
	}
}

func TestMessageDMPermission_OptionsByFollowRelation(t *testing.T) {
	t.Parallel()
	type relation struct {
		name                   string
		peerFollowsMe, iFollow bool
	}
	relations := []relation{{"none", false, false}, {"peer follows me", true, false}, {"i follow peer", false, true}, {"mutual", true, true}}

	for _, opt := range []struct {
		perm int16
		// want 按 relations 顺序：空 = 可以发，否则为 cannot_send_reason。
		want [4]messageapp.CannotSendReason
	}{
		{perm: userdomain.DMPermissionEveryone},
		{perm: userdomain.DMPermissionFollowing, want: [4]messageapp.CannotSendReason{
			messageapp.ReasonFollowingOnly, "", messageapp.ReasonFollowingOnly, ""}},
		{perm: userdomain.DMPermissionMutual, want: [4]messageapp.CannotSendReason{
			messageapp.ReasonMutualOnly, messageapp.ReasonMutualOnly, messageapp.ReasonMutualOnly, ""}},
		{perm: userdomain.DMPermissionClosed, want: [4]messageapp.CannotSendReason{
			messageapp.ReasonDMClosed, messageapp.ReasonDMClosed, messageapp.ReasonDMClosed, messageapp.ReasonDMClosed}},
	} {
		for i, rel := range relations {
			t.Run(fmt.Sprintf("perm=%d/%s", opt.perm, rel.name), func(t *testing.T) {
				t.Parallel()
				e := setupMessage(t)
				ctx := t.Context()
				me := e.insertUser(t, "it240 sender", 0, 1, 0)
				peer := e.insertUser(t, "it240 receiver", 0, 1, 0)
				e.setDMPermission(t, peer, opt.perm)
				if rel.peerFollowsMe {
					if _, err := e.follow.Follow(ctx, peer, me); err != nil {
						t.Fatal(err)
					}
				}
				if rel.iFollow {
					if _, err := e.follow.Follow(ctx, me, peer); err != nil {
						t.Fatal(err)
					}
				}
				want := opt.want[i]

				// 发送前（没有会话）：can_send 已可用。
				before := e.canSend(t, me, peer)
				if before.Conversation != nil {
					t.Fatalf("conversation before any send: %+v", before.Conversation)
				}
				assertCanSend(t, before, want)

				_, err := e.svc.Send(ctx, me, peer, "hi")
				if want == "" {
					if err != nil {
						t.Fatalf("send: %v", err)
					}
					after := e.canSend(t, me, peer)
					if after.Conversation == nil {
						t.Fatal("conversation missing after send")
					}
					assertCanSend(t, after, "")
					return
				}
				if err == nil {
					t.Fatal("send succeeded, want rejected")
				}
				if reason, ok := reasonOfSendErr(err); !ok || reason != want {
					t.Fatalf("send err = %v, want reason %s", err, want)
				}
				// 拒绝不建会话。
				if after := e.canSend(t, me, peer); after.Conversation != nil {
					t.Errorf("rejected send created a conversation: %+v", after.Conversation)
				}
			})
		}
	}
}

// reasonOfSendErr 把发送拒绝错误映射成 can_send 的原因，用来核对两者一致。
func reasonOfSendErr(err error) (messageapp.CannotSendReason, bool) {
	for e, r := range map[error]messageapp.CannotSendReason{
		messagedomain.ErrRecipientFollowingOnly: messageapp.ReasonFollowingOnly,
		messagedomain.ErrRecipientMutualOnly:    messageapp.ReasonMutualOnly,
		messagedomain.ErrRecipientDMClosed:      messageapp.ReasonDMClosed,
		messagedomain.ErrSendLimitReached:       messageapp.ReasonLimitReached,
	} {
		if errors.Is(err, e) {
			return r, true
		}
	}
	return "", false
}

// 已有会话、对方回复过也不豁免：对方改为关闭 / 仅关注的人后，我不能再发；改回所有人后恢复（对方回复过，不受 5 条限制）。
func TestMessageDMPermission_ExistingConversationNotExempt(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	me := e.insertUser(t, "it240 a", 0, 1, 0)
	peer := e.insertUser(t, "it240 b", 0, 1, 0)
	e.mustSend(t, me, peer)
	e.mustSend(t, peer, me) // 对方回复过

	e.setDMPermission(t, peer, userdomain.DMPermissionClosed)
	if _, err := e.svc.Send(t.Context(), me, peer, "hi"); !errors.Is(err, messagedomain.ErrRecipientDMClosed) {
		t.Fatalf("send to closed err = %v", err)
	}
	vo := e.canSend(t, me, peer)
	if vo.Conversation == nil {
		t.Fatal("existing conversation not returned")
	}
	assertCanSend(t, vo, messageapp.ReasonDMClosed)
	// 设置只约束发给设置者的消息：对方仍能给我发。
	e.mustSend(t, peer, me)

	e.setDMPermission(t, peer, userdomain.DMPermissionFollowing)
	if _, err := e.svc.Send(t.Context(), me, peer, "hi"); !errors.Is(err, messagedomain.ErrRecipientFollowingOnly) {
		t.Fatalf("send to following-only err = %v", err)
	}

	e.setDMPermission(t, peer, userdomain.DMPermissionEveryone)
	assertCanSend(t, e.canSend(t, me, peer), "")
	assertRemaining(t, e.mustSend(t, me, peer), nil)
}

// 先看权限再看 5 条：所有人模式下发满 5 条后 can_send=false（dm_limit_reached）；
// 对方再改为关闭时原因变为 dm_closed、发送返回关闭错误；改为仅关注的人并关注我后不再受 5 条限制。
func TestMessageDMPermission_PermissionBeforeSendLimit(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	ctx := t.Context()
	me := e.insertUser(t, "it240 a", 0, 1, 0)
	peer := e.insertUser(t, "it240 b", 0, 1, 0)

	for i := 0; i < messageapp.UnfollowedSendLimit; i++ {
		e.mustSend(t, me, peer)
	}
	assertCanSend(t, e.canSend(t, me, peer), messageapp.ReasonLimitReached)
	e.assertRejected(t, me, peer)

	e.setDMPermission(t, peer, userdomain.DMPermissionClosed)
	assertCanSend(t, e.canSend(t, me, peer), messageapp.ReasonDMClosed)
	if _, err := e.svc.Send(ctx, me, peer, "hi"); !errors.Is(err, messagedomain.ErrRecipientDMClosed) {
		t.Fatalf("closed + limit reached: err = %v, want ErrRecipientDMClosed", err)
	}

	e.setDMPermission(t, peer, userdomain.DMPermissionFollowing)
	if _, err := e.follow.Follow(ctx, peer, me); err != nil {
		t.Fatal(err)
	}
	assertCanSend(t, e.canSend(t, me, peer), "")
	assertRemaining(t, e.mustSend(t, me, peer), nil)
}
