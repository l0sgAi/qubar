package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrCircleNotFound 圈子未找到（与旧 gorm.ErrRecordNotFound 对应）。
var ErrCircleNotFound = errors.New("circle not found")

// ErrMemberNotFound 成员记录未找到。
var ErrMemberNotFound = errors.New("member not found")

// ErrMemberStateConflict 成员状态/角色与预期不符（状态机非法迁移或并发变更，0 行受影响）。
var ErrMemberStateConflict = errors.New("member state conflict")

// ErrInvalidCursor 成员列表 keyset 游标非法（用户可控参数，防御性解析失败）。
var ErrInvalidCursor = errors.New("invalid cursor")

// ErrCircleNameExists 圈子名冲突（service 预检或 DB 唯一索引兜底）。
var ErrCircleNameExists = errors.New("circle name already exists")

// ErrCircleSlugExists 圈子 slug 冲突（service 预检或 DB 唯一索引兜底）。
var ErrCircleSlugExists = errors.New("circle slug already exists")

// CircleRepository 是 circle 领域的持久化接口（由 infrastructure 实现）。
type CircleRepository interface {
	// GetByID 根据 ID 获取圈子（仅未删除）。未找到返回 ErrCircleNotFound。
	GetByID(ctx context.Context, circleID uuid.UUID) (*Circle, error)
	// GetByIDs 批量获取圈子。
	GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*Circle, error)
	// ExistsByName 检查同名圈子是否已存在（未删除）。
	ExistsByName(ctx context.Context, name string) (bool, error)
	// ExistsBySlug 检查同 slug 圈子是否已存在。
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	// Create 创建圈子并自动将创建者设为圈主（事务）。
	Create(ctx context.Context, circle *Circle) error
	// Update 更新圈子资料的可编辑字段（CircleUpdateFields 中 nil 字段跳过）。
	// 圈子不存在返回 ErrCircleNotFound；唯一索引冲突返回 ErrCircleNameExists/ErrCircleSlugExists。
	Update(ctx context.Context, circleID uuid.UUID, fields CircleUpdateFields) error
	// ListPrivateIDs 列出全部私密圈（join_type=2）ID，含已删除圈子（其帖子仍可能留在 ES，
	// 可见性过滤宁多勿漏）。私密圈数量很少，结果由 PrivateCircleCache 缓存。
	ListPrivateIDs(ctx context.Context) ([]uuid.UUID, error)
}

// CircleUpdateFields 圈子资料的可编辑字段（值对象，nil = 不更新该字段）。
//
// Slug 传空串表示清除（repo 落 NULL，避开 slug 唯一索引对空串的碰撞）；
// CategoryID 指向 uuid.Nil 表示清除分类。
type CircleUpdateFields struct {
	Name        *string
	Slug        *string
	AvatarURL   *string
	CoverURL    *string
	Description *string
	Rule        *string
	CategoryID  *uuid.UUID
	JoinType    *int16
}

