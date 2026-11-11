// stream_hub.go SSE 推送中枢（设计 docs/design/sse-notification-design.md §三、§八）。
//
// 纯 Go 实现，不依赖 Web 框架：SSE 传输（hijack writer / 心跳）在 composition 层，
// 本文件只维护 user_id→连接注册表 + 合并窗口去重推送。
//
// 两类事件：
//   - 计数通道（StreamChannel）：消息中心未读数（unread-count）、私信未读总数（dm-unread，#241）。
//     每个通道独立地「全量值、合并推送、去重」：触发源在计数变更之后调 Publish/PublishChannel 标脏；
//     sweeper 每 coalesce 窗口扫脏，经该通道的 CountReader 读全量（缓存优先），与该通道的 lastSent 比对，
//     变了才投递到各连接该通道的 channel（窗口内多次变化只推最终值）。通道之间互不覆盖。
//   - 即时事件（Push，如新私信摘要 dm-message，#241）：不合并、不去重，立即投递到该用户各连接的独立 channel，
//     不经过计数通道的合并 / 丢旧逻辑。
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

// StreamChannel 计数通道。每个通道各自维护合并窗口与 lastSent 去重，SSE 事件名由 composition 层映射。
type StreamChannel int

const (
	// ChannelNoticeUnread 消息中心未读通知数（SSE 事件 unread-count）。
	ChannelNoticeUnread StreamChannel = iota
	// ChannelDMUnread 私信未读总数（SSE 事件 dm-unread，#241）。
	ChannelDMUnread
	// NumStreamChannels 计数通道数量（数组下标上限）。
	NumStreamChannels
)

// StreamEvent 推送给单个 SSE 连接的计数事件（某个通道的全量值）。
type StreamEvent struct {
	ID      string        // {unixMilli}-{perUserSeq}，per 用户单调递增（所有事件共用序号）
	Channel StreamChannel // 所属计数通道
	Count   int64         // 全量值（与对应轮询接口同源）
}

// PushEvent 即时推送事件（不合并、不去重），如新私信摘要。
type PushEvent struct {
	ID   string // 同 StreamEvent.ID，与计数事件共用 per 用户序号
	Type string // SSE 事件名（由触发方决定，如 dm-message）
	Data []byte // JSON 负载
}

// StreamConn 一条已注册的推送连接。
type StreamConn struct {
	ID       uint64
	Events   <-chan StreamEvent // 消息中心未读数事件（ChannelNoticeUnread）
	DMEvents <-chan StreamEvent // 私信未读总数事件（ChannelDMUnread）
	Pushes   <-chan PushEvent   // 即时事件（新私信摘要等）
	Evicted  <-chan struct{}    // 超限时被同用户新连接挤掉（踢最旧）后关闭，handler 收到即通知客户端并断连
}

// CountReader 读取用户某个计数通道的当前全量。消息中心通道注入 NoticeService.GetUnreadCount，
// 私信通道注入 message 域的未读总数（都是缓存优先、miss 回源 DB 回填），推送值与轮询接口语义一致。
type CountReader func(ctx context.Context, userID uuid.UUID) (int64, error)

// StreamHub 推送中枢。
type StreamHub interface {
	// SetCountReader 注入消息中心未读数读取器（= SetChannelCountReader(ChannelNoticeUnread, f)）。
	SetCountReader(f CountReader)
	// SetChannelCountReader 注入某个计数通道的读取器（composition 装配时调用）。未注入的通道不推送、首包不下发。
	SetChannelCountReader(ch StreamChannel, f CountReader)
	// Add 注册一条连接。达到 per-user 上限时踢掉该用户最旧的连接（关闭其 Evicted）腾出名额，
	// 新连接总能建立；仅 hub 已停止时返回 ok=false。
	Add(userID uuid.UUID) (conn StreamConn, ok bool)
	// Remove 注销连接（handler 退出时调用；已被踢的连接为 no-op）。不 close 事件 channel：
	// sweeper 投递快照 channel 列表后在锁外发送，close 会与投递产生 send-on-closed 竞态；
	// channel 无引用后由 GC 回收。
	Remove(userID uuid.UUID, connID uint64)
	// Publish 标记用户消息中心未读数脏（= PublishChannel(ChannelNoticeUnread, userID)）。
	Publish(userID uuid.UUID)
	// PublishBatch 批量标脏消息中心未读数（Redpanda consumer flush 后回调，keys of unreadDeltas）。
	PublishBatch(userIDs []uuid.UUID)
	// PublishChannel 标记用户某个计数通道脏，sweeper 在合并窗口内统一推送（不立即发）。
	PublishChannel(ch StreamChannel, userID uuid.UUID)
	// Push 立即向用户所有连接投递一条即时事件（不合并、不去重）；用户无连接时丢弃（前端以拉取为准）。
	Push(userID uuid.UUID, eventType string, data []byte)
	// Snapshot 读各已注入通道的当前全量并记为已推送，返回带 id 的首包事件（连接建立即推全量用），
	// 按通道顺序排列。某个通道读取失败时跳过它并标脏（sweeper 下一拍补推）；所有通道都失败才返回错误。
	Snapshot(ctx context.Context, userID uuid.UUID) ([]StreamEvent, error)
	// Stop 停 sweeper（server 关停时调用，幂等）。
	// 存量连接由 hertz 关停流程随连接关闭回收，此处不逐个断连。
	Stop()
}

