//go:build integration

// following_feed_integration_test.go 首页关注流（#220）的 ES 检索集成测试（make test-integration）：
// 在开发栈 ES 建临时索引（命中 pg_domains_template，与 CDC 建出的 post 索引同一套 mapping），
// 写入模拟 CDC 同步后的帖子文档，执行真实 SearchFollowingFeed DSL，验证「圈子 或 作者」取并集且不重复、
// 私密圈 must_not、只返回已发布帖子、发帖时间倒序与 search_after 翻页不重不漏。
// CI integration job 不起 ES：testkit.ES 不可达时 Skip（本机用 QUBAR_TEST_REQUIRE_ES=1 强制执行）。
package elasticsearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/server/storage/elasticsearch"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// postDoc 模拟 CDC（es-sink 字段白名单）写入的 post 文档。
type postDoc struct {
	id, circle, author uuid.UUID
	status, deleted    int
	createTime         time.Time
}

// newPostIndex 建临时帖子索引并写入文档（refresh 后可查），测试结束删除。
func newPostIndex(t *testing.T, docs []postDoc) string {
	t.Helper()
	client := testkit.ES(t)
	elasticsearch.Client = client

	// 前缀与 CDC 索引一致 → 命中 pg_domains_template（应用启动时创建；make dev-run 过一次即存在）。
	index := conf.Config.Elasticsearch.IndexPrefix + ".post_it_" + strings.ReplaceAll(sharedomain.NewID().String(), "-", "")
	t.Cleanup(func() {
		if res, err := client.Indices.Delete([]string{index}); err == nil {
			_ = res.Body.Close()
		}
	})

	var body bytes.Buffer
	for _, d := range docs {
		meta, _ := json.Marshal(map[string]interface{}{"index": map[string]interface{}{"_index": index, "_id": d.id.String()}})
		src, _ := json.Marshal(map[string]interface{}{
			"id":          d.id.String(),
			"circle_id":   d.circle.String(),
			"user_id":     d.author.String(),
			"title":       "following feed probe",
			"status":      d.status,
			"deleted":     d.deleted,
			"hot":         0,
			"create_time": d.createTime.UTC().Format(time.RFC3339Nano),
		})
		body.Write(meta)
		body.WriteByte('\n')
		body.Write(src)
		body.WriteByte('\n')
	}
	res, err := client.Bulk(&body, client.Bulk.WithContext(context.Background()), client.Bulk.WithRefresh("true"))
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

// followingPages 用 pageSize 从第一页翻到底，返回按出现顺序拼起来的全部帖子 ID。
func followingPages(t *testing.T, index string, circles, authors, hidden []uuid.UUID, pageSize int) []uuid.UUID {
	t.Helper()
	var (
		all    []uuid.UUID
		cursor []interface{}
	)
	for page := 0; page < 20; page++ {
		resp, err := elasticsearch.SearchFollowingFeedInIndex(context.Background(), index, circles, authors, hidden, pageSize, cursor)
		if err != nil {
			t.Fatalf("SearchFollowingFeed page %d: %v", page, err)
		}
		for _, p := range resp.Posts {
			all = append(all, uuid.MustParse(p.ID))
		}
		// 与应用层一致：不满页即没有更多。
		if len(resp.Posts) < pageSize {
			return all
		}
		cursor = resp.SearchAfter
	}
	t.Fatalf("pagination did not terminate: %v", all)
	return nil
}

func TestSearchFollowingFeed(t *testing.T) {
	t.Parallel()
	var (
		joined   = sharedomain.NewID() // 我加入的圈子
		other    = sharedomain.NewID() // 我没加入的公开圈
		private  = sharedomain.NewID() // 私密圈（是否对我可见由用例决定）
		followed = sharedomain.NewID() // 我关注的普通用户
		bot      = sharedomain.NewID() // 我关注的 AI 机器人账号
		stranger = sharedomain.NewID() // 我没关注的人
	)
	base := time.Now().UTC().Truncate(time.Millisecond)
	at := func(minutesAgo int) time.Time { return base.Add(-time.Duration(minutesAgo) * time.Minute) }
	post := func(circle, author uuid.UUID, status, deleted, minutesAgo int) postDoc {
		return postDoc{id: sharedomain.NewID(), circle: circle, author: author, status: status, deleted: deleted, createTime: at(minutesAgo)}
	}
	var (
		byCircle    = post(joined, stranger, 1, 0, 1)   // 只命中圈子
		byAuthor    = post(other, followed, 1, 0, 2)    // 只命中作者
		byBoth      = post(joined, followed, 1, 0, 3)   // 两个条件都命中：只能出现一次
		inPrivate   = post(private, followed, 1, 0, 4)  // 关注的人发在私密圈
		unrelated   = post(other, stranger, 1, 0, 5)    // 都不命中
		unpublished = post(other, followed, 0, 0, 6)    // 关注的人的未发布帖子
		deleted     = post(joined, stranger, 1, 1, 7)   // 已加入圈子里的已删除帖子
		byBot       = post(other, bot, 1, 0, 8)         // 关注的机器人自动发的帖子
		tieOlder    = post(joined, stranger, 1, 0, 9)   // 与 tieNewer 同一发帖时间：按 id 倒序决胜
		tieNewer    = post(joined, stranger, 1, 0, 9)   // UUIDv7 后生成的 id 更大，排在前面
		oldest      = post(other, followed, 1, 0, 6000) // 很早的帖子：关注流不限时间窗口
	)
	index := newPostIndex(t, []postDoc{
		byCircle, byAuthor, byBoth, inPrivate, unrelated, unpublished, deleted, byBot, tieOlder, tieNewer, oldest,
	})

	ids := func(docs ...postDoc) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(docs))
		for _, d := range docs {
			out = append(out, d.id)
		}
		return out
	}
	circles, authors, hidden := []uuid.UUID{joined}, []uuid.UUID{followed, bot}, []uuid.UUID{private}

	cases := []struct {
		name                     string
		circles, authors, hidden []uuid.UUID
		want                     []uuid.UUID
	}{
		{"circles or authors", circles, authors, hidden, ids(byCircle, byAuthor, byBoth, byBot, tieNewer, tieOlder, oldest)},
		{"only circles", circles, nil, hidden, ids(byCircle, byBoth, tieNewer, tieOlder)},
		{"only authors", nil, authors, hidden, ids(byAuthor, byBoth, byBot, oldest)},
		// 私密圈对查看者可见（已加入私密圈 → 不在排除列表里）时，关注的人在里面发的帖子才出现。
		{"private circle visible", circles, authors, nil, ids(byCircle, byAuthor, byBoth, inPrivate, byBot, tieNewer, tieOlder, oldest)},
		// 只因为加入了私密圈而命中（作者没关注）不受影响；排除列表里的圈子即使在 circles 里也不返回。
		{"hidden wins over joined", []uuid.UUID{joined, private}, nil, hidden, ids(byCircle, byBoth, tieNewer, tieOlder)},
	}
	for _, tc := range cases {
		// 每种页大小都要得到同一个序列：发帖时间倒序、同一帖子只出现一次、翻页不重不漏
		// （页大小 1 / 2 会把游标落在同一发帖时间的两条帖子之间）。
		for _, pageSize := range []int{1, 2, 3, 100} {
			got := followingPages(t, index, tc.circles, tc.authors, tc.hidden, pageSize)
			if !slices.Equal(got, tc.want) {
				t.Errorf("%s (page size %d):\n got %v\nwant %v", tc.name, pageSize, got, tc.want)
			}
		}
	}

	// 圈子与关注都为空：空结果，不会退化成全局最新流。
	resp, err := elasticsearch.SearchFollowingFeedInIndex(context.Background(), index, nil, nil, hidden, 20, nil)
	if err != nil || len(resp.Posts) != 0 {
		t.Fatalf("empty scope: err=%v posts=%v", err, resp)
	}
}

