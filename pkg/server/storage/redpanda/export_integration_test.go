//go:build integration

package redpanda

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// 供外部测试包 redpanda_test 的集成测试调用（testkit 依赖本包，包内测试无法导入 testkit）。

// PersistPostStatsForTest 帖子统计消费者落库：ids 每帖 view / like 各 +delta。
func PersistPostStatsForTest(ids []uuid.UUID, delta int64) error {
	buf := make(postStatsBuf, len(ids))
	for _, id := range ids {
		buf[id] = &postStatDelta{ViewCount: delta, LikeCount: delta}
	}
	_, err := batchUpdatePostStats(buf)
	return err
}

// UpdatePostHotForTest 帖子热度消费者落库：按 ids 给定顺序构造行，每帖 hot +delta。
func UpdatePostHotForTest(ids []uuid.UUID, delta int64) error {
	rows := make([]postHotRow, len(ids))
	for i, id := range ids {
		rows[i] = postHotRow{PostID: id, Delta: delta}
	}
	return updatePostHotDB(rows)
}

// UpdatePostCollectsForTest 收藏消费者落库：每帖 collect_count +delta。
func UpdatePostCollectsForTest(ids []uuid.UUID, delta int64) error {
	buf := make(postDeltaBuf, len(ids))
	for _, id := range ids {
		buf[id] = delta
	}
	return batchUpdatePostCollects(buf)
}

// PersistPostLikesForTest 点赞消费者落库：userID 点赞 ids 中每个帖子（新建 post_like 行，like_count +1）。
func PersistPostLikesForTest(userID uuid.UUID, ids []uuid.UUID) error {
	states := make([]*likeState, len(ids))
	for i, id := range ids {
		states[i] = &likeState{EventType: likeEventTypePost, UserID: userID, TargetID: id, Liked: true}
	}
	return persistLikeStates(states)
}

// PersistCircleStatsForTest 圈子统计消费者落库：每圈 member_count / post_count 各 +delta。
func PersistCircleStatsForTest(ids []uuid.UUID, delta int64) error {
	buf := newCircleStatsBuf()
	for _, id := range ids {
		buf.members[id] += delta
		buf.posts[id] += delta
	}
	return persistCircleStats(buf)
}

// UpdateCircleHotForTest 圈子热度同步器落库：每圈 hot +delta。
func UpdateCircleHotForTest(ids []uuid.UUID, delta int64) error {
	deltas := make(map[uuid.UUID]int64, len(ids))
	for _, id := range ids {
		deltas[id] += delta
	}
	return (&CircleHotSyncer{}).batchUpdateCircleHot(deltas)
}

// UpsertNotificationForTest 通知消费者落库：写一行通知（id 由调用方给定，模拟 Go 侧 NewID）。
func UpsertNotificationForTest(id, recipientID, actorID uuid.UUID, noticeType int16, postID uuid.UUID, snippet string) error {
	return upsertNotifications([]noticeRow{{
		ID: id, RecipientID: recipientID, ActorID: actorID, NoticeType: noticeType, PostID: &postID, Snippet: snippet,
	}})
}

// PostStatsConsumerForTest 帖子统计消费者（生产 spec，不带 reader / ticker）：测试逐条喂消息、手动 flush。
type PostStatsConsumerForTest struct {
	c *batchConsumer[PostStatisticsMessage, postStatsBuf]
}

// NewPostStatsConsumerForTest afterPersist 在真实落库成功之后调用；返回非 nil 时本次 flush 按失败处理
// （缓冲合并回、offset 不提交），模拟「已落库但未确认」后的重试。
func NewPostStatsConsumerForTest(committer interface {
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}, afterPersist func() error) *PostStatsConsumerForTest {
	spec := postStatsSpec
	spec.persist = func(b postStatsBuf) error {
		if err := persistPostStats(b); err != nil {
			return err
		}
		if afterPersist != nil {
			return afterPersist()
		}
		return nil
	}
	c := newBatchConsumer(spec, committer, time.Hour)
	c.ticker.Stop() // 不启动 run()，flush 由测试驱动
	return &PostStatsConsumerForTest{c: c}
}

// Add 与读循环一致：解析消息并合并进缓冲（解析失败只推进 offset）。
func (p *PostStatsConsumerForTest) Add(km kafka.Message) {
	var msg PostStatisticsMessage
	if err := json.Unmarshal(km.Value, &msg); err != nil {
		p.c.addMessage(nil, km)
		return
	}
	p.c.addMessage(&msg, km)
}

// Flush 落库 → 提交 offset（失败时合并回缓冲，下次 Flush 重试）。
func (p *PostStatsConsumerForTest) Flush() { p.c.flush() }
