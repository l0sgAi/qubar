package elasticsearch

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestExcludeCircles(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	q := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{
			"must":     []map[string]interface{}{{"term": map[string]interface{}{"deleted": 0}}},
			"must_not": []map[string]interface{}{{"terms": map[string]interface{}{"circle_id": []string{"joined"}}}},
		}},
	}
	excludeCircles(q, nil)
	excludeCircles(q, []uuid.UUID{a, b})

	raw, _ := json.Marshal(q)
	var got struct {
		Query struct {
			Bool struct {
				MustNot []struct {
					Terms struct {
						CircleID []string `json:"circle_id"`
					} `json:"terms"`
				} `json:"must_not"`
			} `json:"bool"`
		} `json:"query"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	mn := got.Query.Bool.MustNot
	if len(mn) != 2 || len(mn[1].Terms.CircleID) != 2 || mn[1].Terms.CircleID[0] != a.String() {
		t.Fatalf("unexpected must_not: %s", raw)
	}
}
