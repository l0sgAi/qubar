// mention_test.go 发帖 @提及 校验的圈子作用域兜底单元测试。
//
// 覆盖设计文档 P0-8 的"兜底剔除"：越圈机器人剔除 / 本圈机器人保留 / 全局机器人与
// 普通用户保留 / 停用或已删除的机器人（全局、圈内）剔除（#171）/ GetAgentMentionScopes 失败
// fail-open（跳过剔除不报错）。
// @选人列表已在 ES 侧过滤，此层只防手搓 ID 构造请求（场景为正常路径的静默剔除语义）。
package application

import (
	"context"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	conf.Config = &conf.AppConfig{}
	conf.Config.Notice.MentionMax = 10
	logger.Log = zap.NewNop()
	m.Run()
}

// 测试固定标识。
var (
	postCircleID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	otherCircleID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb")
	mentionActor  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")

	normalUserID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000010")
	globalBotID   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000011")
	ownCircleBot  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000012")
	otherCirclBot = uuid.MustParse("0192a0d0-0000-7000-8000-000000000013")
	ghostUserID   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000014")
	disabledGBot  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000015") // 停用/已删除的全局机器人
	disabledCBot  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000016") // 停用/已删除的本圈机器人
)

// fakePostUserFacade UserFacade fake：预设存在性与机器人圈子绑定。
type fakePostUserFacade struct {
	briefs    map[string]UserBrief
	scopes    map[uuid.UUID]AgentMentionScope
	circleErr error // 非 nil 时模拟 GetAgentMentionScopes 失败（fail-open 断言）
}

func (f *fakePostUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error) {
	out := make(map[string]UserBrief, len(userIDs))
	for _, id := range userIDs {
		if b, ok := f.briefs[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

func (f *fakePostUserFacade) GetBrief(ctx context.Context, userID string) (*UserBrief, error) {
	if b, ok := f.briefs[userID]; ok {
		return &b, nil
	}
	return nil, nil
}

func (f *fakePostUserFacade) GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error) {
	if f.circleErr != nil {
		return nil, f.circleErr
	}
	out := make(map[uuid.UUID]AgentMentionScope, len(userIDs))
	for _, id := range userIDs {
		if sc, ok := f.scopes[id]; ok {
			out[id] = sc
		}
	}
	return out, nil
}

// newMentionFacade 组装常规 fake：全部候选存在；ownCircleBot 绑定本圈，
// otherCirclBot 绑定他圈，globalBotID/normalUserID 无记录；disabledGBot / disabledCBot
// 为不可 @ 的全局 / 本圈机器人。
func newMentionFacade() *fakePostUserFacade {
	return &fakePostUserFacade{
		briefs: map[string]UserBrief{
			normalUserID.String():  {ID: normalUserID.String(), Username: "普通用户"},
			globalBotID.String():   {ID: globalBotID.String(), Username: "全局机器人"},
			ownCircleBot.String():  {ID: ownCircleBot.String(), Username: "本圈机器人"},
			otherCirclBot.String(): {ID: otherCirclBot.String(), Username: "他圈机器人"},
			disabledGBot.String():  {ID: disabledGBot.String(), Username: "停用的全局机器人"},
			disabledCBot.String():  {ID: disabledCBot.String(), Username: "停用的本圈机器人"},
		},
		scopes: map[uuid.UUID]AgentMentionScope{
			ownCircleBot:  {CircleID: postCircleID, Mentionable: true},
			otherCirclBot: {CircleID: otherCircleID, Mentionable: true},
			disabledGBot:  {CircleID: uuid.Nil, Mentionable: false},
			disabledCBot:  {CircleID: postCircleID, Mentionable: false},
		},
	}
}

// TestFilterMentionUserIDs_AgentScope 越圈机器人剔除；本圈机器人/全局机器人/普通用户保留；
// 不存在的用户被存在性校验过滤。
func TestFilterMentionUserIDs_AgentScope(t *testing.T) {
	svc := &postServiceImpl{userFacade: newMentionFacade()}
	got := svc.filterMentionUserIDs(context.Background(), mentionActor, []uuid.UUID{
		normalUserID, globalBotID, ownCircleBot, otherCirclBot, ghostUserID,
	}, postCircleID)

	want := map[uuid.UUID]bool{normalUserID: true, globalBotID: true, ownCircleBot: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected id %s in result %v", id, got)
		}
	}
}

// TestFilterMentionUserIDs_DisabledAgents 停用 / 已删除的机器人（全局、本圈）一律剔除（#171），
// 不论帖子在哪个圈子（含 Nil 圈的草稿）。
func TestFilterMentionUserIDs_DisabledAgents(t *testing.T) {
	svc := &postServiceImpl{userFacade: newMentionFacade()}
	for _, circleID := range []uuid.UUID{postCircleID, otherCircleID, uuid.Nil} {
		got := svc.filterMentionUserIDs(context.Background(), mentionActor,
			[]uuid.UUID{disabledGBot, normalUserID, disabledCBot}, circleID)
		if len(got) != 1 || got[0] != normalUserID {
			t.Fatalf("circle=%s: got %v, want only normalUserID", circleID, got)
		}
	}
}

// TestFilterMentionUserIDs_FailOpenOnScopeError GetAgentMentionScopes 失败 → fail-open
// 跳过剔除（仅 ES 列表过滤生效），不报错、不清空名单。
func TestFilterMentionUserIDs_FailOpenOnScopeError(t *testing.T) {
	facade := newMentionFacade()
	facade.circleErr = context.DeadlineExceeded
	svc := &postServiceImpl{userFacade: facade}
	got := svc.filterMentionUserIDs(context.Background(), mentionActor, []uuid.UUID{ownCircleBot, otherCirclBot}, postCircleID)
	if len(got) != 2 {
		t.Fatalf("fail-open: got %v, want both kept", got)
	}
}

// TestFilterMentionUserIDs_BasicRules 既有语义回归：去自己、去 Nil、去重。
func TestFilterMentionUserIDs_BasicRules(t *testing.T) {
	svc := &postServiceImpl{userFacade: newMentionFacade()}
	got := svc.filterMentionUserIDs(context.Background(), mentionActor, []uuid.UUID{
		mentionActor, uuid.Nil, normalUserID, normalUserID,
	}, postCircleID)
	if len(got) != 1 || got[0] != normalUserID {
		t.Fatalf("got %v, want [normalUser]", got)
	}
}
