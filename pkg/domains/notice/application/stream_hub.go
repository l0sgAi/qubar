// stream_hub.go 未读通知数 SSE 推送中枢（设计 docs/design/sse-notification-design.md §三）。
//
// 纯 Go 实现，不依赖 Web 框架：SSE 传输（hijack writer / 心跳）在 composition 层，
// 本文件只维护 user_id→连接注册表 + 合并窗口去重推送。
//
// 时序：触发源（consumer flush / MarkRead / MarkAllRead）在计数变更之后调 Publish 标脏；
// sweeper 每 coalesce 窗口扫脏用户，经 CountReader 读全量（缓存优先），与 lastSent 比对，
// 变了才投递到各连接 channel（窗口内多次变化只推最终值）。
//
// 单实例进程内实现；注册表 user_id→conns 结构为多实例 Redis pub/sub（P1）预留。
package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// StreamEvent 推送给单个 SSE 连接的事件（未读数全量）。
type StreamEvent struct {
	ID    string // {unixMilli}-{perUserSeq}，per 用户单调递增
	Count int64  // 未读数全量（与 GET /notice/unread-count 同源）
}

// StreamConn 一条已注册的推送连接。
type StreamConn struct {
	ID      uint64
	Events  <-chan StreamEvent // 未读数事件
	Evicted <-chan struct{}    // 超限时被同用户新连接挤掉（踢最旧）后关闭，handler 收到即通知客户端并断连
}

// CountReader 读取用户当前未读数全量。注入值 = NoticeService.GetUnreadCount（缓存优先，
// miss 回源 DB 回填），推送值与轮询接口语义完全一致。
type CountReader func(ctx context.Context, userID uuid.UUID) (int64, error)

// StreamHub 未读数推送中枢。
type StreamHub interface {
	// SetCountReader 注入未读数读取器（composition 装配时调用）。
	SetCountReader(f CountReader)
	// Add 注册一条连接。达到 per-user 上限时踢掉该用户最旧的连接（关闭其 Evicted）腾出名额，
	// 新连接总能建立；仅 hub 已停止时返回 ok=false。
	Add(userID uuid.UUID) (conn StreamConn, ok bool)
	// Remove 注销连接（handler 退出时调用；已被踢的连接为 no-op）。不 close 事件 channel：
	// sweeper 投递快照 channel 列表后在锁外发送，close 会与投递产生 send-on-closed 竞态；
	// channel 无引用后由 GC 回收。
	Remove(userID uuid.UUID, connID uint64)
	// Publish 标记用户未读数脏，sweeper 在合并窗口内统一推送（不立即发）。
	Publish(userID uuid.UUID)
	// PublishBatch 批量标脏（Redpanda consumer flush 后回调，keys of unreadDeltas）。
	PublishBatch(userIDs []uuid.UUID)
	// Snapshot 读当前全量并记为已推送，返回带 id 的首事件（连接建立即推全量用）。
	Snapshot(ctx context.Context, userID uuid.UUID) (StreamEvent, error)
	// Stop 停 sweeper（server 关停时调用，幂等）。
	// 存量连接由 hertz 关停流程随连接关闭回收，此处不逐个断连。
	Stop()
}

// connChanCap 单连接事件缓冲。推送值是全量（新值取代旧值），满时丢最旧即可。
const connChanCap = 4

// hubConn hub 内单连接状态。
type hubConn struct {
	ch      chan StreamEvent
	evicted chan struct{} // 仅在锁内、从 conns 摘除时关闭一次（无人向其发送，close 无竞态）
}

// userEntry 单用户推送状态。
type userEntry struct {
	conns    map[uint64]*hubConn // connID 全局单调递增：最小者即最旧连接
	dirty    bool                // 未读数已变更、待 sweeper 推送
	lastSent int64               // 最近一次推送值（去重：未变不推）
	hasSent  bool                // 是否已推过（区分 lastSent 零值与真实 0）
	seq      uint64              // 事件序号（事件 id 第二段，per 用户单调递增）
}

type streamHub struct {
	mu          sync.Mutex
	users       map[uuid.UUID]*userEntry
	maxConns    int
	connSeq     atomic.Uint64
	countReader CountReader
	ticker      *time.Ticker
	stopChan    chan struct{}
	done        chan struct{} // run() 退出后关闭
	stopped     bool
}

