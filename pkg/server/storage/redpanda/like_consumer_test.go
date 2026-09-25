package redpanda

import (
	"context"
	"errors"
	"os"
	"testing"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

func km(partition int, offset int64) kafka.Message {
	return kafka.Message{Partition: partition, Offset: offset}
}

func TestToLikeState(t *testing.T) {
	u, p := uuid.New(), uuid.New()
	cases := []struct {
		name  string
		msg   LikeEventMessage
		ok    bool
		liked bool
	}{
		{"like", LikeEventMessage{Type: likeEventTypePost, UserID: u, TargetID: p, Amount: 1}, true, true},
		{"unlike", LikeEventMessage{Type: likeEventTypeComment, UserID: u, TargetID: p, Amount: -1}, true, false},
		{"legacy summed amount keeps sign", LikeEventMessage{Type: likeEventTypePost, UserID: u, TargetID: p, Amount: 2}, true, true},
		{"zero amount", LikeEventMessage{Type: likeEventTypePost, UserID: u, TargetID: p}, false, false},
		{"unknown type", LikeEventMessage{Type: "share", UserID: u, TargetID: p, Amount: 1}, false, false},
		{"nil user", LikeEventMessage{Type: likeEventTypePost, TargetID: p, Amount: 1}, false, false},
		{"nil target", LikeEventMessage{Type: likeEventTypePost, UserID: u, Amount: 1}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ok := toLikeState(tc.msg)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && s.Liked != tc.liked {
				t.Fatalf("liked = %v, want %v", s.Liked, tc.liked)
			}
		})
	}
}

func TestLikeBuffer_LastStateWins(t *testing.T) {
	b := newLikeBuffer()
	u, p := uuid.New(), uuid.New()
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p, Liked: true}, km(0, 1))
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p, Liked: false}, km(0, 2))
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p, Liked: true}, km(0, 3))

	if len(b.states) != 1 {
		t.Fatalf("states = %d, want 1", len(b.states))
	}
	for _, s := range b.states {
		if !s.Liked {
			t.Fatal("want last state liked=true")
		}
	}
	if got := b.offsets[0].Offset; got != 3 {
		t.Fatalf("partition 0 offset = %d, want 3", got)
	}
}

func TestLikeBuffer_TracksMaxOffsetPerPartition(t *testing.T) {
	b := newLikeBuffer()
	b.add(nil, km(0, 5))
	b.add(nil, km(1, 2))
	b.add(nil, km(0, 4)) // 乱序到达也保留最大值
	if len(b.states) != 0 {
		t.Fatalf("nil state must only track offset, got %d states", len(b.states))
	}
	if b.offsets[0].Offset != 5 || b.offsets[1].Offset != 2 {
		t.Fatalf("offsets = %v", b.offsets)
	}
}

func TestLikeBuffer_RestoreKeepsNewerState(t *testing.T) {
	b := newLikeBuffer()
	u, p1, p2 := uuid.New(), uuid.New(), uuid.New()
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p1, Liked: true}, km(0, 1))
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p2, Liked: true}, km(0, 2))
	failed := b.take()

	// flush 期间 p1 又被取消，随后失败批次合并回
	b.add(&likeState{EventType: likeEventTypePost, UserID: u, TargetID: p1, Liked: false}, km(0, 3))
	b.restoreStates(failed.states)
	b.restoreOffsets(failed.offsets)

	if got := b.states[likeStateKey(likeEventTypePost, u, p1)].Liked; got {
		t.Fatal("newer unlike for p1 must not be overwritten by restored like")
	}
	if _, ok := b.states[likeStateKey(likeEventTypePost, u, p2)]; !ok {
		t.Fatal("p2 from failed batch must be restored")
	}
	if got := b.offsets[0].Offset; got != 3 {
		t.Fatalf("offset = %d, want 3", got)
	}
}

type fakeCommitter struct {
	committed []kafka.Message
	err       error
}

func (f *fakeCommitter) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.err != nil {
		return f.err
	}
	f.committed = append(f.committed, msgs...)
	return nil
}

