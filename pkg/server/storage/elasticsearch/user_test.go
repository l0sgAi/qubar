// user_test.go 用户搜索 DSL 构建的 @提及 圈子作用域过滤单元测试。
//
// 覆盖设计文档 P0-8 的"ES 过滤两路"：全站（circleID=Nil）排除所有圈内机器人
// （must_not exists）、圈内（circleID 非 Nil）本圈机器人可见（should: 非圈内机器人
// 或 agent_circle_id=本圈）。关键字/无关键字两分支均验证；排序与分页结构不受影响。
package elasticsearch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var testCircleID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000cc")

// mustConditionsOf 取 bool.query.must 条件数组。
func mustConditionsOf(t *testing.T, q map[string]interface{}) []interface{} {
	t.Helper()
	query := q["query"].(map[string]interface{})
	boolQ := query["bool"].(map[string]interface{})
	must, ok := boolQ["must"].([]map[string]interface{})
	if !ok {
		t.Fatal("query.bool.must missing or wrong type")
	}
	out := make([]interface{}, len(must))
	for i, m := range must {
		out[i] = m
	}
	return out
}

// findScopeCondition 在 must 条件中定位 agent_circle_id 作用域条件（不存在返回 nil）。
// 关键字分支的首个 must 是关键词召回 bool（也含 should），不能按结构区分，
// 以"序列化 JSON 含 agent_circle_id 字段名"为唯一判据。
func findScopeCondition(t *testing.T, must []interface{}) map[string]interface{} {
	t.Helper()
	for _, c := range must {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal condition: %v", err)
		}
		if strings.Contains(string(raw), "agent_circle_id") {
			return m
		}
	}
	return nil
}

// TestBuildUserSearchQuery_SiteWideExcludesCircleAgents 全站场景：必须排除所有圈内机器人。
func TestBuildUserSearchQuery_SiteWideExcludesCircleAgents(t *testing.T) {
	for _, kw := range []string{"", "小明"} {
		q := buildUserSearchQuery(kw, 20, uuid.Nil)
		scope := findScopeCondition(t, mustConditionsOf(t, q))
		if scope == nil {
			t.Fatalf("keyword=%q: agent_circle_id scope condition missing", kw)
		}
		boolQ := scope["bool"].(map[string]interface{})
		mustNot, ok := boolQ["must_not"].(map[string]interface{})
		if !ok {
			t.Fatalf("keyword=%q: want must_not clause, got %v", kw, scope)
		}
		exists := mustNot["exists"].(map[string]interface{})
		if exists["field"] != "agent_circle_id" {
			t.Fatalf("keyword=%q: exists field = %v, want agent_circle_id", kw, exists["field"])
		}
	}
}

// TestBuildUserSearchQuery_InCircleKeepsOwnAgents 圈内场景：普通用户/全局机器人/本圈机器人
// 三类可见（should: must_not exists OR term 本圈），他圈机器人被排除。
func TestBuildUserSearchQuery_InCircleKeepsOwnAgents(t *testing.T) {
	for _, kw := range []string{"", "机器人"} {
		q := buildUserSearchQuery(kw, 20, testCircleID)
		scope := findScopeCondition(t, mustConditionsOf(t, q))
		if scope == nil {
			t.Fatalf("keyword=%q: scope condition missing", kw)
		}
		boolQ := scope["bool"].(map[string]interface{})
		should, ok := boolQ["should"].([]map[string]interface{})
		if !ok || len(should) != 2 {
			t.Fatalf("keyword=%q: want 2 should clauses, got %v", kw, scope)
		}
		if boolQ["minimum_should_match"] != 1 {
			t.Fatalf("keyword=%q: minimum_should_match = %v, want 1", kw, boolQ["minimum_should_match"])
		}
		// 第一路：非圈内机器人（must_not exists）
		first := should[0]["bool"].(map[string]interface{})["must_not"].(map[string]interface{})
		if first["exists"].(map[string]interface{})["field"] != "agent_circle_id" {
			t.Fatalf("keyword=%q: should[0] not a must_not exists clause: %v", kw, should[0])
		}
		// 第二路：本圈机器人（term agent_circle_id = 本圈）
		term := should[1]["term"].(map[string]interface{})
		if term["agent_circle_id"] != testCircleID.String() {
			t.Fatalf("keyword=%q: term agent_circle_id = %v, want %s", kw, term["agent_circle_id"], testCircleID)
		}
	}
}

// TestBuildUserSearchQuery_SortUnchanged 作用域过滤不影响排序结构：
// 无关键字按 id desc（search_after 游标兼容），有关键字按 _score desc + id tiebreaker。
func TestBuildUserSearchQuery_SortUnchanged(t *testing.T) {
	cases := []struct {
		keyword   string
		firstSort string
	}{
		{"", "id"},
		{"kw", "_score"},
	}
	for _, tc := range cases {
		for _, cid := range []uuid.UUID{uuid.Nil, testCircleID} {
			q := buildUserSearchQuery(tc.keyword, 20, cid)
			sort, ok := q["sort"].([]map[string]interface{})
			if !ok || len(sort) == 0 {
				t.Fatalf("keyword=%q: sort missing", tc.keyword)
			}
			if _, has := sort[0][tc.firstSort]; !has {
				t.Fatalf("keyword=%q: sort[0] want %s, got %v", tc.keyword, tc.firstSort, sort[0])
			}
		}
	}
}
