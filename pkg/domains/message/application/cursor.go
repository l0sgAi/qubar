package application

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"interestBar/pkg/domains/message/domain"

	"github.com/google/uuid"
)

// cursorVersion 当前产出的游标版本。
const cursorVersion = 1

// 游标所属的列表。取值会写进游标（作用域校验），不要改已有值。
const (
	listConversations = "conversations"
	listMessages      = "messages"
)

// pageCursor 两种列表 keyset 游标的线上格式：URL 安全的 base64(JSON)，对前端是不透明字符串。
//
//	{"v":1,"list":"messages","scope_id":"<会话ID>","id":"<上一页最后一条消息ID>"}
//
// list + scope_id 是游标所属的列表（会话列表的 scope_id 是当前用户，消息历史的是会话）：解析时必须与本次请求一致，
// 防止把 A 的游标用在 B 的列表上（做法同关注列表游标）。id 是消息 ID（UUIDv7 即时间序），单列即可唯一定序：
// 会话列表按会话最后一条消息的 ID 排序，消息历史按消息 ID 排序。
type pageCursor struct {
	V       int    `json:"v"`
	List    string `json:"list"`
	ScopeID string `json:"scope_id"`
	ID      string `json:"id"`
}

func encodeCursor(list string, scopeID, lastID uuid.UUID) string {
	data, _ := json.Marshal(pageCursor{V: cursorVersion, List: list, ScopeID: scopeID.String(), ID: lastID.String()})
	return base64.RawURLEncoding.EncodeToString(data)
}

// parseCursor 解码并校验游标。空串表示第一页，返回 nil。
// 游标来自用户可控的 query 参数：任何不合法情况都返回包装 domain.ErrInvalidCursor 的错误，绝不 panic。
func parseCursor(cursor, list string, scopeID uuid.UUID) (*uuid.UUID, error) {
	if cursor == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, invalidCursor("decode failed: " + err.Error())
	}
	var c pageCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, invalidCursor("decode failed: " + err.Error())
	}
	if c.V != cursorVersion {
		return nil, invalidCursor(fmt.Sprintf("unsupported version %d", c.V))
	}
	if c.List != list {
		return nil, invalidCursor("list mismatch")
	}
	if scope, err := uuid.Parse(c.ScopeID); err != nil || scope != scopeID {
		return nil, invalidCursor("scope mismatch")
	}
	id, err := uuid.Parse(c.ID)
	if err != nil || id == uuid.Nil {
		return nil, invalidCursor("missing or invalid keyset")
	}
	return &id, nil
}

// invalidCursor 包装 domain.ErrInvalidCursor；reason 供排查用，handler 不把它回给客户端。
func invalidCursor(reason string) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalidCursor, reason)
}
