package elasticsearch

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// feedQueryBody 是 homeFeedQuery 序列化后（即真正发给 ES 的 JSON）的结构。
type feedQueryBody struct {
	Query struct {
		Bool struct {
			Must               []map[string]map[string]interface{} `json:"must"`
			Should             []map[string]map[string][]string    `json:"should"`
			MinimumShouldMatch *int                                `json:"minimum_should_match"`
			MustNot            []map[string]map[string][]string    `json:"must_not"`
		} `json:"bool"`
	} `json:"query"`
	Size            int                               `json:"size"`
	Sort            []map[string]map[string]string    `json:"sort"`
	SearchAfter     []interface{}                     `json:"search_after"`
	RuntimeMappings map[string]map[string]interface{} `json:"runtime_mappings"`
}

func marshalFeedQuery(t *testing.T, q map[string]interface{}) (feedQueryBody, string) {
	t.Helper()
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body feedQueryBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return body, string(raw)
}

// termsOf 在 must 子句里找指定字段的 terms 值；没有返回 nil。
func termsOf(must []map[string]map[string]interface{}, field string) []string {
	for _, clause := range must {
		vals, ok := clause["terms"][field].([]interface{})
		if !ok {
			continue
		}
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			out = append(out, v.(string))
		}
		return out
	}
	return nil
}

// assertPublishedOnly 断言基础过滤：deleted=0 且 status=1（与 latest 流一致）。
func assertPublishedOnly(t *testing.T, body feedQueryBody, raw string) {
	t.Helper()
	var deleted, status bool
	for _, clause := range body.Query.Bool.Must {
		if v, ok := clause["term"]["deleted"].(float64); ok && v == 0 {
			deleted = true
		}
		if v, ok := clause["term"]["status"].(float64); ok && v == 1 {
			status = true
		}
	}
	if !deleted || !status {
		t.Fatalf("missing deleted=0 / status=1 filter: %s", raw)
	}
}

func assertLatestSort(t *testing.T, body feedQueryBody, raw string) {
	t.Helper()
	want := []map[string]map[string]string{{"create_time": {"order": "desc"}}, {"id": {"order": "desc"}}}
	if !reflect.DeepEqual(body.Sort, want) || body.RuntimeMappings != nil {
		t.Fatalf("latest sort must be create_time desc, id desc without runtime mappings: %s", raw)
	}
}

// 关注流：圈子与作者都有 → should 两个 terms + minimum_should_match=1，私密圈走 must_not。
func TestHomeFeedQuery_CirclesOrAuthors(t *testing.T) {
	circles := []uuid.UUID{uuid.New(), uuid.New()}
	authors := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	hidden := []uuid.UUID{uuid.New()}
	cursor := []interface{}{"2026-11-10T08:00:00Z", "0193-post"}

	body, raw := marshalFeedQuery(t, homeFeedQuery("latest", circles, authors, hidden, 15, cursor))

	assertPublishedOnly(t, body, raw)
	assertLatestSort(t, body, raw)
	if len(body.Query.Bool.Must) != 2 {
		t.Fatalf("scope must not leak into must when both lists are set: %s", raw)
	}
	b := body.Query.Bool
	if len(b.Should) != 2 || b.MinimumShouldMatch == nil || *b.MinimumShouldMatch != 1 {
		t.Fatalf("want 2 should clauses with minimum_should_match=1: %s", raw)
	}
	if !reflect.DeepEqual(b.Should[0]["terms"]["circle_id"], uuidStrings(circles)) ||
		!reflect.DeepEqual(b.Should[1]["terms"]["user_id"], uuidStrings(authors)) {
		t.Fatalf("should clauses must be terms circle_id / terms user_id: %s", raw)
	}
	if len(b.MustNot) != 1 || !reflect.DeepEqual(b.MustNot[0]["terms"]["circle_id"], uuidStrings(hidden)) {
		t.Fatalf("hidden circles must be excluded by must_not: %s", raw)
	}
	if body.Size != 15 || !reflect.DeepEqual(body.SearchAfter, cursor) {
		t.Fatalf("size / search_after not passed through: %s", raw)
	}
}

