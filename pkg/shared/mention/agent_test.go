package mention

import (
	"context"
	"errors"
	"slices"
	"testing"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	m.Run()
}

var (
	postCircle  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	otherCircle = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb")

	normalUser     = uuid.MustParse("0192a0d0-0000-7000-8000-000000000010")
	globalBot      = uuid.MustParse("0192a0d0-0000-7000-8000-000000000011")
	ownCircleBot   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000012")
	otherCircleBot = uuid.MustParse("0192a0d0-0000-7000-8000-000000000013")
	disabledGlobal = uuid.MustParse("0192a0d0-0000-7000-8000-000000000014")
	disabledOwn    = uuid.MustParse("0192a0d0-0000-7000-8000-000000000015")
)

// fakeLookup 预设机器人可见范围；普通用户与可 @ 的全局机器人不出现在 map（与 user 域约定一致）。
type fakeLookup struct {
	scopes map[uuid.UUID]AgentScope
	err    error
	calls  int
}

func (f *fakeLookup) GetAgentMentionScopes(_ context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentScope, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]AgentScope, len(userIDs))
	for _, id := range userIDs {
		if s, ok := f.scopes[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func newLookup() *fakeLookup {
	return &fakeLookup{scopes: map[uuid.UUID]AgentScope{
		ownCircleBot:   {CircleID: postCircle, Mentionable: true},
		otherCircleBot: {CircleID: otherCircle, Mentionable: true},
		disabledGlobal: {Mentionable: false},
		disabledOwn:    {CircleID: postCircle, Mentionable: false},
	}}
}

func TestAgentScope_MentionableIn(t *testing.T) {
	cases := []struct {
		name  string
		scope AgentScope
		want  bool
	}{
		{"全局机器人", AgentScope{Mentionable: true}, true},
		{"本圈机器人", AgentScope{CircleID: postCircle, Mentionable: true}, true},
		{"他圈机器人", AgentScope{CircleID: otherCircle, Mentionable: true}, false},
		{"停用的全局机器人", AgentScope{Mentionable: false}, false},
		{"停用的本圈机器人", AgentScope{CircleID: postCircle, Mentionable: false}, false},
	}
	for _, tc := range cases {
		if got := tc.scope.MentionableIn(postCircle); got != tc.want {
			t.Errorf("%s: MentionableIn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// 越圈 / 停用的机器人剔除，其余保留且顺序不变。
func TestStripUnmentionableAgents(t *testing.T) {
	candidates := []uuid.UUID{otherCircleBot, normalUser, disabledGlobal, ownCircleBot, disabledOwn, globalBot}
	got := StripUnmentionableAgents(context.Background(), newLookup(), candidates, postCircle)
	want := []uuid.UUID{normalUser, ownCircleBot, globalBot}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// 帖子不在任何圈子（草稿等，postCircleID=Nil）：圈内机器人一律剔除，全局机器人保留。
func TestStripUnmentionableAgents_NoPostCircle(t *testing.T) {
	got := StripUnmentionableAgents(context.Background(), newLookup(),
		[]uuid.UUID{ownCircleBot, globalBot, normalUser}, uuid.Nil)
	if want := []uuid.UUID{globalBot, normalUser}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// 查询失败 fail-open：原样返回候选。
func TestStripUnmentionableAgents_FailOpen(t *testing.T) {
	lookup := newLookup()
	lookup.err = errors.New("user service down")
	candidates := []uuid.UUID{otherCircleBot, normalUser}
	if got := StripUnmentionableAgents(context.Background(), lookup, candidates, postCircle); !slices.Equal(got, candidates) {
		t.Fatalf("fail-open: got %v, want %v", got, candidates)
	}
}

// 空候选不查询。
func TestStripUnmentionableAgents_EmptySkipsLookup(t *testing.T) {
	lookup := newLookup()
	if got := StripUnmentionableAgents(context.Background(), lookup, nil, postCircle); len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
	if lookup.calls != 0 {
		t.Fatalf("lookup called %d times for empty candidates", lookup.calls)
	}
}
