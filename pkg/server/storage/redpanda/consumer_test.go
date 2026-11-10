package redpanda

import (
	"strings"
	"testing"

	commentdomain "interestBar/pkg/domains/comment/domain"
)

// TestPostStatsApplySQL_UsesCommentVisibleCondition 帖子评论数重算复用评论可见性谓词（#208），
// 不再手写 deleted / status 条件：谓词改动时这里自动跟随，手写条件会让本测试失败。
func TestPostStatsApplySQL_UsesCommentVisibleCondition(t *testing.T) {
	want := "WHERE c.post_id = v.post_id AND " + commentdomain.VisibleCondition + ")"
	if !strings.Contains(postStatsApplySQL, want) {
		t.Fatalf("postStatsApplySQL comment recount must use commentdomain.VisibleCondition, want fragment %q", want)
	}
	if n := strings.Count(postStatsApplySQL, "domains.comment"); n != 1 {
		t.Fatalf("postStatsApplySQL references domains.comment %d times, want 1 (the recount subquery)", n)
	}
}
