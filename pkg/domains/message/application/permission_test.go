package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

func (f *fakeRepo) GetSendLimitState(context.Context, uuid.UUID, uuid.UUID) (*domain.SendLimitState, error) {
	f.limitCalls++
	if f.limitErr != nil {
		return nil, f.limitErr
	}
	st := f.limitState
	return &st, nil
}

// fakePermission 私信权限桩：所有用户都是同一个设置。
type fakePermission struct {
	perm  DMPermission
	err   error
	calls *int
}

func (f fakePermission) GetDMPermission(context.Context, uuid.UUID) (DMPermission, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.perm, f.err
}

// edgeFollows 按有向边判断关注关系：edges[[follower, followee]]。
type edgeFollows struct {
	edges map[[2]uuid.UUID]bool
	calls *int
}

func (f edgeFollows) IsFollowing(_ context.Context, follower, followee uuid.UUID) (bool, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.edges[[2]uuid.UUID{follower, followee}], nil
}

// TestSend_DMPermission 四个选项 × 关注关系组合：拒绝时不写库，并且不再查 5 条限制（不传 limit）。
func TestSend_DMPermission(t *testing.T) {
	me, peer := sharedomain.NewID(), sharedomain.NewID()
	relations := map[string]map[[2]uuid.UUID]bool{
		"none":           {},
		"peer follows":   {{peer, me}: true},
		"i follow":       {{me, peer}: true},
		"mutual follows": {{peer, me}: true, {me, peer}: true},
	}
	// want[permission][relation]：nil = 可以发。
	want := map[DMPermission]map[string]error{
		DMPermissionEveryone: {"none": nil, "peer follows": nil, "i follow": nil, "mutual follows": nil},
		DMPermissionFollowing: {
			"none": domain.ErrRecipientFollowingOnly, "peer follows": nil,
			"i follow": domain.ErrRecipientFollowingOnly, "mutual follows": nil,
		},
		DMPermissionMutual: {
			"none": domain.ErrRecipientMutualOnly, "peer follows": domain.ErrRecipientMutualOnly,
			"i follow": domain.ErrRecipientMutualOnly, "mutual follows": nil,
		},
		DMPermissionClosed: {
			"none": domain.ErrRecipientDMClosed, "peer follows": domain.ErrRecipientDMClosed,
			"i follow": domain.ErrRecipientDMClosed, "mutual follows": domain.ErrRecipientDMClosed,
		},
		DMPermission(9): { // 未知取值按关闭处理
			"none": domain.ErrRecipientDMClosed, "mutual follows": domain.ErrRecipientDMClosed,
		},
	}
	for perm, byRel := range want {
		for rel, wantErr := range byRel {
			repo := &fakeRepo{}
			svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, edgeFollows{edges: relations[rel]})
			svc.SetDMPermissionReader(fakePermission{perm: perm})
			_, err := svc.Send(t.Context(), me, peer, "hi")
			if !errors.Is(err, wantErr) {
				t.Errorf("perm=%d rel=%q: err = %v, want %v", perm, rel, err, wantErr)
				continue
			}
			if wantErr != nil {
				if repo.sendCalls != 0 {
					t.Errorf("perm=%d rel=%q: denied send reached the repo", perm, rel)
				}
				continue
			}
			// 允许发送后照常走 5 条限制：对方关注了我则不限制，否则 limit=5。
			wantLimit := UnfollowedSendLimit
			if relations[rel][[2]uuid.UUID{peer, me}] {
				wantLimit = 0
			}
			if repo.sendCalls != 1 || repo.gotSendLim != wantLimit {
				t.Errorf("perm=%d rel=%q: send calls=%d limit=%d, want 1 / %d", perm, rel, repo.sendCalls, repo.gotSendLim, wantLimit)
			}
		}
	}
}

// TestSend_DMPermissionOrderAndErrors 判断顺序：接收方状态 → 权限设置 → 5 条限制；读设置出错直接返回。
func TestSend_DMPermissionOrderAndErrors(t *testing.T) {
	me, peer := sharedomain.NewID(), sharedomain.NewID()
	boom := errors.New("boom")

	// 接收方不存在：不读权限设置。
	permCalls := 0
	svc := NewMessageService(&fakeRepo{}, fakeRecipients{status: RecipientNotFound}, &fakeUsers{}, fakeFollows{})
	svc.SetDMPermissionReader(fakePermission{perm: DMPermissionClosed, calls: &permCalls})
	if _, err := svc.Send(t.Context(), me, peer, "hi"); !errors.Is(err, domain.ErrRecipientNotFound) || permCalls != 0 {
		t.Errorf("not found: err = %v, permission reads = %d", err, permCalls)
	}

	// 关闭：不查关注关系（权限拒绝时不再查 5 条）。
	followCalls := 0
	svc = NewMessageService(&fakeRepo{}, fakeRecipients{}, &fakeUsers{}, edgeFollows{calls: &followCalls})
	svc.SetDMPermissionReader(fakePermission{perm: DMPermissionClosed})
	if _, err := svc.Send(t.Context(), me, peer, "hi"); !errors.Is(err, domain.ErrRecipientDMClosed) || followCalls != 0 {
		t.Errorf("closed: err = %v, follow reads = %d", err, followCalls)
	}

	// 读设置失败。
	repo := &fakeRepo{}
	svc = NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})
	svc.SetDMPermissionReader(fakePermission{err: boom})
	if _, err := svc.Send(t.Context(), me, peer, "hi"); !errors.Is(err, boom) || repo.sendCalls != 0 {
		t.Errorf("permission error: err = %v, sends = %d", err, repo.sendCalls)
	}

	// 未注入读取端口：所有人可发（#240 之前的行为）。
	repo = &fakeRepo{}
	if _, err := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{}).Send(t.Context(), me, peer, "hi"); err != nil || repo.sendCalls != 1 {
		t.Errorf("no reader: err = %v, sends = %d", err, repo.sendCalls)
	}
}