// MemberRepository 是圈子成员关系的持久化接口。
type MemberRepository interface {
	// GetMember 获取成员信息。未找到返回 ErrMemberNotFound。
	GetMember(ctx context.Context, circleID, userID uuid.UUID) (*CircleMember, error)
	// ListJoinedWithScore 列出用户已加入圈子（status IN (正常, 禁言)）的 (circleID, 加入时间ms)，
	// 按加入时间倒序。limit=0 表示不限制。用于 JoinedCirclesCache 重建。
	// 口径与增量维护一致：禁言不移出 ZSET，拉黑/退出移出，申请中不进入。
	ListJoinedWithScore(ctx context.Context, userID uuid.UUID, limit int) ([]JoinedMember, error)
	// ListJoinedMemberships 批量查询用户在给定圈子中的成员关系（仅 status IN (正常, 禁言)），
	// 返回 circleID → 成员行（role/status/mute_end_time/create_time）。一次查询，供列表回填。
	ListJoinedMemberships(ctx context.Context, userID uuid.UUID, circleIDs []uuid.UUID) (map[uuid.UUID]CircleMember, error)
	// ListJoinedByRole 按角色分页用户已加入的圈子（status IN (正常, 禁言)），
	// keyset 排序 (create_time DESC, circle_id DESC)，与 joined ZSET 的加入时间倒序一致。
	// after 为 nil 表示第一页；返回成员行（circle_id/role/status/mute_end_time/create_time）。
	ListJoinedByRole(ctx context.Context, userID uuid.UUID, role int16, after *JoinedKey, limit int) ([]CircleMember, error)
	// CountJoinedByRole 用户以指定角色加入的圈子数（status IN (正常, 禁言)）。
	CountJoinedByRole(ctx context.Context, userID uuid.UUID, role int16) (int64, error)
	// JoinCircle 用户自助加入圈子：仅公开圈（joinType=0）可从无记录 / 已退出变为正常；
	// 非公开圈对无记录 / 已退出拒绝且不写库；已在圈 / 拉黑拒绝。不产生待审核（0）记录。
	// 返回错误信息与旧 model.JoinCircle 一致（"user is already a member..." 等）。
	JoinCircle(ctx context.Context, circleID, userID uuid.UUID, joinType int16) (*CircleMember, error)
	// LeaveCircle 用户退出圈子（圈主不能退）。
	LeaveCircle(ctx context.Context, circleID, userID uuid.UUID) error
	// ListMembers 管理端成员列表（keyset 分页，排序对齐 idx_member_circle_role：
	// role DESC, create_time DESC, id DESC）。role/status 传 -1 表示不过滤。
	// userIDs 非空时按成员用户集合过滤（成员搜索场景：关键词先经 user 域搜索
	// 解析为至多百余个用户 ID；circle_member 对 (circle_id, user_id) 唯一，
	// 故过滤后至多 |userIDs| 行，游标翻页仍精确）。
	// 返回 (成员, 下一页游标)，游标空串表示没有更多；游标非法返回 ErrInvalidCursor 包装错误。
	// 查询前惰性解除已过期的禁言（与 GetMember 自愈一致，保证管理列表状态准确）。
	ListMembers(ctx context.Context, circleID uuid.UUID, role, status int16, userIDs []uuid.UUID, cursor string, size int) ([]CircleMember, string, error)
	// UpdateMemberRole 角色变更（CAS：WHERE role=fromRole AND status=normal）。
	// 目标状态不符或并发变更（0 行受影响）返回 ErrMemberStateConflict。
	UpdateMemberRole(ctx context.Context, circleID, userID uuid.UUID, fromRole, toRole int16) error
	// UpdateMemberStatus 状态迁移（CAS：WHERE status=fromStatus，0 行受影响返回 ErrMemberStateConflict）。
	// toStatus=禁言时写 muteEndTime；其余迁移统一清空 mute_end_time。
	UpdateMemberStatus(ctx context.Context, circleID, userID uuid.UUID, fromStatus, toStatus int16, muteEndTime time.Time) error
	// TransferOwner 转让圈主（单事务：from 降为普通成员、to 升为圈主）。
	// 任一条前置状态不满足则整体回滚并返回 ErrMemberStateConflict。
	TransferOwner(ctx context.Context, circleID, fromUser, toUser uuid.UUID) error
	// ListManagedCircles 列出用户作为圈主/管理员（role IN (20,30), status=normal）
	// 的圈子，keyword 非空时按 name/description 子串过滤；offset 分页返回 total。
	// 管理控制台专用：直查 PG 不走缓存/ES，角色变更立即可见。
	ListManagedCircles(ctx context.Context, userID uuid.UUID, keyword string, offset, size int) ([]ManagedCircle, int64, error)
}

// ManagedCircle 用户可管理的圈子视图（Circle + 当前用户在圈内的角色）。
//
// 领域自有结构（无基础设施依赖）：infrastructure 的 ListManagedCircles 扫描结果，
// application 层映射为管理端 DTO。嵌入 Circle 复用其 gorm 列标签，JOIN 查询
// `SELECT c.*, m.role AS my_role` 可直接扫描。
type ManagedCircle struct {
	Circle
	MyRole int16 `json:"my_role" gorm:"column:my_role"`
}

// CircleBaseCache 圈子基础信息缓存（不含统计）。
type CircleBaseCache interface {
	// GetBase 从缓存读取圈子基础信息。未命中返回 nil, nil。
	GetBase(ctx context.Context, circleID uuid.UUID) (*CircleBaseInfo, error)
	// SetBase 写入圈子基础信息缓存。
	SetBase(ctx context.Context, circleID uuid.UUID, info *CircleBaseInfo) error
	// DeleteBase 删除圈子基础信息缓存（编辑圈子资料后失效）。
	DeleteBase(ctx context.Context, circleID uuid.UUID) error
}

