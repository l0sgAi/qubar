package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// 列表相关的哨兵错误。
var (
	// ErrInvalidCursor 列表游标无法解析，或不属于本次请求的列表（handler 映射 400）。
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrFollowListPrivate 目标用户把关注 / 粉丝列表设为私密，查看者不是本人（handler 映射 403）。
	ErrFollowListPrivate = errors.New("follow list is private")
	// ErrTooManyTargets 批量关系查询的用户 ID 数量超过上限。
	ErrTooManyTargets = errors.New("too many user ids")
)

// ListKind 列表类型。取值会写进游标（作用域校验），不要改已有值。
type ListKind string

const (
	// ListFollowing owner 关注的人，按关注时间倒序。
	ListFollowing ListKind = "following"
	// ListFollowers 关注 owner 的人，按对方关注 owner 的时间倒序。
	ListFollowers ListKind = "followers"
	// ListMutual 与 owner 互相关注的人，按 owner 关注对方的时间倒序。
	ListMutual ListKind = "mutual"
)

// FollowEdge 列表里的一条关系：对端用户 + keyset 键 (FollowTime, ID)。
// ID 是关系行主键，只用来在 FollowTime 相同时定序（行会复用，不代表关注时间）。
type FollowEdge struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	FollowTime time.Time
}

// ListRepository 关注关系的列表读取与批量关系查询（只读）。
type ListRepository interface {
	// ListEdges 按 (follow_time DESC, id DESC) 返回 ownerID 的一段列表，最多 limit 条。
	// after 为 nil 表示从头开始，否则只返回排在 after 之后的行（keyset，不含 after 本身）。
	// 不过滤对端用户的状态（仓储不读 users 表）。
	ListEdges(ctx context.Context, kind ListKind, ownerID uuid.UUID, after *FollowEdge, limit int) ([]FollowEdge, error)
	// GetRelations 一次查询返回 viewerID 与每个 targetID 的关系；没有任何关系的 ID 不出现在 map 里（视为 RelationNone）。
	GetRelations(ctx context.Context, viewerID uuid.UUID, targetIDs []uuid.UUID) (map[uuid.UUID]Relation, error)
}
