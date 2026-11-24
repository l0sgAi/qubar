// Package application 的圈内帖子列表计数口径单元测试（#250）。
//
// 覆盖：GetCirclePosts 的 view/comment/like/collect 用 post 域 Redis post:stats（经 PostMediaFetcher 桥接）覆盖
// ES 快照；未命中的帖子保留快照；取不到实时计数（Redis 出错 → 桥接返回空 map / 未注入 fetcher）时降级为快照、不报错；
// share_count 仍走 BatchGetShareCounts（#233）。
package application

import (
	"context"
	"testing"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// postsRepo 只实现 GetCirclePosts 用到的 GetByID / ListPrivateIDs（无私密圈）。
type postsRepo struct {
	domain.CircleRepository
	circle domain.Circle
}

func (r *postsRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Circle, error) {
	if id != r.circle.ID {
		return nil, domain.ErrCircleNotFound
	}
	c := r.circle
	return &c, nil
}

func (r *postsRepo) ListPrivateIDs(context.Context) ([]uuid.UUID, error) { return nil, nil }

// fakePostsSearcher 固定返回一页 ES 帖子快照。
type fakePostsSearcher struct {
	CircleSearcher
	docs []PostDoc
}

func (s *fakePostsSearcher) SearchCirclePosts(_ context.Context, _ uuid.UUID, _, size int, _ []interface{}) (*RawCirclePostResult, error) {
	return &RawCirclePostResult{Posts: s.docs, Total: int64(len(s.docs)), Size: size}, nil
}

// fakePostFetcher 模拟 post 域桥接：stats 为 Redis post:stats 命中的帖子（未命中不在 map 里）。
type fakePostFetcher struct {
	stats      map[uuid.UUID]CirclePostStats
	shares     map[uuid.UUID]int
	statsCalls [][]uuid.UUID
}

func (f *fakePostFetcher) GetMediaByPostIDs(context.Context, []string) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (f *fakePostFetcher) BatchGetShareCounts(context.Context, []uuid.UUID) map[uuid.UUID]int {
	return f.shares
}

func (f *fakePostFetcher) BatchGetStats(_ context.Context, postIDs []uuid.UUID) map[uuid.UUID]CirclePostStats {
	f.statsCalls = append(f.statsCalls, postIDs)
	out := map[uuid.UUID]CirclePostStats{}
	for _, id := range postIDs {
		if st, ok := f.stats[id]; ok {
			out[id] = st
		}
	}
	return out
}

// newCirclePostsFixture 一个公开圈 + 两篇 ES 快照帖子（hit 在 Redis 有实时计数，miss 没有）。
func newCirclePostsFixture() (svc *circleServiceImpl, circleID, hit, miss uuid.UUID) {
	circleID, hit, miss = uuid.New(), uuid.New(), uuid.New()
	repo := &postsRepo{circle: domain.Circle{ID: circleID, Name: "c"}}
	searcher := &fakePostsSearcher{docs: []PostDoc{
		{ID: hit.String(), CircleID: circleID.String(), UserID: uuid.NewString(),
			ViewCount: 10, CommentCount: 2, LikeCount: 3, CollectCount: 1},
		{ID: miss.String(), CircleID: circleID.String(), UserID: uuid.NewString(),
			ViewCount: 20, CommentCount: 4, LikeCount: 5, CollectCount: 6},
	}}
	svc = NewCircleService(repo, nil, nil, nil, nil, searcher, nil, nil).(*circleServiceImpl)
	return svc, circleID, hit, miss
}

func assertCounts(t *testing.T, label string, p CirclePostListItem, view, comment, like, collect int) {
	t.Helper()
	if p.ViewCount != view || p.CommentCount != comment || p.LikeCount != like || p.CollectCount != collect {
		t.Fatalf("%s counts = view %d comment %d like %d collect %d; want %d %d %d %d", label,
			p.ViewCount, p.CommentCount, p.LikeCount, p.CollectCount, view, comment, like, collect)
	}
}

func TestGetCirclePosts_StatsOverlay(t *testing.T) {
	t.Parallel()
	svc, circleID, hit, miss := newCirclePostsFixture()
	fetcher := &fakePostFetcher{
		stats:  map[uuid.UUID]CirclePostStats{hit: {ViewCount: 11, CommentCount: 3, LikeCount: 4, CollectCount: 2}},
		shares: map[uuid.UUID]int{hit: 7},
	}
	svc.SetPostFetcher(fetcher)

	res, err := svc.GetCirclePosts(context.Background(), uuid.Nil, circleID, 1, 20, nil)
	if err != nil {
		t.Fatalf("GetCirclePosts: %v", err)
	}
	if len(res.Posts) != 2 || res.Posts[0].ID != hit || res.Posts[1].ID != miss {
		t.Fatalf("posts = %+v; want [hit, miss]", res.Posts)
	}
	assertCounts(t, "hit", res.Posts[0], 11, 3, 4, 2)  // Redis 命中：用实时值
	assertCounts(t, "miss", res.Posts[1], 20, 4, 5, 6) // 未命中：保留 ES 快照
	if res.Posts[0].ShareCount != 7 || res.Posts[1].ShareCount != 0 {
		t.Fatalf("share_count = %d / %d; want 7 / 0", res.Posts[0].ShareCount, res.Posts[1].ShareCount)
	}
	// 一页只取一次实时计数，入参为整页帖子
	if len(fetcher.statsCalls) != 1 || len(fetcher.statsCalls[0]) != 2 {
		t.Fatalf("BatchGetStats calls = %v; want one call with 2 ids", fetcher.statsCalls)
	}
}

func TestGetCirclePosts_StatsUnavailableKeepsSnapshot(t *testing.T) {
	t.Parallel()
	cases := map[string]PostMediaFetcher{
		"overlay failed (empty map)": &fakePostFetcher{}, // 桥接在 Redis 出错时返回空 map
		"fetcher not injected":       nil,
	}
	for name, fetcher := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, circleID, _, _ := newCirclePostsFixture()
			if fetcher != nil {
				svc.SetPostFetcher(fetcher)
			}
			res, err := svc.GetCirclePosts(context.Background(), uuid.Nil, circleID, 1, 20, nil)
			if err != nil {
				t.Fatalf("GetCirclePosts: %v", err)
			}
			assertCounts(t, "first", res.Posts[0], 10, 2, 3, 1)
			assertCounts(t, "second", res.Posts[1], 20, 4, 5, 6)
		})
	}
}
