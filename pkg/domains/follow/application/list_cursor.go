package application

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

// listCursorVersion 当前产出的游标版本。
const listCursorVersion = 1

// listCursor 关注列表 keyset 游标的线上格式：URL 安全的 base64(JSON)，对前端是不透明字符串。
//
//	{"v":1,"list":"following","scope_id":"<列表所属用户ID>","follow_time":1762766400123456,"id":"<上一页末条关系行ID>"}
//
// list + scope_id 是游标所属的列表：解析时必须与本次请求一致，防止把 A 的游标用在 B 的列表上，
// 或把关注列表的游标用在粉丝 / 互关列表上（做法同评论游标 v2，docs/comment-locate-design.md §12）。
// follow_time 是 Unix 微秒（PG timestamptz 的精度），与 id 一起构成 keyset 键。
type listCursor struct {
	V          int             `json:"v"`
	List       domain.ListKind `json:"list"`
	ScopeID    string          `json:"scope_id"`
	FollowTime int64           `json:"follow_time"`
	ID         string          `json:"id"`
}

// encodeListCursor 以 last（一页里最后读到的关系）为 keyset 构建游标。
func encodeListCursor(kind domain.ListKind, ownerID uuid.UUID, last domain.FollowEdge) string {
	data, _ := json.Marshal(listCursor{
		V:          listCursorVersion,
		List:       kind,
		ScopeID:    ownerID.String(),
		FollowTime: last.FollowTime.UnixMicro(),
		ID:         last.ID.String(),
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

// parseListCursor 解码并校验游标。空串表示第一页，返回 nil。
//
// 游标来自用户可控的 query 参数：解码失败、字段缺失 / 类型错误、版本未知、list / scope_id 与本次请求不一致，
// 都返回包装 domain.ErrInvalidCursor 的错误（handler 映射 400），绝不 panic。
func parseListCursor(cursor string, kind domain.ListKind, ownerID uuid.UUID) (*domain.FollowEdge, error) {
	if cursor == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, invalidCursor("decode failed: " + err.Error())
	}
	var c listCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, invalidCursor("decode failed: " + err.Error())
	}
	if c.V != listCursorVersion {
		return nil, invalidCursor(fmt.Sprintf("unsupported version %d", c.V))
	}
	if c.List != kind {
		return nil, invalidCursor("list mismatch")
	}
	if scope, err := uuid.Parse(c.ScopeID); err != nil || scope != ownerID {
		return nil, invalidCursor("scope mismatch")
	}
	id, err := uuid.Parse(c.ID)
	if err != nil || c.FollowTime <= 0 {
		return nil, invalidCursor("missing or invalid keyset")
	}
	return &domain.FollowEdge{ID: id, FollowTime: time.UnixMicro(c.FollowTime)}, nil
}

// invalidCursor 包装 domain.ErrInvalidCursor（errors.Is 可识别）；reason 供排查用，handler 不把它回给客户端。
func invalidCursor(reason string) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalidCursor, reason)
}
