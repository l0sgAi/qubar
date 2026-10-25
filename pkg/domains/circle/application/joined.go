package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// 「我加入的圈子」列表：角色筛选（PG keyset）、成员关系回填、游标编解码。
// 无筛选的 ZSET rank 主流程见 service.go loadJoinedCircles。

// errInvalidJoinedRoleFilter 列表 role 参数非法（只接受 0 / 10 / 20 / 30）。
var errInvalidJoinedRoleFilter = errors.New("role must be 30 (owner), 20 (admin), 10 (member) or 0 (all)")

// IsInvalidJoinedRoleFilterErr 判断是否加入圈子列表的 role 参数非法。
func IsInvalidJoinedRoleFilterErr(err error) bool { return errors.Is(err, errInvalidJoinedRoleFilter) }

// joinedListQuery GetMyCircles / GetUserCircles 的统一入参。
type joinedListQuery struct {
	userID     uuid.UUID
	keyword    string
	role       int16 // 0 = 全部；10/20/30 单选筛选
	size       int
	cursor     string
	withStatus bool // 仅本人列表回填 member_status / mute_end_time
}

// isJoinedRoleFilter role 筛选取值是否合法。
func isJoinedRoleFilter(role int16) bool {
	return role == 0 || isMemberRole(role)
}

// isMemberRole 是否为成员角色值（10/20/30）。
func isMemberRole(role int16) bool {
	switch role {
	case domain.MemberRoleMember, domain.MemberRoleAdmin, domain.MemberRoleOwner:
		return true
	}
	return false
}

// loadJoinedByRole 角色筛选模式：PG circle_member 按 (create_time DESC, circle_id DESC) keyset
// 取 ID → ES SearchMy 过滤 status=1/deleted=0 并 hydrate。结果按加入时间倒序。
func (s *circleServiceImpl) loadJoinedByRole(ctx context.Context, q joinedListQuery, after *domain.JoinedKey) (*MyCircleSearchResult, error) {
	if q.keyword == "" {
		return s.browseJoinedByRole(ctx, q, after)
	}
	return s.searchJoinedByRole(ctx, q, after)
}

// browseJoinedByRole 角色筛选浏览：多取 1 行判断是否还有下一页，total 为该角色的加入数。
func (s *circleServiceImpl) browseJoinedByRole(ctx context.Context, q joinedListQuery, after *domain.JoinedKey) (*MyCircleSearchResult, error) {
	total, err := s.memberRepo.CountJoinedByRole(ctx, q.userID, q.role)
	if err != nil {
		return nil, err
	}
	rows, err := s.memberRepo.ListJoinedByRole(ctx, q.userID, q.role, after, q.size+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > q.size
	if hasMore {
		rows = rows[:q.size]
	}
	hits, err := s.hydrateJoinedRows(ctx, rows, "", q.withStatus)
	if err != nil {
		return nil, err
	}
	next := ""
	if hasMore {
		next = encodeJoinedRoleCursor(q.role, rows[len(rows)-1])
	}
	return &MyCircleSearchResult{
		Circles: hitDocs(hits), Total: total, Size: q.size, SearchAfter: next,
	}, nil
}

// searchJoinedByRole 角色筛选 + 关键字：PG 按 keyset 批量取 ID → 每批 ES 过滤 → 累积到 size。
//
// 批内命中按加入时间排序后截断，游标落在最后一个返回命中的成员行上，
// 未返回的命中下一页从该位置之后重扫，不丢不重。
// 扫满 joinedSearchMaxScan 仍未集齐 → Truncated=true。total 恒 0（与无筛选的 keyword 模式一致）。
func (s *circleServiceImpl) searchJoinedByRole(ctx context.Context, q joinedListQuery, after *domain.JoinedKey) (*MyCircleSearchResult, error) {
	collected := make([]MyCircleDoc, 0, q.size)
	pos := after
	scanned := 0
	exhausted := false
	hitMaxScan := false

	for len(collected) < q.size {
		if scanned >= joinedSearchMaxScan {
			hitMaxScan = true
			break
		}
		batch := min(joinedSearchBatchSize, joinedSearchMaxScan-scanned)
		rows, err := s.memberRepo.ListJoinedByRole(ctx, q.userID, q.role, pos, batch)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			exhausted = true
			break
		}
		hits, err := s.hydrateJoinedRows(ctx, rows, q.keyword, q.withStatus)
		if err != nil {
			return nil, err
		}
		if need := q.size - len(collected); len(hits) > need {
			hits = hits[:need]
			last := hits[need-1].key
			pos = &last
			collected = append(collected, hitDocs(hits)...)
			break
		}
		collected = append(collected, hitDocs(hits)...)
		last := joinedKeyOf(rows[len(rows)-1])
		pos = &last
		scanned += len(rows)
		if len(rows) < batch {
			exhausted = true
			break
		}
	}

	next := ""
	if !exhausted && pos != nil {
		next = encodeJoinedRoleKey(q.role, *pos)
	}
	return &MyCircleSearchResult{
		Circles: collected, Total: 0, Size: q.size, SearchAfter: next,
		Truncated: hitMaxScan && !exhausted,
	}, nil
}

