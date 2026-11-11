package application

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TestMain hub 在读数失败 / 即时事件缓冲满时写日志，单测里换成 Nop logger。
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

// newTwoChannelHub 构造同时注入消息中心与私信两个通道读取器的 hub（sweeper 不打拍，测试手动 sweep）。
func newTwoChannelHub(t *testing.T, notice, dm *atomic.Int64) *streamHub {
	t.Helper()
	h := newTestHub(t, 5, staticReader(notice))
	h.SetChannelCountReader(ChannelDMUnread, staticReader(dm))
	return h
}

func recvPush(ch <-chan PushEvent, timeout time.Duration) (PushEvent, bool) {
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(timeout):
		return PushEvent{}, false
	}
}

// 首包：两个通道各一条全量，按通道顺序，id 单调递增。
func TestStreamHubSnapshotAllChannels(t *testing.T) {
	var notice, dm atomic.Int64
	notice.Store(3)
	dm.Store(2)
	h := newTwoChannelHub(t, &notice, &dm)
	uid := uuid.New()
	if _, ok := h.Add(uid); !ok {
		t.Fatal("Add should succeed")
	}

	events, err := h.Snapshot(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("snapshot events = %d, want 2", len(events))
	}
	if events[0].Channel != ChannelNoticeUnread || events[0].Count != 3 {
		t.Fatalf("first event = %+v, want notice 3", events[0])
	}
	if events[1].Channel != ChannelDMUnread || events[1].Count != 2 {
		t.Fatalf("second event = %+v, want dm 2", events[1])
	}
	if events[0].ID == events[1].ID {
		t.Fatal("snapshot event ids should differ")
	}
}

// 两个计数通道互不覆盖：各自合并、各自 lastSent 去重，投递到各自的 channel。
func TestStreamHubChannelsIndependent(t *testing.T) {
	var notice, dm atomic.Int64
	notice.Store(1)
	dm.Store(0)
	h := newTwoChannelHub(t, &notice, &dm)
	uid := uuid.New()
	conn, _ := h.Add(uid)
	if _, err := h.Snapshot(context.Background(), uid); err != nil {
		t.Fatal(err)
	}

	// 只有私信变化：只推 dm-unread，不推 unread-count。
	dm.Store(1)
	h.PublishChannel(ChannelDMUnread, uid)
	dm.Store(2)
	h.PublishChannel(ChannelDMUnread, uid)
	h.sweep()
	ev, ok := recvEvent(conn.DMEvents, time.Second)
	if !ok || ev.Count != 2 || ev.Channel != ChannelDMUnread {
		t.Fatalf("dm event = %+v ok=%v, want merged dm 2", ev, ok)
	}
	if _, ok := recvEvent(conn.DMEvents, 50*time.Millisecond); ok {
		t.Fatal("dm channel should merge into one event")
	}
	if _, ok := recvEvent(conn.Events, 50*time.Millisecond); ok {
		t.Fatal("notice channel should not be pushed when only dm changed")
	}

	// 两个通道在同一窗口都变化：各推各的值。
	notice.Store(9)
	dm.Store(0)
	h.Publish(uid)
	h.PublishChannel(ChannelDMUnread, uid)
	h.sweep()
	if ev, ok := recvEvent(conn.Events, time.Second); !ok || ev.Count != 9 {
		t.Fatalf("notice event = %+v ok=%v, want 9", ev, ok)
	}
	if ev, ok := recvEvent(conn.DMEvents, time.Second); !ok || ev.Count != 0 {
		t.Fatalf("dm event = %+v ok=%v, want 0", ev, ok)
	}

	// 值相同（与 notice 的 lastSent 无关）：私信通道去重只看自己的 lastSent。
	dm.Store(0)
	h.PublishChannel(ChannelDMUnread, uid)
	h.sweep()
	if _, ok := recvEvent(conn.DMEvents, 50*time.Millisecond); ok {
		t.Fatal("unchanged dm count should not be pushed")
	}

	// 越界通道被忽略，不 panic。
	h.PublishChannel(NumStreamChannels, uid)
	h.PublishChannel(-1, uid)
	h.SetChannelCountReader(NumStreamChannels, staticReader(&dm))
}

