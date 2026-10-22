package application

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newTestHub 构造 sweeper 不打拍（coalesce 1h）的 hub，测试手动调 sweep() 保证确定性。
func newTestHub(t *testing.T, maxConns int, reader CountReader) *streamHub {
	t.Helper()
	h := NewStreamHub(maxConns, 60*60*1000).(*streamHub)
	h.SetCountReader(reader)
	t.Cleanup(h.Stop)
	return h
}

func staticReader(count *atomic.Int64) CountReader {
	return func(context.Context, uuid.UUID) (int64, error) {
		return count.Load(), nil
	}
}

// recvEvent 读一条事件，timeout 内未到返回 false。
func recvEvent(ch <-chan StreamEvent, timeout time.Duration) (StreamEvent, bool) {
	select {
	case ev := <-ch:
		return ev, true
	case <-time.After(timeout):
		return StreamEvent{}, false
	}
}

// isClosed 判断 Evicted 是否已关闭（不阻塞）。
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestStreamHubAddEvictsOldest(t *testing.T) {
	var count atomic.Int64
	h := newTestHub(t, 2, staticReader(&count))
	uid := uuid.New()

	conn1, ok := h.Add(uid)
	if !ok {
		t.Fatal("first Add should succeed")
	}
	conn2, ok := h.Add(uid)
	if !ok {
		t.Fatal("second Add should succeed")
	}

	// 超限（max 2）：新连接照常建立，最旧的 conn1 被踢，conn2 不受影响。
	conn3, ok := h.Add(uid)
	if !ok {
		t.Fatal("third Add should succeed by evicting the oldest")
	}
	if !isClosed(conn1.Evicted) {
		t.Fatal("oldest connection should be evicted")
	}
	if isClosed(conn2.Evicted) || isClosed(conn3.Evicted) {
		t.Fatal("only the oldest connection should be evicted")
	}

	// 被踢连接的 handler 退出时 Remove 是 no-op，不误删其他连接。
	h.Remove(uid, conn1.ID)
	if got := len(h.users[uid].conns); got != 2 {
		t.Fatalf("conns after removing evicted = %d, want 2", got)
	}

	// 推送只投递给在册连接，不再投递给被踢连接。
	count.Store(4)
	h.Publish(uid)
	h.sweep()
	for name, c := range map[string]StreamConn{"conn2": conn2, "conn3": conn3} {
		if ev, ok := recvEvent(c.Events, time.Second); !ok || ev.Count != 4 {
			t.Fatalf("%s should receive count 4, got %+v ok=%v", name, ev, ok)
		}
	}
	if _, ok := recvEvent(conn1.Events, 50*time.Millisecond); ok {
		t.Fatal("evicted connection should not receive events")
	}

	// Remove 正常释放名额：再加一条不踢人。
	h.Remove(uid, conn2.ID)
	if _, ok := h.Add(uid); !ok {
		t.Fatal("Add after Remove should succeed")
	}
	if isClosed(conn3.Evicted) {
		t.Fatal("Add after Remove should not evict anyone")
	}
	// 其他用户不受影响。
	other, ok := h.Add(uuid.New())
	if !ok || isClosed(other.Evicted) {
		t.Fatal("Add for another user should succeed")
	}
}

func TestStreamHubCoalesceAndDedup(t *testing.T) {
	var count atomic.Int64
	count.Store(1)
	h := newTestHub(t, 5, staticReader(&count))
	uid := uuid.New()

	conn, ok := h.Add(uid)
	if !ok {
		t.Fatal("Add should succeed")
	}

	// 连接建立首事件（Snapshot）记为已推送。
	first, err := h.Snapshot(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if first.Count != 1 {
		t.Fatalf("snapshot count = %d, want 1", first.Count)
	}

	// 合并窗口：多次 Publish + 多次变化，一次 sweep 只推最终值。
	count.Store(3)
	h.Publish(uid)
	count.Store(5)
	h.Publish(uid)
	h.sweep()

	ev, ok := recvEvent(conn.Events, time.Second)
	if !ok {
		t.Fatal("expected one event after sweep")
	}
	if ev.Count != 5 {
		t.Fatalf("event count = %d, want 5", ev.Count)
	}
	if ev.ID == "" || ev.ID == first.ID {
		t.Fatalf("event id should be set and differ from first: %q", ev.ID)
	}
	// 无第二条（窗口合并）。
	if _, ok := recvEvent(conn.Events, 50*time.Millisecond); ok {
		t.Fatal("coalesce window should merge into one event")
	}

	// lastSent 去重：值未变，Publish 后 sweep 不推。
	h.Publish(uid)
	h.sweep()
	if _, ok := recvEvent(conn.Events, 50*time.Millisecond); ok {
		t.Fatal("unchanged count should not be pushed (lastSent dedup)")
	}

	// 值再变 → 推新值，seq 单调递增。
	count.Store(2)
	h.Publish(uid)
	h.sweep()
	ev2, ok := recvEvent(conn.Events, time.Second)
	if !ok {
		t.Fatal("expected event after count change")
	}
	if ev2.Count != 2 {
		t.Fatalf("event count = %d, want 2", ev2.Count)
	}
	if ev2.ID == ev.ID {
		t.Fatal("event id should increase monotonically")
	}
}

func TestStreamHubPublishWithoutConn(t *testing.T) {
	var count atomic.Int64
	count.Store(7)
	h := newTestHub(t, 5, staticReader(&count))
	uid := uuid.New()

	// 无连接时 Publish 不积脏（连接建立走 Snapshot 拿全量）。
	h.Publish(uid)
	h.sweep()

	conn, _ := h.Add(uid)
	first, err := h.Snapshot(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if first.Count != 7 {
		t.Fatalf("snapshot count = %d, want 7", first.Count)
	}
	// sweep 不应再向新连接补推同值。
	h.sweep()
	if _, ok := recvEvent(conn.Events, 50*time.Millisecond); ok {
		t.Fatal("no event expected without new change")
	}
}

func TestStreamHubStopIdempotent(t *testing.T) {
	var count atomic.Int64
	h := NewStreamHub(0, 0) // 默认值兜底路径
	h.SetCountReader(staticReader(&count))
	h.Stop()
	h.Stop() // 二次调用不 panic
	// Stop 后 Add 拒绝。
	if _, ok := h.Add(uuid.New()); ok {
		t.Fatal("Add after Stop should be rejected")
	}
}
