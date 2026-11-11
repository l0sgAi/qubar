package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestOrderPair(t *testing.T) {
	a := uuid.MustParse("018f0000-0000-7000-8000-000000000001")
	b := uuid.MustParse("018f0000-0000-7000-8000-000000000002")
	for _, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		low, high := OrderPair(pair[0], pair[1])
		if low != a || high != b {
			t.Errorf("OrderPair(%s, %s) = (%s, %s), want (%s, %s)", pair[0], pair[1], low, high, a, b)
		}
	}
	// 契约值写进库，不能改。
	if ConversationTypeDirect != 1 || ConversationTypeGroup != 2 || MessageTypeText != 1 {
		t.Fatal("type contract values changed")
	}
}