func newTestAggregator(persist func([]*likeState) error, c messageCommitter) *LikeEventAggregator {
	return &LikeEventAggregator{buf: newLikeBuffer(), persist: persist, committer: c}
}

func TestLikeAggregatorFlush_CommitsOnlyAfterPersist(t *testing.T) {
	var persisted []*likeState
	c := &fakeCommitter{}
	a := newTestAggregator(func(s []*likeState) error { persisted = s; return nil }, c)
	a.addMessage(&likeState{EventType: likeEventTypePost, UserID: uuid.New(), TargetID: uuid.New(), Liked: true}, km(0, 7))
	a.addMessage(nil, km(1, 9))

	a.flush()

	if len(persisted) != 1 {
		t.Fatalf("persisted = %d, want 1", len(persisted))
	}
	if len(c.committed) != 2 {
		t.Fatalf("committed = %d msgs, want 2 (one per partition)", len(c.committed))
	}
	if !a.buf.empty() {
		t.Fatal("buffer must be empty after successful flush")
	}
}

func TestLikeAggregatorFlush_PersistFailureRestoresAndSkipsCommit(t *testing.T) {
	c := &fakeCommitter{}
	a := newTestAggregator(func([]*likeState) error { return errors.New("db down") }, c)
	a.addMessage(&likeState{EventType: likeEventTypePost, UserID: uuid.New(), TargetID: uuid.New(), Liked: true}, km(0, 7))

	a.flush()

	if len(c.committed) != 0 {
		t.Fatal("must not commit offsets when persist fails")
	}
	if len(a.buf.states) != 1 || a.buf.offsets[0].Offset != 7 {
		t.Fatalf("failed batch must be restored, got states=%d offsets=%v", len(a.buf.states), a.buf.offsets)
	}
}

func TestLikeAggregatorFlush_CommitFailureDoesNotRetryOrRepersist(t *testing.T) {
	c := &fakeCommitter{err: errors.New("broker down")}
	calls := 0
	a := newTestAggregator(func([]*likeState) error { calls++; return nil }, c)
	a.addMessage(&likeState{EventType: likeEventTypePost, UserID: uuid.New(), TargetID: uuid.New(), Liked: true}, km(0, 7))

	a.flush()

	if !a.buf.empty() {
		t.Fatal("nothing may be restored after a commit failure (persist already succeeded)")
	}

	// 下一批带来更大的 offset：一次提交即覆盖之前失败的 offset。
	c.err = nil
	a.addMessage(nil, km(0, 9))
	a.flush()
	if calls != 1 {
		t.Fatalf("persist calls = %d, want 1", calls)
	}
	if len(c.committed) != 1 || c.committed[0].Offset != 9 {
		t.Fatalf("committed = %v", c.committed)
	}
}

func TestLikeAggregator_DropsMessagesAfterStop(t *testing.T) {
	a := newTestAggregator(func([]*likeState) error { return nil }, &fakeCommitter{})
	a.stopped = true
	a.addMessage(&likeState{EventType: likeEventTypePost, UserID: uuid.New(), TargetID: uuid.New(), Liked: true}, km(0, 1))
	if !a.buf.empty() {
		t.Fatal("messages after stop must not be buffered (left uncommitted for redelivery)")
	}
}

func TestBuildLikeRows(t *testing.T) {
	u, p, c, cp := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	posts, comments := buildLikeRows([]*likeState{
		{EventType: likeEventTypePost, UserID: u, TargetID: p, Liked: true},
		{EventType: likeEventTypeComment, UserID: u, TargetID: c, PostID: cp, Liked: false},
		{EventType: likeEventTypeComment, UserID: u, TargetID: uuid.New()}, // 缺冗余 post_id
	})
	if len(posts) != 1 || posts[0].PostID != p || !posts[0].Liked || posts[0].ID == uuid.Nil {
		t.Fatalf("posts = %+v", posts)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	if comments[0].PostID == nil || *comments[0].PostID != cp || comments[0].Liked {
		t.Fatalf("comment[0] = %+v", comments[0])
	}
	if comments[1].PostID != nil {
		t.Fatal("missing post_id must serialize as NULL")
	}
}
