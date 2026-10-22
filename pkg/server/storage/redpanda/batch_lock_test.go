package redpanda

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestSortByIDMatchesPostgresUUIDOrder(t *testing.T) {
	ids := []uuid.UUID{
		uuid.MustParse("ffffffff-0000-0000-0000-000000000000"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("0000000a-0000-0000-0000-000000000000"),
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
	}
	type row struct{ ID uuid.UUID }
	rows := make([]row, len(ids))
	for i, id := range ids {
		rows[i] = row{ID: id}
	}
	sortByID(rows, func(r row) uuid.UUID { return r.ID })

	// PostgreSQL uuid 按字节序比较，与规范文本的字典序一致
	want := []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"0000000a-0000-0000-0000-000000000000",
		"ffffffff-0000-0000-0000-000000000000",
	}
	for i, r := range rows {
		if r.ID.String() != want[i] {
			t.Fatalf("rows[%d] = %s, want %s", i, r.ID, want[i])
		}
	}
}

func TestSortedIDsDedupes(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got := sortedIDs([]uuid.UUID{b, a, b, a})
	want := []uuid.UUID{a, b}
	if compareID(a, b) > 0 {
		want = []uuid.UUID{b, a}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sortedIDs = %v, want %v", got, want)
	}
}
