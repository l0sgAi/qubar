// Package infrastructure 的游标编解码单元测试（#209 游标 v2）。
//
// 覆盖：
//   - v2 游标编码格式（字段与顺序固定，列表 / 定位逐字节一致的前提）与 round-trip；
//   - v1 游标（v2 上线前前端可能持有的旧游标）仍可解析；
//   - v2 游标跨帖 / 跨楼、sort 不一致被拒绝；
//   - 防御性解析：游标来自用户可控的 query 参数，任何字段缺失 / 类型错误 / 版本未知都返回
//     包装 ErrInvalidCursor 的错误，绝不 panic（早期实现用无 comma-ok 的类型断言，
//     缺 like_count 的游标可触发远程 panic）。
package infrastructure

import (
	"encoding/base64"
	"errors"
	"strconv"
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// 测试固定标识：两个帖子、帖 A 下的两个楼、一条评论。
var (
	curPostA = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a0")
	curPostB = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b0")
	curRootA = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a1")
	curRootB = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a2")
	curItem  = uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
)

// rawCursor 把 JSON 文本直接 base64 成游标（绕过 encodeListCursor，模拟旧游标与篡改）。
func rawCursor(jsonText string) string {
	return base64.StdEncoding.EncodeToString([]byte(jsonText))
}

// v1 游标：v2 上线前 buildNextCursor 的实际产物（map 编码，键按字典序）。
func v1LikeCursor(id uuid.UUID, likeCount int) string {
	return rawCursor(`{"id":"` + id.String() + `","like_count":` + strconv.Itoa(likeCount) + `}`)
}

func v1TimeCursor(id uuid.UUID) string {
	return rawCursor(`{"id":"` + id.String() + `"}`)
}

// assertInvalidCursor 断言解析失败且错误可被 errors.Is 识别为 ErrInvalidCursor（handler 据此回 400）。
func assertInvalidCursor(t *testing.T, cursor string, scopeID uuid.UUID, sort int) {
	t.Helper()
	_, _, err := parseListCursor(cursor, scopeID, sort)
	if err == nil {
		t.Fatalf("cursor %q (sort=%d): expected error, got nil", cursor, sort)
	}
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("cursor %q (sort=%d): error should wrap ErrInvalidCursor, got: %v", cursor, sort, err)
	}
}

// ===== v2 编码 =====

// TestEncodeListCursor_V2Format v2 游标的线上格式：字段与顺序固定。
// 列表 next_cursor 与定位游标都走 encodeListCursor，格式固定才谈得上逐字节一致。
func TestEncodeListCursor_V2Format(t *testing.T) {
	item := &domain.Comment{ID: curItem, LikeCount: 42}

	gotLike := encodeListCursor(curPostA, sortByLike, item)
	wantLike := rawCursor(`{"v":2,"sort":0,"scope_id":"` + curPostA.String() + `","like_count":42,"id":"` + curItem.String() + `"}`)
	if gotLike != wantLike {
		t.Fatalf("sort=0 cursor = %q, want %q", gotLike, wantLike)
	}

	gotTime := encodeListCursor(curRootA, sortByTime, item)
	wantTime := rawCursor(`{"v":2,"sort":1,"scope_id":"` + curRootA.String() + `","id":"` + curItem.String() + `"}`)
	if gotTime != wantTime {
		t.Fatalf("sort=1 cursor = %q, want %q", gotTime, wantTime)
	}

	// 零赞也必须带 like_count（sort=0 的 keyset 必填字段，不能被 omitempty 吃掉）
	zero := encodeListCursor(curPostA, sortByLike, &domain.Comment{ID: curItem})
	if likeCount, _, err := parseListCursor(zero, curPostA, sortByLike); err != nil || likeCount != 0 {
		t.Fatalf("zero-like cursor round-trip: likeCount=%d err=%v", likeCount, err)
	}
}

// TestListCursor_V2RoundTrip v2 游标在同一作用域、同一排序下可解析回 keyset。
func TestListCursor_V2RoundTrip(t *testing.T) {
	item := &domain.Comment{ID: curItem, LikeCount: 42}

	likeCount, id, err := parseListCursor(encodeListCursor(curPostA, sortByLike, item), curPostA, sortByLike)
	if err != nil || likeCount != 42 || id != curItem {
		t.Fatalf("sort=0 round-trip: likeCount=%d id=%v err=%v", likeCount, id, err)
	}

	_, id, err = parseListCursor(encodeListCursor(curRootA, sortByTime, item), curRootA, sortByTime)
	if err != nil || id != curItem {
		t.Fatalf("sort=1 round-trip: id=%v err=%v", id, err)
	}
}

// ===== v2 作用域 / 排序校验 =====

// TestParseListCursor_V2RejectsOtherScope 跨帖 / 跨楼 / 顶层与楼内混用的 v2 游标被拒绝。
func TestParseListCursor_V2RejectsOtherScope(t *testing.T) {
	item := &domain.Comment{ID: curItem, LikeCount: 3}
	cases := []struct {
		name               string
		cursorScope, scope uuid.UUID
	}{
		{"跨帖：帖 A 的顶层游标用于帖 B", curPostA, curPostB},
		{"跨楼：楼 A 的回复游标用于楼 B", curRootA, curRootB},
		{"顶层游标用于楼内回复", curPostA, curRootA},
		{"楼内回复游标用于顶层列表", curRootA, curPostA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, sort := range []int{sortByLike, sortByTime} {
				assertInvalidCursor(t, encodeListCursor(tc.cursorScope, sort, item), tc.scope, sort)
			}
		})
	}
}

