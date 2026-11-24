package domain

import (
	"fmt"
	"testing"
)

// TestVisibleCondition_MatchesStatusConstant SQL 谓词字面量与状态常量同步（防漂移）。
func TestVisibleCondition_MatchesStatusConstant(t *testing.T) {
	want := fmt.Sprintf("deleted = 0 AND status = %d", CommentStatusNormal)
	if VisibleCondition != want {
		t.Fatalf("VisibleCondition = %q, want %q", VisibleCondition, want)
	}
}

func TestComment_Visible(t *testing.T) {
	cases := []struct {
		name    string
		deleted int16
		status  int16
		want    bool
	}{
		{"normal", 0, CommentStatusNormal, true},
		{"review", 0, CommentStatusReview, false},
		{"hidden", 0, CommentStatusHidden, false},
		{"deleted", 1, CommentStatusNormal, false},
		{"deleted hidden", 1, CommentStatusHidden, false},
		{"zero status", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Comment{Deleted: tc.deleted, Status: tc.status}
			if got := c.Visible(); got != tc.want {
				t.Fatalf("Visible() = %v, want %v", got, tc.want)
			}
		})
	}
	var nilComment *Comment
	if nilComment.Visible() {
		t.Fatal("nil comment must not be visible")
	}
}
