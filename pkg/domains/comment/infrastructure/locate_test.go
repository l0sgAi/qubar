// locate_test.go 评论定位算法的单元测试。
//
// 核心防线：locatePage 的页码/页起始位次计算 off-by-one 边界
// （设计文档 §5.3 边界表）；以及 rank 条件与游标条件排序键的一致性。
package infrastructure

import (
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// TestLocatePage 页码与页起始位次的边界表（验收标准 #6）。
//
// page 从 1 开始；k 为页起始游标对应条目位次（上一页末条，1-based），
// page=1 时 k=0 表示无需游标（首页）。
func TestLocatePage(t *testing.T) {
	cases := []struct {
		name            string
		before          int64 // 严格排在目标之前的条数（rank-1）
		size            int
		wantPage, wantK int64
	}{
		// 顶层页大小 20
		{"首页首条", 0, 20, 1, 0},
		{"首页末条(rank=20)", 19, 20, 1, 0},
		{"次页首条(rank=21)", 20, 20, 2, 20},
		{"次页中间(rank=30)", 29, 20, 2, 20},
		{"次页末条(rank=40)", 39, 20, 2, 20},
		{"第三页首条(rank=41)", 40, 20, 3, 40},
		{"第三页末条(rank=60)", 59, 20, 3, 40},
		// 回复页大小 10
		{"回复首页末条(rank=10)", 9, 10, 1, 0},
		{"回复次页首条(rank=11)", 10, 10, 2, 10},
		{"回复次页次条(rank=12)", 11, 10, 2, 10},
		{"回复第三页首条(rank=21)", 20, 10, 3, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, k := locatePage(tc.before, tc.size)
			if page != tc.wantPage || k != tc.wantK {
				t.Fatalf("locatePage(before=%d, size=%d) = (%d, %d), want (%d, %d)",
					tc.before, tc.size, page, k, tc.wantPage, tc.wantK)
			}
		})
	}
}

// TestLocateCursor_OutputFormat 定位的页起始游标与列表的 next_cursor 逐字节一致：
// 两者是同一个 encodeListCursor(scope.id, sort, 上一页末条)，这里固定住「同一作用域 + 同一末条 → 同一字符串」，
// 并验证它能被列表侧的 parseListCursor 在同一作用域下读回（真实 SQL 路径见集成测试
// TestCommentRepo_LocateCursorMatchesListPages）。
func TestLocateCursor_OutputFormat(t *testing.T) {
	postID := uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b0")
	item := &domain.Comment{
		ID:        uuid.MustParse("0192a0d0-0000-7000-8000-000000000042"),
		LikeCount: 7,
	}
	scope := rootListScope(postID)
	if scope.id != postID || replyListScope(item.ID).id != item.ID {
		t.Fatalf("list scope id must be the post id (root list) / root comment id (replies)")
	}

	for _, sort := range []int{sortByLike, sortByTime} {
		cursor := encodeListCursor(scope.id, sort, item)
		if again := encodeListCursor(scope.id, sort, item); again != cursor {
			t.Fatalf("sort=%d: cursor encoding is not deterministic: %q vs %q", sort, cursor, again)
		}
		likeCount, id, err := parseListCursor(cursor, scope.id, sort)
		if err != nil || id != item.ID || (sort == sortByLike && likeCount != 7) {
			t.Fatalf("sort=%d cursor round-trip failed: likeCount=%d id=%v err=%v", sort, likeCount, id, err)
		}
	}
}
