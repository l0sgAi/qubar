//go:build integration

// message_integration_test.go 私信经 composition 装配后的端到端集成测试（#238）：真实 PG + Redis，
// 接收方校验走 messageRecipientChecker → UserService.GetByID，对方信息走 messageUserBriefReader → UserFacade.GetBriefs。
// 覆盖：普通用户可收私信、AI 机器人账号不可收（403 语义）、不存在 / 已禁用 / 已删除用户不可收（404 语义）、
// 会话列表回填对方简要信息。
package composition

import (
	"errors"
	"testing"

	followapp "interestBar/pkg/domains/follow/application"
	messageapp "interestBar/pkg/domains/message/application"
	messagedomain "interestBar/pkg/domains/message/domain"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type messageEnv struct {
	db     *gorm.DB
	svc    messageapp.MessageService
	follow followapp.FollowService
	// settings 用户设置（#240 私信权限，与 server.go 一样经 wireMessageDMPermission 接入 message）。
	settings userapp.SettingService
}

func setupMessage(t *testing.T) *messageEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	deps := NewDeps()
	userSvc := newUserService(deps)
	userFacade := userapp.NewUserFacade(userSvc)
	// 关注通知不在这里验证，发布器用空实现（原因见 follow_integration_test.go 的 setupFollow）。
	settingSvc := newUserSettingService(deps)
	followSvc := newFollowServiceWithEvents(deps, userFacade, settingSvc, noFollowEvents{})
	messageSvc := newMessageService(deps, userSvc, userFacade, followSvc)
	wireMessageDMPermission(messageSvc, settingSvc)
	return &messageEnv{db: db, svc: messageSvc, follow: followSvc, settings: settingSvc}
}

// insertUser 插入一个用户（role 0=普通 / 2=AI 机器人；status 1=正常；deleted 0=未删除），注册行、缓存与私信数据的清理。
func (e *messageEnv) insertUser(t *testing.T, name string, role, status, deleted int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := e.db.Exec(`INSERT INTO domains.users (id, username, email, role, status, deleted) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, id.String()+"@it.qubar.local", role, status, deleted).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	testkit.CleanupRows(t, "domains.conversation", "user_low", id)
	testkit.CleanupRows(t, "domains.conversation_member", "user_id", id)
	testkit.CleanupRows(t, "domains.message", "sender_id", id)
	testkit.CleanupRows(t, "domains.user_follow", "follower_id", id)
	testkit.CleanupRows(t, "domains.user_follow_stat", "user_id", id)
	testkit.CleanupRows(t, "domains.user_setting", "user_id", id)
	testkit.CleanupKeys(t, redispkg.GetUserInfoKey(id))
	return id
}

func TestMessage_RecipientRules(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	me := e.insertUser(t, "it238 sender", 0, 1, 0)

	for _, tc := range []struct {
		name    string
		target  uuid.UUID
		wantErr error
	}{
		{"bot", e.insertUser(t, "it238 bot", 2, 1, 0), messagedomain.ErrRecipientUnavailable},
		{"disabled", e.insertUser(t, "it238 disabled", 0, 0, 0), messagedomain.ErrRecipientNotFound},
		{"deleted", e.insertUser(t, "it238 deleted", 0, 1, 1), messagedomain.ErrRecipientNotFound},
		{"missing", sharedomain.NewID(), messagedomain.ErrRecipientNotFound},
		{"self", me, messagedomain.ErrCannotMessageSelf},
	} {
		if _, err := e.svc.Send(t.Context(), me, tc.target, "hi"); !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
	// 上面都没有写入任何会话。
	if list, err := e.svc.ListConversations(t.Context(), me, "", 20); err != nil || len(list.Items) != 0 {
		t.Fatalf("conversations after rejected sends = %+v (err %v), want none", list, err)
	}

	peer := e.insertUser(t, "it238 peer", 0, 1, 0)
	sent, err := e.svc.Send(t.Context(), me, peer, "hello")
	if err != nil {
		t.Fatalf("send to normal user: %v", err)
	}
	list, err := e.svc.ListConversations(t.Context(), peer, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("peer conversations = %d, want 1", len(list.Items))
	}
	item := list.Items[0]
	if item.ConversationID != sent.ConversationID || item.Peer == nil || item.Peer.Username != "it238 sender" ||
		item.PeerID != me.String() || item.UnreadCount != 1 || item.LastMessage.Content != "hello" {
		t.Errorf("peer conversation item = %+v (peer %+v)", item, item.Peer)
	}
}