// 即时事件：立即投递、不合并、不去重、不占计数通道；无连接时丢弃。
func TestStreamHubPush(t *testing.T) {
	var notice, dm atomic.Int64
	h := newTwoChannelHub(t, &notice, &dm)
	uid := uuid.New()

	// 无连接：丢弃，不积压。
	h.Push(uid, "dm-message", []byte(`{"n":0}`))

	conn1, _ := h.Add(uid)
	conn2, _ := h.Add(uid)
	h.Push(uid, "dm-message", []byte(`{"n":1}`))
	h.Push(uid, "dm-message", []byte(`{"n":1}`)) // 相同负载也照推（不去重）
	for name, c := range map[string]StreamConn{"conn1": conn1, "conn2": conn2} {
		first, ok := recvPush(c.Pushes, time.Second)
		if !ok || first.Type != "dm-message" || string(first.Data) != `{"n":1}` || first.ID == "" {
			t.Fatalf("%s first push = %+v ok=%v", name, first, ok)
		}
		second, ok := recvPush(c.Pushes, time.Second)
		if !ok || second.ID == first.ID {
			t.Fatalf("%s second push = %+v ok=%v, want distinct id", name, second, ok)
		}
	}
	if _, ok := recvEvent(conn1.DMEvents, 50*time.Millisecond); ok {
		t.Fatal("push should not produce count events")
	}

	// 缓冲满：丢新事件，不阻塞、不 panic，已入队的保持顺序。
	for i := 0; i < pushChanCap+5; i++ {
		h.Push(uid, "dm-message", []byte{byte('a' + i%26)})
	}
	got := 0
	for {
		if _, ok := recvPush(conn1.Pushes, 20*time.Millisecond); !ok {
			break
		}
		got++
	}
	if got != pushChanCap {
		t.Fatalf("buffered pushes = %d, want %d", got, pushChanCap)
	}

	// Stop 后 Push 是 no-op：conn1 已排空，不应再收到事件。
	h.Stop()
	h.Push(uid, "dm-message", nil)
	if _, ok := recvPush(conn1.Pushes, 20*time.Millisecond); ok {
		t.Fatal("push after Stop should be dropped")
	}
}

// 首包某个通道读失败：其余通道照常下发，失败通道标脏、下一拍补推；全部失败才返回错误。
func TestStreamHubSnapshotPartialFailure(t *testing.T) {
	var notice atomic.Int64
	notice.Store(4)
	var dmFail atomic.Bool
	dmFail.Store(true)
	h := newTestHub(t, 5, staticReader(&notice))
	h.SetChannelCountReader(ChannelDMUnread, func(context.Context, uuid.UUID) (int64, error) {
		if dmFail.Load() {
			return 0, errors.New("redis down")
		}
		return 6, nil
	})
	uid := uuid.New()
	conn, _ := h.Add(uid)

	events, err := h.Snapshot(context.Background(), uid)
	if err != nil {
		t.Fatalf("partial failure should not fail snapshot: %v", err)
	}
	if len(events) != 1 || events[0].Channel != ChannelNoticeUnread {
		t.Fatalf("snapshot events = %+v, want only notice", events)
	}

	dmFail.Store(false)
	h.sweep()
	if ev, ok := recvEvent(conn.DMEvents, time.Second); !ok || ev.Count != 6 {
		t.Fatalf("dm retry event = %+v ok=%v, want 6", ev, ok)
	}

	// 全部失败：返回错误。
	h.SetCountReader(func(context.Context, uuid.UUID) (int64, error) { return 0, errors.New("db down") })
	dmFail.Store(true)
	if _, err := h.Snapshot(context.Background(), uid); err == nil {
		t.Fatal("snapshot should fail when every channel fails")
	}
}
