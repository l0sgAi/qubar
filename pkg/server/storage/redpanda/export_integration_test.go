//go:build integration

package redpanda

import "github.com/google/uuid"

// 供外部测试包 redpanda_test 的集成测试调用（testkit 依赖本包，包内测试无法导入 testkit）。

// PersistPostStatsForTest 帖子统计消费者落库：ids 每帖 view / like 各 +delta。
func PersistPostStatsForTest(ids []uuid.UUID, delta int64) error {
	buf := make(postStatsBuf, len(ids))
	for _, id := range ids {
		buf[id] = &postStatDelta{ViewCount: delta, LikeCount: delta}
	}
	return batchUpdatePostStats(buf)
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
