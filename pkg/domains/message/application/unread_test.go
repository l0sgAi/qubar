package application

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/logger"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TestMain 未读缓存 / 推送失败时写日志，单测里换成 Nop logger。
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

// SumUnread 让 fakeRepo 满足 domain.Repository（其他用例不关心未读总数）。
func (f *fakeRepo) SumUnread(context.Context, uuid.UUID) (int64, error) { return 0, nil }

// unreadRepo 在 fakeRepo 基础上预置未读总数。
type unreadRepo struct {
	fakeRepo
	sum      int64
	sumErr   error
	sumCalls int
}

func (r *unreadRepo) SumUnread(context.Context, uuid.UUID) (int64, error) {
	r.sumCalls++
	return r.sum, r.sumErr
}

// fakeCache 内存版 UnreadCache，记录调用。
type fakeCache struct {
	vals        map[uuid.UUID]int64
	getErr      error
	incrErr     error
	sets        int
	incrs       []uuid.UUID
	invalidated []uuid.UUID
}

func newFakeCache() *fakeCache { return &fakeCache{vals: map[uuid.UUID]int64{}} }

func (c *fakeCache) Get(_ context.Context, id uuid.UUID) (int64, bool, error) {
	if c.getErr != nil {
		return 0, false, c.getErr
	}
	v, ok := c.vals[id]
	return v, ok, nil
}

func (c *fakeCache) Set(_ context.Context, id uuid.UUID, v int64) error {
	c.sets++
	c.vals[id] = v
	return nil
}

func (c *fakeCache) IncrIfExists(_ context.Context, id uuid.UUID, d int64) error {
	c.incrs = append(c.incrs, id)
	if c.incrErr != nil {
		return c.incrErr
	}
	if v, ok := c.vals[id]; ok {
		c.vals[id] = v + d
	}
	return nil
}

func (c *fakeCache) Invalidate(_ context.Context, id uuid.UUID) error {
	c.invalidated = append(c.invalidated, id)
	delete(c.vals, id)
	return nil
}

// fakeNotifier 记录推送。
type fakeNotifier struct {
	changed []uuid.UUID
	msgTo   []uuid.UUID
	msgs    []NewMessageEvent
}

func (n *fakeNotifier) UnreadChanged(id uuid.UUID) { n.changed = append(n.changed, id) }

func (n *fakeNotifier) NewMessage(to uuid.UUID, ev NewMessageEvent) {
	n.msgTo = append(n.msgTo, to)
	n.msgs = append(n.msgs, ev)
}

func TestGetUnreadTotal_CacheFirstThenDB(t *testing.T) {
	me := sharedomain.NewID()
	repo := &unreadRepo{sum: 5}
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})

	// 未注入缓存：直接查 DB。
	if got, err := svc.GetUnreadTotal(t.Context(), me); err != nil || got != 5 || repo.sumCalls != 1 {
		t.Fatalf("no cache: got %d err %v calls %d, want 5 from DB", got, err, repo.sumCalls)
	}

	cache := newFakeCache()
	svc.SetUnreadCache(cache)
	// miss → 回源 + 回填。
	if got, err := svc.GetUnreadTotal(t.Context(), me); err != nil || got != 5 || repo.sumCalls != 2 || cache.vals[me] != 5 {
		t.Fatalf("miss: got %d err %v calls %d cache %v", got, err, repo.sumCalls, cache.vals)
	}
	// hit → 不查 DB。
	repo.sum = 99
	if got, err := svc.GetUnreadTotal(t.Context(), me); err != nil || got != 5 || repo.sumCalls != 2 {
		t.Fatalf("hit: got %d err %v calls %d, want cached 5", got, err, repo.sumCalls)
	}
	// 缓存出错 → 降级查 DB。
	cache.getErr = errors.New("redis down")
	if got, err := svc.GetUnreadTotal(t.Context(), me); err != nil || got != 99 {
		t.Fatalf("cache error: got %d err %v, want 99 from DB", got, err)
	}
	// DB 出错 → 返回错误。
	cache.getErr = nil
	delete(cache.vals, me)
	repo.sumErr = errors.New("db down")
	if _, err := svc.GetUnreadTotal(t.Context(), me); err == nil {
		t.Fatal("db error should be returned")
	}
}

