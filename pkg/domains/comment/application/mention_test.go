// mention_test.go 评论 @提及 校验的圈子作用域兜底单元测试。
//
// 覆盖设计文档 P0-8 的"兜底剔除"（comment 侧）：越圈机器人剔除 / 本圈机器人保留 /
// 全局机器人与普通用户保留 / GetAgentCircleIDs 失败 fail-open。与 post 域同构逻辑
// （UserFacade 名义类型不同，各自声明），故独立成套。
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
	commentCircleID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	commentOtherCID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb")
	commentActorID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	cNormalUserID   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000020")
	cNormalUser2ID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000025")
	cNormalUser3ID  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000026")
	cGlobalBotID    = uuid.MustParse("0192a0d0-0000-7000-8000-000000000021")
	cOwnCircleBotID = uuid.MustParse("0192a0d0-0000-7000-8000-000000000022")
	cOtherCircleBot = uuid.MustParse("0192a0d0-0000-7000-8000-000000000023")
	cGhostUserID    = uuid.MustParse("0192a0d0-0000-7000-8000-000000000024")
)

// fakeCommentUserFacade UserFacade fake：预设存在性与机器人圈子绑定。
type fakeCommentUserFacade struct {
	briefs       map[string]UserBrief
	agentCircles map[uuid.UUID]uuid.UUID
	circleErr    error
}

func (f *fakeCommentUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error) {
	out := make(map[string]UserBrief, len(userIDs))
	for _, id := range userIDs {
		if b, ok := f.briefs[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

func (f *fakeCommentUserFacade) GetBrief(ctx context.Context, userID string) (*UserBrief, error) {
	if b, ok := f.briefs[userID]; ok {
		return &b, nil
	}
	return nil, nil
}

func (f *fakeCommentUserFacade) GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	if f.circleErr != nil {
		return nil, f.circleErr
	}
	out := make(map[uuid.UUID]uuid.UUID, len(userIDs))
	for _, id := range userIDs {
		if c, ok := f.agentCircles[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

// newCommentMentionFacade 组装常规 fake：全部候选存在；本圈/他圈机器人绑定如名。
func newCommentMentionFacade() *fakeCommentUserFacade {
	return &fakeCommentUserFacade{
		briefs: map[string]UserBrief{
			cNormalUserID.String():   {ID: cNormalUserID.String(), Username: "普通用户"},
			cNormalUser2ID.String():  {ID: cNormalUser2ID.String(), Username: "普通用户乙"},
			cNormalUser3ID.String():  {ID: cNormalUser3ID.String(), Username: "普通用户丙"},
			cGlobalBotID.String():    {ID: cGlobalBotID.String(), Username: "全局机器人"},
			cOwnCircleBotID.String(): {ID: cOwnCircleBotID.String(), Username: "本圈机器人"},
			cOtherCircleBot.String(): {ID: cOtherCircleBot.String(), Username: "他圈机器人"},
		},
		agentCircles: map[uuid.UUID]uuid.UUID{
			cOwnCircleBotID: commentCircleID,
			cOtherCircleBot: commentOtherCID,
		},
	}
}

// TestFilterMentionUserIDs_AgentScope 评论 @提及：越圈机器人剔除，本圈机器人/全局机器人/
// 普通用户保留，不存在用户被存在性校验过滤。
func TestFilterMentionUserIDs_AgentScope(t *testing.T) {
	svc := &commentServiceImpl{userFacade: newCommentMentionFacade()}
	got := svc.filterMentionUserIDs(context.Background(), commentActorID, []uuid.UUID{
		cNormalUserID, cGlobalBotID, cOwnCircleBotID, cOtherCircleBot, cGhostUserID,
	}, commentCircleID)

	want := map[uuid.UUID]bool{cNormalUserID: true, cGlobalBotID: true, cOwnCircleBotID: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected id %s in result %v", id, got)
		}
	}
}

// TestFilterMentionUserIDs_FailOpenOnScopeError 作用域查询失败 → fail-open 跳过剔除。
func TestFilterMentionUserIDs_FailOpenOnScopeError(t *testing.T) {
	facade := newCommentMentionFacade()
	facade.circleErr = context.DeadlineExceeded
	svc := &commentServiceImpl{userFacade: facade}
	got := svc.filterMentionUserIDs(context.Background(), commentActorID,
		[]uuid.UUID{cOwnCircleBotID, cOtherCircleBot}, commentCircleID)
	if len(got) != 2 {
		t.Fatalf("fail-open: got %v, want both kept", got)
	}
}

// TestStripOutOfScopeAgents_MentionsQuota 剔除发生在截断前：越圈机器人不占用
// MentionMax 配额（先剔除后截断的顺序语义）。
func TestStripOutOfScopeAgents_MentionsQuota(t *testing.T) {
	conf.Config.Notice.MentionMax = 2
	defer func() { conf.Config.Notice.MentionMax = 10 }()

	facade := newCommentMentionFacade()
	svc := &commentServiceImpl{userFacade: facade}
	// 3 个有效候选（1 他圈机器人 + 2 普通用户），上限 2：他圈机器人被剔除后才截断，
	// 最终保留 2 个普通用户（若先截断则会误留他圈机器人）。
	got := svc.filterMentionUserIDs(context.Background(), commentActorID,
		[]uuid.UUID{cOtherCircleBot, cNormalUserID, cNormalUser2ID, cNormalUser3ID},
		commentCircleID)
	for _, id := range got {
		if id == cOtherCircleBot {
			t.Fatalf("out-of-scope bot survived truncation: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 kept within quota", got)
	}
}