const (
	// connChanCap 单连接单通道计数事件缓冲。推送值是全量（新值取代旧值），满时丢最旧即可。
	connChanCap = 4
	// pushChanCap 单连接即时事件缓冲。满了说明客户端长时间不读（假死），丢弃新事件并记日志，前端以拉取为准。
	pushChanCap = 32
)

// hubConn hub 内单连接状态。
type hubConn struct {
	counts  [NumStreamChannels]chan StreamEvent
	pushes  chan PushEvent
	evicted chan struct{} // 仅在锁内、从 conns 摘除时关闭一次（无人向其发送，close 无竞态）
}

// channelState 单用户单通道的推送状态。
type channelState struct {
	dirty    bool  // 计数已变更、待 sweeper 推送
	lastSent int64 // 最近一次推送值（去重：未变不推）
	hasSent  bool  // 是否已推过（区分 lastSent 零值与真实 0）
}

// userEntry 单用户推送状态。
type userEntry struct {
	conns    map[uint64]*hubConn // connID 全局单调递增：最小者即最旧连接
	channels [NumStreamChannels]channelState
	seq      uint64 // 事件序号（事件 id 第二段，per 用户单调递增，所有事件共用）
}

type streamHub struct {
	mu       sync.Mutex
	users    map[uuid.UUID]*userEntry
	maxConns int
	connSeq  atomic.Uint64
	readers  [NumStreamChannels]CountReader
	ticker   *time.Ticker
	stopChan chan struct{}
	done     chan struct{} // run() 退出后关闭
	stopped  bool
}

// NewStreamHub 构造并启动 sweeper。maxConnsPerUser<=0 兜底 5，coalesceMs<=0 兜底 1000。
// 合并窗口对所有计数通道相同。
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

// validChannel 通道是否在范围内（越界的调用直接忽略，防止数组越界 panic）。
func validChannel(ch StreamChannel) bool {
	return ch >= 0 && ch < NumStreamChannels
}

func (h *streamHub) SetCountReader(f CountReader) {
	h.SetChannelCountReader(ChannelNoticeUnread, f)
}

func (h *streamHub) SetChannelCountReader(ch StreamChannel, f CountReader) {
	if !validChannel(ch) {
		return
	}
	h.mu.Lock()
	h.readers[ch] = f
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
		pushes:  make(chan PushEvent, pushChanCap),
		evicted: make(chan struct{}),
	}
	for i := range c.counts {
		c.counts[i] = make(chan StreamEvent, connChanCap)
	}
	e.conns[connID] = c
	return StreamConn{
		ID:       connID,
		Events:   c.counts[ChannelNoticeUnread],
		DMEvents: c.counts[ChannelDMUnread],
		Pushes:   c.pushes,
		Evicted:  c.evicted,
	}, true
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
	h.PublishChannel(ChannelNoticeUnread, userID)
}

func (h *streamHub) PublishBatch(userIDs []uuid.UUID) {
	for _, id := range userIDs {
		h.Publish(id)
	}
}

func (h *streamHub) PublishChannel(ch StreamChannel, userID uuid.UUID) {
	if !validChannel(ch) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	// 无连接不标脏：连接建立时 Snapshot 会推当前全量，无积压必要。
	if e := h.users[userID]; e != nil && len(e.conns) > 0 {
		e.channels[ch].dirty = true
	}
}

