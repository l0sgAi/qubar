//go:build integration

// user_integration_test.go 用户搜索（@选人）的 ES 过滤条件集成测试（#171，make test-integration）：
// 在开发栈 ES 建临时索引（命中 pg_domains_template，与 CDC 建出的 users 索引同一套 mapping），
// 写入模拟 CDC 同步后的用户文档，执行真实 SearchUsers DSL，验证停用 / 已删除机器人（agent_mention_disabled=1）
// 在全站与圈内两种场景都被排除、圈内机器人仍只在本圈可见、存量文档缺该字段时照常可见。
// CI integration job 不起 ES：testkit.ES 不可达时 Skip（本机用 QUBAR_TEST_REQUIRE_ES=1 强制执行）。
package elasticsearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/server/storage/elasticsearch"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// userDoc 模拟 CDC（es-sink 字段白名单）写入的 users 文档；nil 字段表示文档里没有该键。
type userDoc struct {
	id              uuid.UUID
	username        string
	role            int
	agentCircleID   *uuid.UUID // nil → agent_circle_id: null（Debezium 对 NULL 列输出 null）
	mentionDisabled *int       // nil → 文档无 agent_mention_disabled 键（上线前的存量文档）
}

func (d userDoc) source() map[string]interface{} {
	src := map[string]interface{}{
		"id":              d.id.String(),
		"username":        d.username,
		"email":           d.id.String() + "@it.qubar.local",
		"role":            d.role,
		"status":          1,
		"deleted":         0,
		"agent_circle_id": nil,
	}
	if d.agentCircleID != nil {
		src["agent_circle_id"] = d.agentCircleID.String()
	}
	if d.mentionDisabled != nil {
		src["agent_mention_disabled"] = *d.mentionDisabled
	}
	return src
}

func intPtr(v int) *int { return &v }

// newUserIndex 建临时用户索引并写入文档（refresh 后可查），测试结束删除。
func newUserIndex(t *testing.T, docs []userDoc) string {
	t.Helper()
	client := testkit.ES(t)
	elasticsearch.Client = client
	ctx := context.Background()

	// 前缀与 CDC 索引一致 → 命中 pg_domains_template（应用启动时创建；make dev-run 过一次即存在）。
	index := conf.Config.Elasticsearch.IndexPrefix + ".users_it_" + strings.ReplaceAll(sharedomain.NewID().String(), "-", "")
	t.Cleanup(func() {
		if res, err := client.Indices.Delete([]string{index}); err == nil {
			_ = res.Body.Close()
		}
	})

	var body bytes.Buffer
	for _, d := range docs {
		meta, _ := json.Marshal(map[string]interface{}{"index": map[string]interface{}{"_index": index, "_id": d.id.String()}})
		src, _ := json.Marshal(d.source())
		body.Write(meta)
		body.WriteByte('\n')
		body.Write(src)
		body.WriteByte('\n')
	}
	res, err := client.Bulk(&body, client.Bulk.WithContext(ctx), client.Bulk.WithRefresh("true"))
	if err != nil {
		t.Fatalf("bulk index: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var out struct {
		Errors bool `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || res.IsError() || out.Errors {
		t.Fatalf("bulk index failed: status=%s errors=%v decode=%v", res.Status(), out.Errors, err)
	}
	return index
}

// searchIDs 执行真实 SearchUsers DSL，返回命中 ID 集合。
func searchIDs(t *testing.T, index, keyword string, circleID uuid.UUID) map[uuid.UUID]bool {
	t.Helper()
	resp, err := elasticsearch.SearchUsersInIndex(context.Background(), index, keyword, 100, nil, circleID)
	if err != nil {
		t.Fatalf("SearchUsers(keyword=%q, circle=%s): %v", keyword, circleID, err)
	}
	got := make(map[uuid.UUID]bool, len(resp.Users))
	for _, u := range resp.Users {
		got[uuid.MustParse(u.ID)] = true
	}
	return got
}

func TestSearchUsers_ExcludesUnmentionableAgents(t *testing.T) {
	t.Parallel()
	circleA, circleB := sharedomain.NewID(), sharedomain.NewID()
	var (
		normal          = sharedomain.NewID() // 普通用户（存量文档，无 agent_mention_disabled 键）
		normalNew       = sharedomain.NewID() // 普通用户（新文档，agent_mention_disabled=0）
		globalBot       = sharedomain.NewID() // 启用的全局机器人
		legacyGlobalBot = sharedomain.NewID() // 启用的全局机器人（存量文档，无键）
		disabledGlobal  = sharedomain.NewID() // 停用 / 已删除的全局机器人
		circleBot       = sharedomain.NewID() // 启用的 A 圈机器人
		disabledCircle  = sharedomain.NewID() // 停用 / 已删除的 A 圈机器人（删除后圈子绑定保留）
	)
	index := newUserIndex(t, []userDoc{
		{id: normal, username: "probe normal"},
		{id: normalNew, username: "probe normal new", mentionDisabled: intPtr(0)},
		{id: globalBot, username: "probe global bot", role: 2, mentionDisabled: intPtr(0)},
		{id: legacyGlobalBot, username: "probe legacy bot", role: 2},
		{id: disabledGlobal, username: "probe disabled global", role: 2, mentionDisabled: intPtr(1)},
		{id: circleBot, username: "probe circle bot", role: 2, agentCircleID: &circleA, mentionDisabled: intPtr(0)},
		{id: disabledCircle, username: "probe disabled circle", role: 2, agentCircleID: &circleA, mentionDisabled: intPtr(1)},
	})

	cases := []struct {
		name   string
		circle uuid.UUID
		want   []uuid.UUID
	}{
		// 全站：圈内机器人一律排除，停用 / 已删除机器人排除，存量文档照常可见。
		{"site-wide", uuid.Nil, []uuid.UUID{normal, normalNew, globalBot, legacyGlobalBot}},
		// A 圈：本圈启用机器人可见，本圈停用 / 已删除机器人仍排除。
		{"own circle", circleA, []uuid.UUID{normal, normalNew, globalBot, legacyGlobalBot, circleBot}},
		// B 圈：A 圈机器人（含已删除的）不出现——删除不再清空绑定、也不会全站可见。
		{"other circle", circleB, []uuid.UUID{normal, normalNew, globalBot, legacyGlobalBot}},
	}
	for _, tc := range cases {
		for _, kw := range []string{"", "probe"} {
			got := searchIDs(t, index, kw, tc.circle)
			if len(got) != len(tc.want) {
				t.Errorf("%s keyword=%q: got %d hits %v, want %v", tc.name, kw, len(got), got, tc.want)
				continue
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Errorf("%s keyword=%q: missing %s in %v", tc.name, kw, id, got)
				}
			}
		}
	}
}