// NewStreamHub 构造并启动 sweeper。maxConnsPerUser<=0 兜底 5，coalesceMs<=0 兜底 1000。
func NewStreamHub(maxConnsPerUser, coalesceMs int) StreamHub {
	if maxConnsPerUser <= 0 {
		maxConnsPerUser = 5
	}
	if coalesceMs <= 0 {
		coalesceMs = 1000
	}
	h := &streamHub{
		users:    make(map[uuid.UUID]*userEntry),
		maxConns: maxConnsPerUser,
		ticker:   time.NewTicker(time.Duration(coalesceMs) * time.Millisecond),
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *streamHub) SetCountReader(f CountReader) {
	h.mu.Lock()
	h.countReader = f
	h.mu.Unlock()
}

func (h *streamHub) Add(userID uuid.UUID) (StreamConn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return StreamConn{}, false
	}
	e := h.users[userID]
	if e == nil {
		e = &userEntry{conns: make(map[uint64]*hubConn)}
		h.users[userID] = e
	}
	// 超限踢最旧（#127）：最旧连接最可能是已断开但未被感知的僵尸（半开 TCP / 代理未透传断开），
	// 拒新会让浏览器 EventSource 遇非 200 永久放弃重连。
	for len(e.conns) >= h.maxConns {
		h.evictOldestLocked(e)
	}
	connID := h.connSeq.Add(1)
	c := &hubConn{
		ch:      make(chan StreamEvent, connChanCap),
		evicted: make(chan struct{}),
	}
	e.conns[connID] = c
	return StreamConn{ID: connID, Events: c.ch, Evicted: c.evicted}, true
}

// evictOldestLocked 摘除并通知该用户 connID 最小（最旧）的连接。调用方持锁且 conns 非空。
func (h *streamHub) evictOldestLocked(e *userEntry) {
	var oldest uint64
	for id := range e.conns {
		if oldest == 0 || id < oldest {
			oldest = id
		}
	}
	close(e.conns[oldest].evicted)
	delete(e.conns, oldest)
}

func (h *streamHub) Remove(userID uuid.UUID, connID uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.users[userID]
	if e == nil {
		return
	}
	delete(e.conns, connID)
	// 无连接的空 entry 回收（dirty 一并丢弃：无连接无可推，下次连接建立走 Snapshot 拿全量）。
	if len(e.conns) == 0 {
		delete(h.users, userID)
	}
}

func (h *streamHub) Publish(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	// 无连接不标脏：连接建立时 Snapshot 会推当前全量，无积压必要。
	if e := h.users[userID]; e != nil && len(e.conns) > 0 {
		e.dirty = true
	}
}

func (h *streamHub) PublishBatch(userIDs []uuid.UUID) {
	for _, id := range userIDs {
		h.Publish(id)
	}
}

func (h *streamHub) Snapshot(ctx context.Context, userID uuid.UUID) (StreamEvent, error) {
	h.mu.Lock()
	reader := h.countReader
	h.mu.Unlock()
	if reader == nil {
		return StreamEvent{}, errors.New("notice stream hub: count reader not set")
	}
	count, err := reader(ctx, userID)
	if err != nil {
		return StreamEvent{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.users[userID]
	if e == nil {
		e = &userEntry{conns: make(map[uint64]*hubConn)}
		h.users[userID] = e
	}
	e.seq++
	e.lastSent = count
	e.hasSent = true
	return StreamEvent{ID: streamEventID(e.seq), Count: count}, nil
}

func (h *streamHub) Stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	h.mu.Unlock()
	close(h.stopChan)
	<-h.done
}

func (h *streamHub) run() {
	defer close(h.done)
	for {
		select {
		case <-h.ticker.C:
			h.sweep()
		case <-h.stopChan:
			h.ticker.Stop()
			return
		}
	}
}

// sweep 扫一遍脏用户：读全量 → 与 lastSent 比对 → 变了才投递。
// CountReader 可能阻塞（Redis/DB），在锁外调用。
func (h *streamHub) sweep() {
	h.mu.Lock()
	dirty := make([]uuid.UUID, 0, len(h.users))
	for uid, e := range h.users {
		if e.dirty && len(e.conns) > 0 {
			dirty = append(dirty, uid)
		}
		e.dirty = false
	}
	reader := h.countReader
	h.mu.Unlock()

	for _, uid := range dirty {
		if reader == nil {
			return
		}
		count, err := reader(context.Background(), uid)
		if err != nil {
			logger.Log.Error("Failed to read unread count for stream push: " + err.Error())
			// 重新标脏，下一拍重试。
			h.mu.Lock()
			if e := h.users[uid]; e != nil {
				e.dirty = true
			}
			h.mu.Unlock()
			continue
		}
		h.deliver(uid, count)
	}
}

// deliver 去重后投递到该用户所有连接。
func (h *streamHub) deliver(uid uuid.UUID, count int64) {
	h.mu.Lock()
	e := h.users[uid]
	if e == nil || len(e.conns) == 0 || (e.hasSent && e.lastSent == count) {
		h.mu.Unlock()
		return
	}
	e.seq++
	e.lastSent = count
	e.hasSent = true
	ev := StreamEvent{ID: streamEventID(e.seq), Count: count}
	conns := make([]chan StreamEvent, 0, len(e.conns))
	for _, c := range e.conns {
		conns = append(conns, c.ch)
	}
	h.mu.Unlock()

	for _, ch := range conns {
		sendLatest(ch, ev)
	}
}

// sendLatest 非阻塞投递；缓冲满则丢最旧（推送值是全量，旧值已被取代）。
func sendLatest(ch chan StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- ev:
		default:
		}
	}
}

// streamEventID 事件 id：{unixMilli}-{perUserSeq}，per 用户单调递增。
func streamEventID(seq uint64) string {
	return fmt.Sprintf("%d-%d", time.Now().UnixMilli(), seq)
}