// PrivateCircleCache 私密圈 ID 集合缓存（私密圈内容可见性过滤用，集合很小）。
//
// 建圈、修改 join_type 后由 service 调 Delete 失效；短 TTL 兜底并发回填的旧值。
type PrivateCircleCache interface {
	// Get 读取集合。未命中返回 nil, false, nil；命中空集合返回空切片, true。
	Get(ctx context.Context) ([]uuid.UUID, bool, error)
	// Set 写入集合（含空集合）。
	Set(ctx context.Context, ids []uuid.UUID) error
	// Delete 失效集合。
	Delete(ctx context.Context) error
}

// CircleStatsCache 圈子统计信息缓存（member_count/post_count/hot）。
type CircleStatsCache interface {
	// GetStats 读取统计信息。未命中返回 nil, nil（触发 DB 恢复）。
	GetStats(ctx context.Context, circleID uuid.UUID) (*CircleStatistics, error)
	// StatsExists 检查统计 Hash 是否已回种。
	StatsExists(ctx context.Context, circleID uuid.UUID) (bool, error)
	// SeedStatsIfAbsent 从 DB 回种统计信息，只写尚不存在的字段（HSETNX），不覆盖并发的增减。
	SeedStatsIfAbsent(ctx context.Context, circleID uuid.UUID, stats *CircleStatistics) error
	// IncrMemberCount / DecrMemberCount 原子增减成员计数；Hash 未回种时不写（先 SeedStatsIfAbsent）。
	IncrMemberCount(ctx context.Context, circleID uuid.UUID) error
	DecrMemberCount(ctx context.Context, circleID uuid.UUID) error
	// IncrPostCount 原子增加帖子计数；Hash 未回种时不写。
	IncrPostCount(ctx context.Context, circleID uuid.UUID) error
}

// JoinedMember 成员关系（ZSET 元素 + score，用于缓存重建/写入）。
type JoinedMember struct {
	CircleID uuid.UUID
	ScoreMs  int64 // 加入时间 Unix 毫秒
}

// JoinedKey 按角色分页「我加入的圈子」的 keyset 位置（成员行 create_time + circle_id）。
type JoinedKey struct {
	CreateTime time.Time
	CircleID   uuid.UUID
}

// JoinedCirclesCache 用户已加入圈子 ID 的 ZSET 缓存。
//
// member=circle_id, score=加入时间 Unix 毫秒，倒序。支持无上限成员数：
// 分页/批量按 rank 取，永不物化全量列表。
type JoinedCirclesCache interface {
	// PageByRank 倒序按 rank 区间 [start, start+limit) 取 circleID。
	PageByRank(ctx context.Context, userID uuid.UUID, start, limit int64) ([]uuid.UUID, error)
	// Card 成员总数（ZCARD）。
	Card(ctx context.Context, userID uuid.UUID) (int64, error)
	// Exists ZSET 是否存在（miss 检测，触发 DB 重建）。
	Exists(ctx context.Context, userID uuid.UUID) (bool, error)
	// Add 加入（ZADD，score=加入时间ms）。
	Add(ctx context.Context, userID, circleID uuid.UUID, scoreMs int64) error
	// Remove 退出（ZREM）。
	Remove(ctx context.Context, userID uuid.UUID, circleID uuid.UUID) error
	// Rebuild 全量重建（先 Del 再批量 ZADD + 续 TTL）。
	Rebuild(ctx context.Context, userID uuid.UUID, members []JoinedMember) error
}

// CircleEventPublisher 圈子事件发布（用于异步持久化计数到 DB）。
type CircleEventPublisher interface {
	// PublishMemberCount 发布成员计数变化消息（+1 表示加入，-1 表示退出）。
	PublishMemberCount(ctx context.Context, circleID uuid.UUID, delta int64) error
	// PublishPostCount 发布帖子计数 +1 消息（新发帖）。
	PublishPostCount(ctx context.Context, circleID uuid.UUID) error
}

// CircleBrief 是给跨领域调用的圈子精简视图（Facade DTO）。
//
// post 领域组装帖子列表时需要圈子名称/头像，依赖此视图而非完整 Circle。
type CircleBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// CircleBaseInfo 圈子基础信息（不含统计，对应旧 redispkg.CircleBaseInfo）。
type CircleBaseInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`
}

// CircleStatistics 圈子统计信息（对应旧 redispkg.CircleStatistics）。
type CircleStatistics struct {
	MemberCount int `json:"member_count"`
	PostCount   int `json:"post_count"`
	Hot         int `json:"hot"`
}
