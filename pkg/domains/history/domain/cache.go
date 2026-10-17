package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ViewEntry 浏览历史条目(帖子ID + 最近访问时间)。
// ViewedAt 取自 ZSET score(热路径=浏览时刻)或 DB update_time(冷启动回源)。
type ViewEntry struct {
	PostID   uuid.UUID
	ViewedAt time.Time
}

// PostHistoryCache 帖子浏览历史缓存(Redis ZSET,即时读 + cap 500 去重)。
//
// ZSET key: user:view:posts:{user_id},score=访问时间(Unix 毫秒),member=postId。
// 列表读即 ZRevRangeWithScores(最近访问倒序 + 取 score 还原访问时间);cap 500 超限按最低 score 淘汰。
type PostHistoryCache interface {
	// RecordView 原子记录一次浏览(ZADD upsert + trim 500 + EXPIRE, Lua 脚本)。
	// 重复浏览同帖则 bump score 移到「最近浏览」顶部。
	RecordView(ctx context.Context, userID, postID uuid.UUID) error
	// ListViews 倒序取浏览历史 [offset, offset+size-1] 区间条目(含访问时间,最近访问倒序)。
	// 返回 ViewEntry 列表、总数(ZCARD)、error。total==0 触发上层冷启动回源。
	ListViews(ctx context.Context, userID uuid.UUID, offset, size int) (entries []ViewEntry, total int64, err error)
	// Backfill DB 回源后批量回填 ZSET(冷启动恢复)。
	// entries 须已按 ViewedAt DESC 排序(由 repo 保证);用 ViewedAt 作 score,回填后 ZSET 序 = DB 序。
	Backfill(ctx context.Context, userID uuid.UUID, entries []ViewEntry) error
}