// 只有一种范围时对应的 terms 直接进 must，不产生 should（否则 should 在有 must 时不再是必要条件）。
func TestHomeFeedQuery_SingleScope(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	hidden := []uuid.UUID{uuid.New()}
	cases := []struct {
		name             string
		circles, authors []uuid.UUID
		field, absent    string
	}{
		{"only circles", ids, nil, "circle_id", "user_id"},
		{"only authors", nil, ids, "user_id", "circle_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, raw := marshalFeedQuery(t, homeFeedQuery("latest", tc.circles, tc.authors, hidden, 20, nil))
			assertPublishedOnly(t, body, raw)
			assertLatestSort(t, body, raw)
			b := body.Query.Bool
			if len(b.Should) != 0 || b.MinimumShouldMatch != nil {
				t.Fatalf("single scope must not use should: %s", raw)
			}
			if !reflect.DeepEqual(termsOf(b.Must, tc.field), uuidStrings(ids)) || termsOf(b.Must, tc.absent) != nil {
				t.Fatalf("want must terms %s only: %s", tc.field, raw)
			}
			// 只关注了人、没加圈子时，私密圈同样要排除。
			if len(b.MustNot) != 1 || !reflect.DeepEqual(b.MustNot[0]["terms"]["circle_id"], uuidStrings(hidden)) {
				t.Fatalf("hidden circles must be excluded by must_not: %s", raw)
			}
			if body.SearchAfter != nil {
				t.Fatalf("first page must not carry search_after: %s", raw)
			}
		})
	}
}

// 抽出 homeFeedQuery 后，hot / latest（SearchHomeFeed 的调用形态）的查询体与重构前逐字节一致。
func TestHomeFeedQuery_MatchesLegacyBody(t *testing.T) {
	circle, hidden := uuid.New(), uuid.New()
	latestSort := `"sort":[{"create_time":{"order":"desc"}},{"id":{"order":"desc"}}]`
	base := `{"term":{"deleted":0}},{"term":{"status":1}}`

	got, _ := json.Marshal(homeFeedQuery("latest", nil, nil, nil, 20, nil))
	if want := `{"query":{"bool":{"must":[` + base + `]}},"size":20,` + latestSort + `}`; string(got) != want {
		t.Fatalf("global latest:\n got %s\nwant %s", got, want)
	}

	got, _ = json.Marshal(homeFeedQuery("latest", []uuid.UUID{circle}, nil, []uuid.UUID{hidden}, 10, []interface{}{"t", "id"}))
	want := `{"query":{"bool":{"must":[` + base + `,{"terms":{"circle_id":["` + circle.String() + `"]}}],` +
		`"must_not":[{"terms":{"circle_id":["` + hidden.String() + `"]}}]}},"search_after":["t","id"],"size":10,` + latestSort + `}`
	if string(got) != want {
		t.Fatalf("circle latest:\n got %s\nwant %s", got, want)
	}

	body, raw := marshalFeedQuery(t, homeFeedQuery("hot", nil, nil, nil, 20, nil))
	assertPublishedOnly(t, body, raw)
	if body.RuntimeMappings["rank_score"] == nil || len(body.Sort) != 2 || body.Sort[0]["rank_score"]["order"] != "desc" {
		t.Fatalf("hot must sort by runtime rank_score desc: %s", raw)
	}
}

// 范围为空的关注流直接返回空结果，不查 ES（Client 为 nil，真去查会 panic），也不会退化成全局最新流。
func TestSearchFollowingFeed_EmptyScope(t *testing.T) {
	res, err := searchFollowingFeedInIndex(context.Background(), "unused", nil, []uuid.UUID{}, []uuid.UUID{uuid.New()}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Posts) != 0 || res.SearchAfter != nil || res.Size != 20 {
		t.Fatalf("want empty page with normalized size, got %+v", res)
	}
}