// TestParseListCursor_V2RejectsOtherSort v2 游标只在生成它的 sort 下有效。
// 特别是 sort=0 的游标带 like_count + id，字段上足以满足 sort=1 的解析（v1 时会被静默接受并翻到错页）。
func TestParseListCursor_V2RejectsOtherSort(t *testing.T) {
	item := &domain.Comment{ID: curItem, LikeCount: 3}
	assertInvalidCursor(t, encodeListCursor(curPostA, sortByLike, item), curPostA, sortByTime)
	assertInvalidCursor(t, encodeListCursor(curPostA, sortByTime, item), curPostA, sortByLike)
}

// TestParseListCursor_V2Malformed v2 游标缺字段 / 版本未知。
func TestParseListCursor_V2Malformed(t *testing.T) {
	scope, id := curPostA.String(), curItem.String()
	cases := map[string]string{
		"缺 sort":          `{"v":2,"scope_id":"` + scope + `","id":"` + id + `"}`,
		"缺 scope_id":      `{"v":2,"sort":1,"id":"` + id + `"}`,
		"scope_id 非 UUID": `{"v":2,"sort":1,"scope_id":"nope","id":"` + id + `"}`,
		"缺 id":            `{"v":2,"sort":1,"scope_id":"` + scope + `"}`,
		"未知版本":            `{"v":3,"sort":1,"scope_id":"` + scope + `","id":"` + id + `"}`,
		"v 类型错误":          `{"v":"2","sort":1,"scope_id":"` + scope + `","id":"` + id + `"}`,
	}
	for name, jsonText := range cases {
		t.Run(name, func(t *testing.T) {
			assertInvalidCursor(t, rawCursor(jsonText), curPostA, sortByTime)
		})
	}

	// sort=0 的 v2 游标缺 like_count
	assertInvalidCursor(t,
		rawCursor(`{"v":2,"sort":0,"scope_id":"`+scope+`","id":"`+id+`"}`), curPostA, sortByLike)
}

// ===== v1 兼容 =====

// TestParseListCursor_V1StillAccepted v1 游标（无 v / sort / scope_id）仍可解析。
// v1 没有作用域信息，无法校验归属：任何 scope 下都按旧逻辑接受。
func TestParseListCursor_V1StillAccepted(t *testing.T) {
	for _, scope := range []uuid.UUID{curPostA, curPostB, curRootA} {
		likeCount, id, err := parseListCursor(v1LikeCursor(curItem, 42), scope, sortByLike)
		if err != nil || likeCount != 42 || id != curItem {
			t.Fatalf("v1 sort=0: likeCount=%d id=%v err=%v", likeCount, id, err)
		}
		_, id, err = parseListCursor(v1TimeCursor(curItem), scope, sortByTime)
		if err != nil || id != curItem {
			t.Fatalf("v1 sort=1: id=%v err=%v", id, err)
		}
	}

	// 旧行为保持：v1 的点赞序游标（带 like_count）在 sort=1 下只取 id
	if _, id, err := parseListCursor(v1LikeCursor(curItem, 42), curPostA, sortByTime); err != nil || id != curItem {
		t.Fatalf("v1 like cursor under sort=1: id=%v err=%v", id, err)
	}
}

// TestParseListCursor_V1MissingLikeCount_NoPanic 核心回归：sort=0 缺 like_count 必须返回 error 不 panic。
func TestParseListCursor_V1MissingLikeCount_NoPanic(t *testing.T) {
	assertInvalidCursor(t, v1TimeCursor(curItem), curPostA, sortByLike)
}

// TestParseListCursor_V1WrongLikeCountType like_count 类型错误（字符串 / 小数）必须返回 error 不 panic。
func TestParseListCursor_V1WrongLikeCountType(t *testing.T) {
	id := curItem.String()
	assertInvalidCursor(t, rawCursor(`{"id":"`+id+`","like_count":"not-a-number"}`), curPostA, sortByLike)
	assertInvalidCursor(t, rawCursor(`{"id":"`+id+`","like_count":1.5}`), curPostA, sortByLike)
}

// TestParseListCursor_V1MissingOrInvalidID 缺 id / id 不是合法 UUID 必须返回 error。
func TestParseListCursor_V1MissingOrInvalidID(t *testing.T) {
	for _, sort := range []int{sortByLike, sortByTime} {
		assertInvalidCursor(t, rawCursor(`{"like_count":42}`), curPostA, sort)
		assertInvalidCursor(t, rawCursor(`{"id":"not-a-uuid","like_count":42}`), curPostA, sort)
		assertInvalidCursor(t, rawCursor(`{"id":7,"like_count":42}`), curPostA, sort)
	}
}

// ===== 损坏输入 =====

// TestParseListCursor_Malformed 非 base64 / 非 JSON / 非对象 / 空串必须返回 error 不 panic。
// （空串在 applyCursorCondition 已短路为首页；parseListCursor 是纯解析函数，这里验证它自身的行为。）
func TestParseListCursor_Malformed(t *testing.T) {
	badCursors := []string{
		"!!!not-base64!!!",
		rawCursor("not-json"),
		rawCursor("null"),
		rawCursor("[]"),
		rawCursor(`"str"`),
		"",
	}
	for _, c := range badCursors {
		for _, sort := range []int{sortByLike, sortByTime} {
			assertInvalidCursor(t, c, curPostA, sort)
		}
	}
}
