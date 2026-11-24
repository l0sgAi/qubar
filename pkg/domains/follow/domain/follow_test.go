package domain

import "testing"

func TestRelationOf(t *testing.T) {
	for _, tc := range []struct {
		following, followedBy bool
		want                  Relation
	}{
		{false, false, RelationNone},
		{true, false, RelationFollowing},
		{false, true, RelationFollowedBy},
		{true, true, RelationMutual},
	} {
		if got := RelationOf(tc.following, tc.followedBy); got != tc.want {
			t.Errorf("RelationOf(%v, %v) = %d, want %d", tc.following, tc.followedBy, got, tc.want)
		}
	}
	// 契约值固定为 0/1/2/3（前端按数值渲染）。
	if RelationNone != 0 || RelationFollowing != 1 || RelationFollowedBy != 2 || RelationMutual != 3 {
		t.Fatal("relation contract values changed")
	}
}
