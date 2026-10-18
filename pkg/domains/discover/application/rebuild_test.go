package application

import (
	"testing"

	"github.com/google/uuid"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/discover/domain"
)

// parseUUIDs 应跳过非法字符串、保留合法 uuid、保持顺序。
func TestParseUUIDs(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	raw := []string{id1.String(), "not-a-uuid", id2.String(), ""}
	got := parseUUIDs(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(got))
	}
	if got[0] != id1 || got[1] != id2 {
		t.Fatalf("order/ids mismatch: got %v want [%s %s]", got, id1, id2)
	}
}

// parseUUIDs 空/全非法输入返回空切片（非 nil 引用时安全）。
func TestParseUUIDsEmpty(t *testing.T) {
	if got := parseUUIDs(nil); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	if got := parseUUIDs([]string{"bad", ""}); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// toUUIDSet 合并多列表并去重。
func TestToUUIDSet(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	// id2 在多个列表重复出现 → set 只应有一个键。
	set := toUUIDSet([]uuid.UUID{id1}, []uuid.UUID{id2, id2}, nil)
	if len(set) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(set))
	}
	if _, ok := set[id1]; !ok {
		t.Errorf("missing id1")
	}
	if _, ok := set[id2]; !ok {
		t.Errorf("missing id2")
	}
}

// filterOutUUIDs 保序剔除 exclude 集合内的元素。
func TestFilterOutUUIDs(t *testing.T) {
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()
	ids := []uuid.UUID{id1, id2, id3}
	exclude := map[uuid.UUID]struct{}{id2: {}}
	got := filterOutUUIDs(ids, exclude)
	if len(got) != 2 {
		t.Fatalf("expected 2 ids after filter, got %d", len(got))
	}
	if got[0] != id1 || got[1] != id3 {
		t.Fatalf("order mismatch: got %v want [%s %s]", got, id1, id3)
	}
}

// filterOutUUIDs 空 exclude → 全保留（保序）。
func TestFilterOutUUIDsEmptyExclude(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	ids := []uuid.UUID{id1, id2}
	got := filterOutUUIDs(ids, nil)
	if len(got) != 2 || got[0] != id1 || got[1] != id2 {
		t.Fatalf("expected all preserved in order, got %v", got)
	}
}

// defaultInt v<=0 返回 def；v>0 返回 v。
func TestDefaultInt(t *testing.T) {
	cases := []struct{ v, def, want int }{
		{0, 30, 30},
		{-5, 30, 30},
		{200, 30, 200},
		{1, 30, 1},
	}
	for _, c := range cases {
		if got := defaultInt(c.v, c.def); got != c.want {
			t.Errorf("defaultInt(%d,%d)=%d want %d", c.v, c.def, got, c.want)
		}
	}
}

// normalizeSection 非法值回落 all；合法值原样返回。
func TestNormalizeSection(t *testing.T) {
	cases := map[string]string{
		"":        domain.SectionAll,
		"all":     domain.SectionAll,
		"posts":   domain.SectionPosts,
		"circles": domain.SectionCircles,
		"invalid": domain.SectionAll,
		"POSTS":   domain.SectionAll, // 大小写敏感
	}
	for in, want := range cases {
		if got := normalizeSection(in); got != want {
			t.Errorf("normalizeSection(%q)=%q want %q", in, got, want)
		}
	}
}

// normalizeSize <=0 或超 max 回落默认；max/default 来自配置，<=0 时回落常量 50/20。
func TestNormalizeSize(t *testing.T) {
	// 配置未初始化时（零值）应回落常量：max=50, default=20。
	conf.Config = &conf.AppConfig{} // 零值配置（max/default 均 0 → 回落 50/20）
	cases := []struct {
		name string
		size int
		want int
	}{
		{"zero", 0, 20},
		{"negative", -1, 20},
		{"over max", 51, 20},
		{"valid", 30, 30},
		{"max boundary", 50, 50},
		{"min positive", 1, 1},
	}
	for _, c := range cases {
		if got := normalizeSize(c.size); got != c.want {
			t.Errorf("%s: normalizeSize(%d)=%d want %d", c.name, c.size, got, c.want)
		}
	}
}