func TestSend_IncrsReceiverUnreadAndPushes(t *testing.T) {
	me, peer := sharedomain.NewID(), sharedomain.NewID()
	repo := &unreadRepo{}
	users := &fakeUsers{briefs: map[uuid.UUID]UserBrief{me: {ID: me.String(), Username: "alice"}}}
	svc := NewMessageService(repo, fakeRecipients{}, users, fakeFollows{})

	// 未注入缓存 / 推送端口：发送照常成功。
	if _, err := svc.Send(t.Context(), me, peer, "hi"); err != nil {
		t.Fatal(err)
	}

	cache, n := newFakeCache(), &fakeNotifier{}
	cache.vals[peer] = 2
	svc.SetUnreadCache(cache)
	svc.SetUnreadNotifier(n)

	long := strings.Repeat("字", PushPreviewChars+5)
	vo, err := svc.Send(t.Context(), me, peer, long)
	if err != nil {
		t.Fatal(err)
	}
	if cache.vals[peer] != 3 || len(cache.incrs) != 1 || cache.incrs[0] != peer {
		t.Fatalf("receiver cache = %v incrs %v, want 3 for peer only", cache.vals, cache.incrs)
	}
	if len(n.changed) != 1 || n.changed[0] != peer {
		t.Fatalf("unread changed = %v, want [peer]", n.changed)
	}
	if len(n.msgTo) != 1 || n.msgTo[0] != peer {
		t.Fatalf("new message pushed to %v, want only receiver", n.msgTo)
	}
	ev := n.msgs[0]
	if ev.ID != vo.ID || ev.ConversationID != vo.ConversationID || ev.SenderID != me.String() ||
		ev.Sender == nil || ev.Sender.Username != "alice" || ev.MsgType != domain.MessageTypeText {
		t.Fatalf("event = %+v (sender %+v)", ev, ev.Sender)
	}
	if !ev.Truncated || ev.Content != strings.Repeat("字", PushPreviewChars) {
		t.Fatalf("event content len %d truncated %v, want %d chars truncated", len([]rune(ev.Content)), ev.Truncated, PushPreviewChars)
	}

	// 短正文不截断；发送人信息读不到时 sender 为 nil。
	users.briefs = nil
	if _, err := svc.Send(t.Context(), me, peer, "短"); err != nil {
		t.Fatal(err)
	}
	if ev := n.msgs[1]; ev.Truncated || ev.Content != "短" || ev.Sender != nil {
		t.Fatalf("short event = %+v", ev)
	}

	// 缓存自增失败：删缓存兜底，发送仍成功、仍推送。
	cache.incrErr = errors.New("redis down")
	if _, err := svc.Send(t.Context(), me, peer, "x"); err != nil {
		t.Fatalf("send should succeed when cache fails: %v", err)
	}
	if _, ok := cache.vals[peer]; ok || len(n.changed) != 3 {
		t.Fatalf("cache should be invalidated after incr failure (vals %v), changed %d", cache.vals, len(n.changed))
	}

	// 发送失败：不动缓存、不推送。
	repo.sendErr = errors.New("tx failed")
	if _, err := svc.Send(t.Context(), me, peer, "y"); err == nil {
		t.Fatal("send error should be returned")
	}
	if len(n.changed) != 3 || len(n.msgs) != 3 {
		t.Fatal("failed send should not push")
	}
}

func TestMarkReadAndDelete_InvalidateAndPush(t *testing.T) {
	me, conv := sharedomain.NewID(), sharedomain.NewID()
	for name, call := range map[string]func(MessageService) error{
		"read":   func(s MessageService) error { return s.MarkRead(t.Context(), me, conv) },
		"delete": func(s MessageService) error { return s.DeleteConversation(t.Context(), me, conv) },
	} {
		t.Run(name, func(t *testing.T) {
			repo := &unreadRepo{}
			svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})
			cache, n := newFakeCache(), &fakeNotifier{}
			cache.vals[me] = 4
			svc.SetUnreadCache(cache)
			svc.SetUnreadNotifier(n)

			if err := call(svc); err != nil {
				t.Fatal(err)
			}
			if _, ok := cache.vals[me]; ok || len(cache.invalidated) != 1 {
				t.Fatalf("cache should be invalidated: %v", cache.vals)
			}
			if len(n.changed) != 1 || n.changed[0] != me || len(n.msgs) != 0 {
				t.Fatalf("changed %v msgs %d, want [me] and no message push", n.changed, len(n.msgs))
			}

			// 不是成员：不动缓存、不推送。
			repo.markReadErr, repo.hideErr = domain.ErrConversationNotFound, domain.ErrConversationNotFound
			if err := call(svc); !errors.Is(err, domain.ErrConversationNotFound) {
				t.Fatalf("err = %v", err)
			}
			if len(cache.invalidated) != 1 || len(n.changed) != 1 {
				t.Fatal("failed write should not touch cache or push")
			}
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	for _, tc := range []struct {
		in        string
		n         int
		want      string
		truncated bool
	}{
		{"", 3, "", false},
		{"abc", 3, "abc", false},
		{"abcd", 3, "abc", true},
		{"你好世界", 2, "你好", true},
		{"😀😀", 1, "😀", true},
	} {
		got, tr := truncateRunes(tc.in, tc.n)
		if got != tc.want || tr != tc.truncated {
			t.Errorf("truncateRunes(%q,%d) = %q,%v want %q,%v", tc.in, tc.n, got, tr, tc.want, tc.truncated)
		}
	}
}
