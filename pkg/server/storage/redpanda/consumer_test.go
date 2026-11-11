package redpanda

import (
	"strings"
	"testing"

	commentdomain "interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
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

// TestPostStatsBuf_ShareMarksDirty 分享数标记（#232）只置脏、不累加 value，合并后保留，且非空行才落库。
func TestPostStatsBuf_ShareMarksDirty(t *testing.T) {
	id := uuid.New()
	a := make(postStatsBuf)
	a.add(PostStatisticsMessage{Type: StatisticsTypePostShare, PostID: id, Value: 1})
	a.add(PostStatisticsMessage{Type: StatisticsTypePostShare, PostID: id, Value: 1})
	if d := a[id]; !d.ShareDirty || d.ViewCount != 0 || d.empty() {
		t.Fatalf("share message delta = %+v, want only ShareDirty", d)
	}

	b := make(postStatsBuf)
	b.add(PostStatisticsMessage{Type: StatisticsTypePostView, PostID: id, Value: 1})
	b.merge(a)
	if d := b[id]; !d.ShareDirty || d.ViewCount != 1 {
		t.Fatalf("merged delta = %+v, want ShareDirty and view +1", d)
	}
}