func (h *streamHub) Push(userID uuid.UUID, eventType string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.users[userID]
	if h.stopped || e == nil || len(e.conns) == 0 {
		return
	}
	e.seq++
	ev := PushEvent{ID: streamEventID(e.seq), Type: eventType, Data: data}
	// 锁内非阻塞投递：同一用户的即时事件按 seq 顺序入队（并发 Push 不会乱序），不会因慢连接阻塞。
	for connID, c := range e.conns {
		select {
		case c.pushes <- ev:
		default:
			logger.Log.Warn(fmt.Sprintf("stream hub: push buffer full, dropped %s event for user %s conn %d",
				eventType, userID, connID))
		}
	}
}

func (h *streamHub) Snapshot(ctx context.Context, userID uuid.UUID) ([]StreamEvent, error) {
	h.mu.Lock()
	readers := h.readers
	h.mu.Unlock()

	type result struct {
		ch    StreamChannel
		count int64
	}
	results := make([]result, 0, NumStreamChannels)
	failed := make([]StreamChannel, 0)
	var firstErr error
	for i, reader := range readers {
		if reader == nil {
			continue
		}
		ch := StreamChannel(i)
		count, err := reader(ctx, userID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, ch)
			continue
		}
		results = append(results, result{ch: ch, count: count})
	}
	if len(results) == 0 {
		if firstErr == nil {
			firstErr = errors.New("stream hub: count reader not set")
		}
		return nil, firstErr
	}
	if firstErr != nil {
		logger.Log.Error("Failed to snapshot some stream channels, will retry on next sweep: " + firstErr.Error())
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.users[userID]
	if e == nil {
		e = &userEntry{conns: make(map[uint64]*hubConn)}
		h.users[userID] = e
	}
	events := make([]StreamEvent, 0, len(results))
	for _, r := range results {
		e.seq++
		st := &e.channels[r.ch]
		st.lastSent = r.count
		st.hasSent = true
		events = append(events, StreamEvent{ID: streamEventID(e.seq), Channel: r.ch, Count: r.count})
	}
	// 读失败的通道标脏，sweeper 下一拍补推（hasSent=false，任何值都会推）。
	for _, ch := range failed {
		e.channels[ch].dirty = true
	}
	return events, nil
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

// dirtyItem 一次 sweep 中待推送的（用户，通道）。
type dirtyItem struct {
	uid uuid.UUID
	ch  StreamChannel
}

// sweep 扫一遍脏（用户，通道）：读全量 → 与该通道 lastSent 比对 → 变了才投递。
// CountReader 可能阻塞（Redis/DB），在锁外调用。
func (h *streamHub) sweep() {
	h.mu.Lock()
	dirty := make([]dirtyItem, 0)
	for uid, e := range h.users {
		for i := range e.channels {
			st := &e.channels[i]
			if st.dirty && len(e.conns) > 0 {
				dirty = append(dirty, dirtyItem{uid: uid, ch: StreamChannel(i)})
			}
			st.dirty = false
		}
	}
	readers := h.readers
	h.mu.Unlock()

	for _, d := range dirty {
		reader := readers[d.ch]
		if reader == nil {
			continue
		}
		count, err := reader(context.Background(), d.uid)
		if err != nil {
			logger.Log.Error("Failed to read unread count for stream push: " + err.Error())
			// 重新标脏，下一拍重试。
			h.mu.Lock()
			if e := h.users[d.uid]; e != nil {
				e.channels[d.ch].dirty = true
			}
			h.mu.Unlock()
			continue
		}
		h.deliver(d.uid, d.ch, count)
	}
}

// deliver 去重后投递到该用户所有连接的对应通道。
func (h *streamHub) deliver(uid uuid.UUID, ch StreamChannel, count int64) {
	h.mu.Lock()
	e := h.users[uid]
	if e == nil || len(e.conns) == 0 {
		h.mu.Unlock()
		return
	}
	st := &e.channels[ch]
	if st.hasSent && st.lastSent == count {
		h.mu.Unlock()
		return
	}
	e.seq++
	st.lastSent = count
	st.hasSent = true
	ev := StreamEvent{ID: streamEventID(e.seq), Channel: ch, Count: count}
	conns := make([]chan StreamEvent, 0, len(e.conns))
	for _, c := range e.conns {
		conns = append(conns, c.counts[ch])
	}
	h.mu.Unlock()

	for _, c := range conns {
		sendLatest(c, ev)
	}
}

// sendLatest 非阻塞投递；缓冲满则丢最旧（推送值是全量，旧值已被取代）。
// 每个通道各自一个 channel，丢最旧不会丢掉别的通道的值。
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
