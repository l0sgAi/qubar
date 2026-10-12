package redpanda

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// testDeltaSpec 以 postDeltaBuf 为缓冲的最小 spec，persist 可注入。
func testDeltaSpec(persist func(postDeltaBuf) error) batchConsumerSpec[PostHotMessage, postDeltaBuf] {
	return batchConsumerSpec[PostHotMessage, postDeltaBuf]{
		name:    "test",
		newBuf:  newPostDeltaBuf,
		add:     func(b postDeltaBuf, m PostHotMessage) { b[m.PostID] += m.Delta },
		size:    postDeltaBufSize,
		merge:   postDeltaBuf.merge,
		persist: persist,
	}
}

func newTestBatchConsumer(persist func(postDeltaBuf) error, c messageCommitter) *batchConsumer[PostHotMessage, postDeltaBuf] {
	return newBatchConsumer(testDeltaSpec(persist), c, time.Hour)
}

func committedOffsets(msgs []kafka.Message) map[int]int64 {
	out := make(map[int]int64)
	for _, m := range msgs {
		out[m.Partition] = m.Offset
	}
	return out
}

func TestBatchConsumerFlush_CommitsMaxOffsetAfterPersist(t *testing.T) {
	var persisted postDeltaBuf
	fc := &fakeCommitter{}
	c := newTestBatchConsumer(func(b postDeltaBuf) error {
		if len(fc.committed) != 0 {
			t.Fatal("offsets committed before persist")
		}
		persisted = b
		return nil
	}, fc)

	p := uuid.New()
	c.addMessage(&PostHotMessage{PostID: p, Delta: 2}, km(0, 5))
	c.addMessage(&PostHotMessage{PostID: p, Delta: 3}, km(0, 7))
	c.addMessage(&PostHotMessage{PostID: p, Delta: -1}, km(1, 4))
	c.flush()

	if persisted[p] != 4 {
		t.Fatalf("persisted delta = %d, want 4", persisted[p])
	}
	got := committedOffsets(fc.committed)
	if len(fc.committed) != 2 || got[0] != 7 || got[1] != 4 {
		t.Fatalf("committed = %v, want p0=7 p1=4 once each", got)
	}
}

func TestBatchConsumerFlush_PersistFailureRestoresAndRetries(t *testing.T) {
	fail := true
	var persisted []int64
	fc := &fakeCommitter{}
	p := uuid.New()
	c := newTestBatchConsumer(func(b postDeltaBuf) error {
		if fail {
			return errors.New("db down")
		}
		persisted = append(persisted, b[p])
		return nil
	}, fc)

	c.addMessage(&PostHotMessage{PostID: p, Delta: 5}, km(0, 1))
	c.flush()
	if len(fc.committed) != 0 {
		t.Fatal("must not commit when persist fails")
	}

	// 失败期间又来新消息：重试时应与失败批次合并。
	c.addMessage(&PostHotMessage{PostID: p, Delta: 2}, km(0, 2))
	fail = false
	c.flush()

	if len(persisted) != 1 || persisted[0] != 7 {
		t.Fatalf("persisted = %v, want single batch with delta 7", persisted)
	}
	if got := committedOffsets(fc.committed); got[0] != 2 {
		t.Fatalf("committed = %v, want p0=2", got)
	}
}

func TestBatchConsumerFlush_CommitFailureDoesNotRepersist(t *testing.T) {
	calls := 0
	fc := &fakeCommitter{err: errors.New("rebalance")}
	c := newTestBatchConsumer(func(postDeltaBuf) error { calls++; return nil }, fc)

	c.addMessage(&PostHotMessage{PostID: uuid.New(), Delta: 1}, km(0, 1))
	c.flush()
	c.flush()

	if calls != 1 {
		t.Fatalf("persist calls = %d, want 1 (already persisted batch must not be re-applied)", calls)
	}
	if len(c.offsets) != 0 {
		t.Fatal("failed commit offsets must not be carried into later flushes")
	}
}

func TestBatchConsumerFlush_PoisonMessageOnlyAdvancesOffset(t *testing.T) {
	calls := 0
	fc := &fakeCommitter{}
	c := newTestBatchConsumer(func(postDeltaBuf) error { calls++; return nil }, fc)

	c.addMessage(nil, km(0, 9))
	c.flush()

	if calls != 0 {
		t.Fatal("nothing to persist for poison-only batch")
	}
	if got := committedOffsets(fc.committed); got[0] != 9 {
		t.Fatalf("committed = %v, want p0=9", got)
	}
}