// joinedHit 角色筛选模式的一条命中：回填后的列表项 + 对应成员行的 keyset 位置。
type joinedHit struct {
	doc MyCircleDoc
	key domain.JoinedKey
}

// hitDocs 取命中的列表项（保证非 nil，JSON 为 [] 而非 null）。
func hitDocs(hits []joinedHit) []MyCircleDoc {
	docs := make([]MyCircleDoc, 0, len(hits))
	for _, h := range hits {
		docs = append(docs, h.doc)
	}
	return docs
}

// hydrateJoinedRows 把一批成员行交给 ES SearchMy 过滤（status=1/deleted=0、keyword）并回填展示字段，
// 再按成员行顺序（加入时间倒序）输出，role / 成员状态直接取自成员行，无需再查库。
func (s *circleServiceImpl) hydrateJoinedRows(ctx context.Context, rows []domain.CircleMember, keyword string, withStatus bool) ([]joinedHit, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.CircleID)
	}
	res, err := s.searcher.SearchMy(ctx, ids, keyword, len(ids), nil)
	if err != nil {
		return nil, err
	}
	docByID := make(map[string]MyCircleDoc, len(res.Circles))
	for _, d := range res.Circles {
		docByID[d.ID] = d
	}
	now := time.Now()
	hits := make([]joinedHit, 0, len(res.Circles))
	for _, m := range rows {
		d, ok := docByID[m.CircleID.String()]
		if !ok {
			continue // ES 过滤掉（圈子非正常 / 已删除 / 未命中关键字）
		}
		hits = append(hits, joinedHit{doc: withMembership(d, m, withStatus, now), key: joinedKeyOf(m)})
	}
	return hits, nil
}

// fillJoinedMembership 无筛选模式的成员关系回填：按页一次批量查 circle_member（不做 N+1）。
// 成员关系已不在「已加入」口径内的圈子（ZSET 残留：退出 / 拉黑）从结果剔除。
func (s *circleServiceImpl) fillJoinedMembership(ctx context.Context, q joinedListQuery, docs []MyCircleDoc) ([]MyCircleDoc, error) {
	out := make([]MyCircleDoc, 0, len(docs))
	if len(docs) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(docs))
	for _, d := range docs {
		if id, err := uuid.Parse(d.ID); err == nil {
			ids = append(ids, id)
		}
	}
	members, err := s.memberRepo.ListJoinedMemberships(ctx, q.userID, ids)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, d := range docs {
		id, err := uuid.Parse(d.ID)
		if err != nil {
			continue
		}
		m, ok := members[id]
		if !ok {
			continue
		}
		out = append(out, withMembership(d, m, q.withStatus, now))
	}
	return out, nil
}