// 关注数达到上限（follow.max_following 默认 2000）时，全量关注 ID 放进一个 terms 查询仍能正常执行。
func TestSearchFollowingFeed_MaxFollowees(t *testing.T) {
	t.Parallel()
	const maxFollowing = 2000
	authors := make([]uuid.UUID, maxFollowing)
	for i := range authors {
		authors[i] = sharedomain.NewID()
	}
	circles := make([]uuid.UUID, 200) // 关注流取的已加入圈子上限
	for i := range circles {
		circles[i] = sharedomain.NewID()
	}
	now := time.Now().UTC()
	first := postDoc{id: sharedomain.NewID(), circle: sharedomain.NewID(), author: authors[0], status: 1, createTime: now}
	last := postDoc{id: sharedomain.NewID(), circle: sharedomain.NewID(), author: authors[maxFollowing-1], status: 1, createTime: now.Add(-time.Minute)}
	inCircle := postDoc{id: sharedomain.NewID(), circle: circles[199], author: sharedomain.NewID(), status: 1, createTime: now.Add(-2 * time.Minute)}
	miss := postDoc{id: sharedomain.NewID(), circle: sharedomain.NewID(), author: sharedomain.NewID(), status: 1, createTime: now}
	index := newPostIndex(t, []postDoc{first, last, inCircle, miss})

	got := followingPages(t, index, circles, authors, []uuid.UUID{sharedomain.NewID()}, 20)
	if want := []uuid.UUID{first.id, last.id, inCircle.id}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