func TestBatchConsumerStop_DrainsAndDropsLateMessages(t *testing.T) {
	var persisted postDeltaBuf
	fc := &fakeCommitter{}
	c := newTestBatchConsumer(func(b postDeltaBuf) error { persisted = b; return nil }, fc)
	go c.run()

	p := uuid.New()
	c.addMessage(&PostHotMessage{PostID: p, Delta: 3}, km(0, 1))
	c.Stop()
	c.Stop() // 幂等

	if persisted[p] != 3 {
		t.Fatalf("Stop must flush buffered deltas, persisted = %v", persisted)
	}
	if got := committedOffsets(fc.committed); got[0] != 1 {
		t.Fatalf("Stop must commit drained offsets, got %v", got)
	}

	c.addMessage(&PostHotMessage{PostID: p, Delta: 1}, km(0, 2))
	if c.spec.size(c.buf) != 0 || len(c.offsets) != 0 {
		t.Fatal("messages after Stop must be dropped (left uncommitted for redelivery)")
	}
}

func TestBatchConsumer_CountThresholdSignalsFlush(t *testing.T) {
	spec := testDeltaSpec(func(postDeltaBuf) error { return nil })
	spec.flushMessages = 2
	c := newBatchConsumer(spec, &fakeCommitter{}, time.Hour)

	c.addMessage(&PostHotMessage{PostID: uuid.New(), Delta: 1}, km(0, 1))
	select {
	case <-c.flushNow:
		t.Fatal("flush signalled before threshold")
	default:
	}
	c.addMessage(&PostHotMessage{PostID: uuid.New(), Delta: 1}, km(0, 2))
	select {
	case <-c.flushNow:
	default:
		t.Fatal("flush not signalled at threshold")
	}
}

func TestCircleStatsBuf_MergeAddsDeltas(t *testing.T) {
	id := uuid.New()
	dst := newCircleStatsBuf()
	dst.add(CircleStatisticsMessage{Type: StatisticsTypeCircleCount, CircleID: id, Value: 1})
	src := newCircleStatsBuf()
	src.add(CircleStatisticsMessage{Type: StatisticsTypeCircleCount, CircleID: id, Value: 2})
	src.add(CircleStatisticsMessage{Type: StatisticsTypePostCount, CircleID: id, Value: -1})
	src.add(CircleStatisticsMessage{Type: "unknown", CircleID: id, Value: 9})

	dst.merge(src)
	if dst.members[id] != 3 || dst.posts[id] != -1 || dst.size() != 2 {
		t.Fatalf("members=%d posts=%d size=%d", dst.members[id], dst.posts[id], dst.size())
	}
}

func TestPostStatsBuf_MergeAddsAllFields(t *testing.T) {
	id := uuid.New()
	dst := make(postStatsBuf)
	dst.add(PostStatisticsMessage{Type: StatisticsTypePostView, PostID: id, Value: 1})
	src := make(postStatsBuf)
	src.add(PostStatisticsMessage{Type: StatisticsTypePostView, PostID: id, Value: 2})
	src.add(PostStatisticsMessage{Type: StatisticsTypePostLike, PostID: id, Value: 1})
	src.add(PostStatisticsMessage{Type: StatisticsTypePostCollect, PostID: id, Value: -1})

	dst.merge(src)
	d := dst[id]
	if d.ViewCount != 3 || d.LikeCount != 1 || d.CollectCount != -1 {
		t.Fatalf("got %+v", *d)
	}
}

func TestHistorySpec_SkipsNilIDsAndCountsViews(t *testing.T) {
	u, p := uuid.New(), uuid.New()
	b := historySpec.newBuf()
	historySpec.add(b, HistoryEventMessage{UserID: u, PostID: p})
	historySpec.add(b, HistoryEventMessage{UserID: u, PostID: p})
	historySpec.add(b, HistoryEventMessage{UserID: u})
	if len(b) != 1 || b[userPostKey{UserID: u, PostID: p}] != 2 {
		t.Fatalf("got %v", b)
	}
}

func TestFlushIntervalOr(t *testing.T) {
	if got := flushIntervalOr(0, 13, time.Minute); got != 13*time.Minute {
		t.Fatalf("fallback = %v", got)
	}
	if got := flushIntervalOr(5, 13, time.Second); got != 5*time.Second {
		t.Fatalf("configured = %v", got)
	}
}

func TestPostInteractionBuf_DedupesByUserPostKeepingMax(t *testing.T) {
	u, p := uuid.New(), uuid.New()
	b := make(postInteractionBuf)
	b.add(PostInteractionMessage{UserID: u, PostID: p, Weight: 5, Ts: 100})
	b.add(PostInteractionMessage{UserID: u, PostID: p, Weight: 2, Ts: 300})
	b.add(PostInteractionMessage{UserID: u, Weight: 1, Ts: 1}) // nil post

	src := make(postInteractionBuf)
	src.add(PostInteractionMessage{UserID: u, PostID: p, Weight: 3, Ts: 200})
	b.merge(src)

	if len(b) != 1 {
		t.Fatalf("len = %d, want 1 row per (user, post)", len(b))
	}
	if got := b[userPostKey{UserID: u, PostID: p}]; got.Weight != 5 || got.TsMs != 300 {
		t.Fatalf("got %+v, want weight=5 ts=300", got)
	}
}