// TestGetConversationWithUser_CanSend can_send 与发送接口同一口径，无会话时也返回。
func TestGetConversationWithUser_CanSend(t *testing.T) {
	me, peer := sharedomain.NewID(), sharedomain.NewID()
	boom := errors.New("boom")
	peerFollows := map[[2]uuid.UUID]bool{{peer, me}: true}

	for _, tc := range []struct {
		name       string
		recipient  fakeRecipients
		perm       DMPermission
		edges      map[[2]uuid.UUID]bool
		limit      domain.SendLimitState
		limitErr   error
		wantErr    error
		wantReason CannotSendReason // 空 = 可以发
		wantLimit  int              // GetSendLimitState 调用次数
	}{
		{name: "everyone fresh", wantLimit: 1},
		{name: "everyone 4 sent", limit: domain.SendLimitState{SentCount: 4}, wantLimit: 1},
		{name: "everyone limit reached", limit: domain.SendLimitState{SentCount: 5}, wantReason: ReasonLimitReached, wantLimit: 1},
		{name: "everyone limit but replied", limit: domain.SendLimitState{SentCount: 9, PeerReplied: true}, wantLimit: 1},
		{name: "everyone followed", edges: peerFollows, limit: domain.SendLimitState{SentCount: 9}},
		{name: "following denied", perm: DMPermissionFollowing, limit: domain.SendLimitState{SentCount: 9}, wantReason: ReasonFollowingOnly},
		{name: "following ok", perm: DMPermissionFollowing, edges: peerFollows},
		{name: "mutual denied", perm: DMPermissionMutual, edges: peerFollows, wantReason: ReasonMutualOnly},
		{name: "closed", perm: DMPermissionClosed, wantReason: ReasonDMClosed},
		{name: "not found", recipient: fakeRecipients{status: RecipientNotFound}, perm: DMPermissionClosed, wantReason: ReasonRecipientNotFound},
		{name: "bot", recipient: fakeRecipients{status: RecipientBot}, wantReason: ReasonRecipientUnavailable},
		{name: "recipient error", recipient: fakeRecipients{err: boom}, wantErr: boom},
		{name: "limit state error", limitErr: boom, wantErr: boom, wantLimit: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, hasConv := range []bool{false, true} {
				repo := &fakeRepo{limitState: tc.limit, limitErr: tc.limitErr, directErr: domain.ErrConversationNotFound}
				if hasConv {
					repo.direct, repo.directErr = &domain.ConversationEntry{ConversationID: sharedomain.NewID(), PeerID: peer}, nil
				}
				svc := NewMessageService(repo, tc.recipient, &fakeUsers{}, edgeFollows{edges: tc.edges})
				svc.SetDMPermissionReader(fakePermission{perm: tc.perm})
				vo, err := svc.GetConversationWithUser(t.Context(), me, peer)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("conv=%v: err = %v, want %v", hasConv, err, tc.wantErr)
				}
				if repo.limitCalls != tc.wantLimit || repo.sendCalls != 0 || repo.writeCalls != 0 {
					t.Errorf("conv=%v: limit reads=%d sends=%d writes=%d, want %d / 0 / 0",
						hasConv, repo.limitCalls, repo.sendCalls, repo.writeCalls, tc.wantLimit)
				}
				if tc.wantErr != nil {
					continue
				}
				if (vo.Conversation != nil) != hasConv {
					t.Errorf("conv=%v: conversation = %+v", hasConv, vo.Conversation)
				}
				if tc.wantReason == "" {
					if !vo.CanSend || vo.CannotSendReason != nil {
						t.Errorf("conv=%v: can_send=%v reason=%v, want true / nil", hasConv, vo.CanSend, vo.CannotSendReason)
					}
				} else if vo.CanSend || vo.CannotSendReason == nil || *vo.CannotSendReason != tc.wantReason {
					t.Errorf("conv=%v: can_send=%v reason=%v, want false / %s", hasConv, vo.CanSend, vo.CannotSendReason, tc.wantReason)
				}
			}
		})
	}
}

func TestPeerConversationVO_JSON(t *testing.T) {
	reason := ReasonDMClosed
	for _, tc := range []struct {
		vo   PeerConversationVO
		want string
	}{
		{PeerConversationVO{SendPermissionVO: SendPermissionVO{CanSend: true}}, `{"conversation":null,"can_send":true,"cannot_send_reason":null}`},
		{PeerConversationVO{SendPermissionVO: SendPermissionVO{CannotSendReason: &reason}}, `{"conversation":null,"can_send":false,"cannot_send_reason":"dm_closed"}`},
	} {
		b, err := json.Marshal(tc.vo)
		if err != nil || string(b) != tc.want {
			t.Errorf("json = %s (err %v), want %s", b, err, tc.want)
		}
	}
}
