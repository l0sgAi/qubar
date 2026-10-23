package redpanda

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// fakeModerationReader 按顺序吐出预设消息，取完后阻塞到 ctx 取消；记录提交的 offset。
type fakeModerationReader struct {
	mu        sync.Mutex
	msgs      []kafka.Message
	committed []int64
}

func (r *fakeModerationReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	r.mu.Lock()
	if len(r.msgs) > 0 {
		m := r.msgs[0]
		r.msgs = r.msgs[1:]
		r.mu.Unlock()
		return m, nil
	}
	r.mu.Unlock()
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (r *fakeModerationReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range msgs {
		r.committed = append(r.committed, m.Offset)
	}
	return nil
}

func (r *fakeModerationReader) Close() error { return nil }

func (r *fakeModerationReader) committedOffsets() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.committed...)
}

func moderationMsg(t *testing.T, offset int64, postID uuid.UUID) kafka.Message {
	t.Helper()
	v, err := json.Marshal(PostModerationMessage{PostID: postID})
	if err != nil {
		t.Fatal(err)
	}
	return kafka.Message{Offset: offset, Value: v}
}

func startFakeModerationConsumer(r moderationReader, handle PostModerationHandler) *postModerationConsumer {
	c := &postModerationConsumer{reader: r, handle: handle, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(ctx)
	return c
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 处理失败重试同一条，成功后才提交；毒消息直接跳过提交；panic 视为坏数据跳过。
func TestPostModerationConsumer_RetryPoisonAndPanic(t *testing.T) {
	retryID, panicID, okID := uuid.New(), uuid.New(), uuid.New()
	r := &fakeModerationReader{msgs: []kafka.Message{
		{Offset: 0, Value: []byte("not json")},
		moderationMsg(t, 1, uuid.Nil), // 缺 post_id 同样是毒消息
		moderationMsg(t, 2, retryID),
		moderationMsg(t, 3, panicID),
		moderationMsg(t, 4, okID),
	}}

	var mu sync.Mutex
	calls := map[uuid.UUID]int{}
	handle := func(_ context.Context, id uuid.UUID) error {
		mu.Lock()
		calls[id]++
		n := calls[id]
		mu.Unlock()
		switch {
		case id == retryID && n == 1:
			return errors.New("db down")
		case id == panicID:
			panic("bad post")
		}
		return nil
	}

	c := startFakeModerationConsumer(r, handle)
	waitFor(t, func() bool { return len(r.committedOffsets()) == 5 })
	c.stop()

	got := r.committedOffsets()
	for i, want := range []int64{0, 1, 2, 3, 4} {
		if got[i] != want {
			t.Fatalf("committed offsets = %v, want [0 1 2 3 4] in order", got)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls[retryID] != 2 || calls[panicID] != 1 || calls[okID] != 1 {
		t.Fatalf("handler calls = %v, want retry=2 panic=1 ok=1", calls)
	}
}

// 关停时进行中的处理被取消：不提交 offset（重启后重投）。
func TestPostModerationConsumer_StopDoesNotCommitInFlight(t *testing.T) {
	r := &fakeModerationReader{msgs: []kafka.Message{moderationMsg(t, 7, uuid.New())}}
	started := make(chan struct{})
	handle := func(ctx context.Context, _ uuid.UUID) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	c := startFakeModerationConsumer(r, handle)
	<-started
	c.stop()

	if got := r.committedOffsets(); len(got) != 0 {
		t.Fatalf("committed %v, want nothing committed for the in-flight message", got)
	}
}