// withMembership 回填角色；withStatus 时回填成员状态与禁言截止时间。
// 禁言生效以 status=禁言 且 mute_end_time > now 为准（与圈内发帖禁言校验一致），
// 过期禁言按正常返回且不带截止时间。
func withMembership(d MyCircleDoc, m domain.CircleMember, withStatus bool, now time.Time) MyCircleDoc {
	d.Role = m.Role
	if !withStatus {
		return d
	}
	d.MemberStatus = domain.MemberStatusNormal
	d.MuteEndTime = nil
	if m.Status == domain.MemberStatusMuted && m.MuteEndTime != nil && m.MuteEndTime.After(now) {
		end := *m.MuteEndTime
		d.MemberStatus = domain.MemberStatusMuted
		d.MuteEndTime = &end
	}
	return d
}

// joinedKeyOf 成员行的 keyset 位置。
func joinedKeyOf(m domain.CircleMember) domain.JoinedKey {
	return domain.JoinedKey{CreateTime: m.CreateTime, CircleID: m.CircleID}
}

// ===== 游标 =====
//
// 两种游标都是 opaque base64url(JSON)：
//   - rank 游标（无筛选，ZSET 偏移）：{"r":40}
//   - 角色游标（PG keyset）：{"k":"role","role":20,"t":<create_time UnixMicro>,"c":"<circle_id>"}
//
// 靠 k 区分；游标与请求的 role 不一致（含 rank/角色混用、换了角色）一律判非法。

// joinedCursorKindRole 角色游标的 k 值。
const joinedCursorKindRole = "role"

// joinedCursor 解析后的游标。role=0 表示 rank 游标。
type joinedCursor struct {
	rank int
	role int16
	key  *domain.JoinedKey
}

// encodeJoinedCursor 把 rank 编码为 opaque base64url 游标串。
func encodeJoinedCursor(rank int) string {
	b, _ := json.Marshal(struct {
		R int `json:"r"`
	}{R: rank})
	return base64.RawURLEncoding.EncodeToString(b)
}

// encodeJoinedRoleCursor 以成员行位置编码角色游标。
func encodeJoinedRoleCursor(role int16, m domain.CircleMember) string {
	return encodeJoinedRoleKey(role, joinedKeyOf(m))
}

// encodeJoinedRoleKey 编码角色游标。时间用 UnixMicro：timestamptz 微秒精度，续页比较精确。
func encodeJoinedRoleKey(role int16, k domain.JoinedKey) string {
	b, _ := json.Marshal(struct {
		K    string `json:"k"`
		Role int16  `json:"role"`
		T    int64  `json:"t"`
		C    string `json:"c"`
	}{K: joinedCursorKindRole, Role: role, T: k.CreateTime.UnixMicro(), C: k.CircleID.String()})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeJoinedCursor 解析游标。空串 → rank 0。非法返回错误（用户可控参数，绝不 panic）。
func decodeJoinedCursor(s string) (joinedCursor, error) {
	if s == "" {
		return joinedCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return joinedCursor{}, err
	}
	var c struct {
		K    string `json:"k"`
		R    int    `json:"r"`
		Role int16  `json:"role"`
		T    *int64 `json:"t"`
		C    string `json:"c"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return joinedCursor{}, err
	}
	switch c.K {
	case "":
		if c.R < 0 {
			return joinedCursor{}, fmt.Errorf("negative cursor rank")
		}
		return joinedCursor{rank: c.R}, nil
	case joinedCursorKindRole:
		if !isMemberRole(c.Role) || c.T == nil {
			return joinedCursor{}, fmt.Errorf("bad role cursor")
		}
		id, err := uuid.Parse(c.C)
		if err != nil {
			return joinedCursor{}, fmt.Errorf("bad role cursor circle id")
		}
		return joinedCursor{role: c.Role, key: &domain.JoinedKey{CreateTime: time.UnixMicro(*c.T), CircleID: id}}, nil
	}
	return joinedCursor{}, fmt.Errorf("unknown cursor kind %q", c.K)
}
