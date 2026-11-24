package infrastructure

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// 评论列表排序（与 handler 的 sort 参数取值一致）。
const (
	sortByLike = 0 // 点赞倒序：keyset (like_count, id)
	sortByTime = 1 // 时间倒序：keyset id（UUIDv7 字典序 == 时间序）
)

// listCursorVersion 当前产出的游标版本。
const listCursorVersion = 2

// listCursor 评论列表 keyset 游标的线上格式：base64(JSON)，对前端是不透明字符串。
//
//	v2：{"v":2,"sort":0,"scope_id":"<帖子ID 或 根评论ID>","like_count":7,"id":"<上一页末条ID>"}
//	v1：{"id":"<上一页末条ID>","like_count":7}（没有 v / sort / scope_id；只解析，不再产出）
//
// scope_id 是游标所属列表：顶层列表为帖子 ID，楼内回复为根评论 ID。解析时校验 sort 与 scope_id
// 和本次请求一致，防止把 A 帖 / A 楼或另一种排序下的游标拿到别处用（#209）。
// like_count 只在 sort=0 时出现。字段顺序即编码顺序，列表与定位共用 encodeListCursor，逐字节一致。
// keyset 键名（like_count / id）与 v1 相同，换排序键时升版本号即可平滑过渡。
type listCursor struct {
	V         int    `json:"v,omitempty"`
	Sort      *int   `json:"sort,omitempty"`
	ScopeID   string `json:"scope_id,omitempty"`
	LikeCount *int64 `json:"like_count,omitempty"`
	ID        string `json:"id"`
}

// encodeListCursor 以 last（一页的末条）为 keyset 构建 v2 游标。
//
// 列表的 next_cursor 与定位接口的页起始游标都由它产出：同一 (scopeID, sort, last) 的结果逐字节相同。
// 未知 sort 返回 ""（handler 已限定 0 / 1）。
func encodeListCursor(scopeID uuid.UUID, sort int, last *domain.Comment) string {
	c := listCursor{V: listCursorVersion, Sort: &sort, ScopeID: scopeID.String(), ID: last.ID.String()}
	switch sort {
	case sortByLike:
		likeCount := int64(last.LikeCount)
		c.LikeCount = &likeCount
	case sortByTime:
	default:
		return ""
	}
	data, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(data)
}

// parseListCursor 解码并校验游标，返回 keyset 键 (likeCount, id)。
//
// 游标来自用户可控的 query 参数：任何解码失败、字段缺失 / 类型错误、版本未知，以及 v2 游标的
// sort / scope_id 与本次请求不一致，都返回包装 domain.ErrInvalidCursor 的错误（handler 映射 400），绝不 panic。
// v1 游标没有作用域信息，只按 sort 校验 keyset 字段（与 v2 上线前的行为一致）。
// sort=0 需要 like_count + id，sort=1 只需要 id。
func parseListCursor(cursor string, scopeID uuid.UUID, sort int) (likeCount int64, id uuid.UUID, err error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, uuid.Nil, invalidCursor("decode failed: " + err.Error())
	}
	var c listCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return 0, uuid.Nil, invalidCursor("decode failed: " + err.Error())
	}

	switch c.V {
	case 0: // v1：没有 v 字段，也没有作用域信息
	case listCursorVersion:
		if c.Sort == nil || *c.Sort != sort {
			return 0, uuid.Nil, invalidCursor("sort mismatch")
		}
		if cursorScope, err := uuid.Parse(c.ScopeID); err != nil || cursorScope != scopeID {
			return 0, uuid.Nil, invalidCursor("scope mismatch")
		}
	default:
		return 0, uuid.Nil, invalidCursor(fmt.Sprintf("unsupported version %d", c.V))
	}

	id, err = uuid.Parse(c.ID)
	if err != nil {
		return 0, uuid.Nil, invalidCursor("missing or invalid id")
	}
	if sort == sortByLike {
		if c.LikeCount == nil {
			return 0, uuid.Nil, invalidCursor("missing like_count")
		}
		likeCount = *c.LikeCount
	}
	return likeCount, id, nil
}

// invalidCursor 包装 domain.ErrInvalidCursor（errors.Is 可识别），reason 只进日志，不回给客户端。
func invalidCursor(reason string) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalidCursor, reason)
}
