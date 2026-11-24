package application

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"interestBar/pkg/domains/recommend/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop() // 降级路径会打错误日志
	os.Exit(m.Run())
}

// followingSearcher 记录关注流检索的入参，并按预设返回命中的帖子 ID。
type followingSearcher struct {
	calls                    int
	circles, authors, hidden []uuid.UUID
	size                     int
	searchAfter              []interface{}

	ids  []uuid.UUID
	next []interface{}
	err  error
}

func (s *followingSearcher) Search(context.Context, string, []uuid.UUID, []uuid.UUID, int, []interface{}) ([]uuid.UUID, []interface{}, error) {
	panic("following tab must use SearchFollowing")
}

func (s *followingSearcher) SearchFollowing(_ context.Context, circles, authors, hidden []uuid.UUID, size int, searchAfter []interface{}) ([]uuid.UUID, []interface{}, error) {
	s.calls++
	s.circles, s.authors, s.hidden, s.size, s.searchAfter = circles, authors, hidden, size, searchAfter
	return s.ids, s.next, s.err
}

type fixedCircles struct {
	ids      []uuid.UUID
	err      error
	gotLimit int
}

func (c *fixedCircles) ListJoinedCircleIDs(_ context.Context, _ uuid.UUID, limit int) ([]uuid.UUID, error) {
	c.gotLimit = limit
	return c.ids, c.err
}

type fixedFollowees struct {
	ids   []uuid.UUID
	err   error
	calls int
}

func (f *fixedFollowees) ListFolloweeIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	f.calls++
	return f.ids, f.err
}

// echoHydrator 原样回填传入的帖子 ID（顺序打乱由 hydrateOrdered 负责还原）。
type echoHydrator struct{ viewer uuid.UUID }

func (h *echoHydrator) Hydrate(_ context.Context, viewer uuid.UUID, ids []uuid.UUID) ([]domain.FeedPostItem, error) {
	h.viewer = viewer
	out := make([]domain.FeedPostItem, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		out = append(out, domain.FeedPostItem{ID: ids[i]})
	}
	return out, nil
}

// interactionSet 固定的交互态：liked → is_liked，shared → is_shared（#233）。
type interactionSet struct {
	liked, shared map[uuid.UUID]bool
}

func (s interactionSet) BatchCheck(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]bool, map[uuid.UUID]bool, map[uuid.UUID]bool, error) {
	return s.liked, nil, s.shared, nil
}

type followingEnv struct {
	svc       RecommendService
	searcher  *followingSearcher
	circles   *fixedCircles
	followees *fixedFollowees
	hydrator  *echoHydrator
}

func newFollowingEnv(circles, followees, hidden []uuid.UUID) *followingEnv {
	e := &followingEnv{
		searcher:  &followingSearcher{},
		circles:   &fixedCircles{ids: circles},
		followees: &fixedFollowees{ids: followees},
		hydrator:  &echoHydrator{},
	}
	e.svc = NewRecommendService(e.searcher, e.circles, nil, nil, e.hydrator, interactionSet{}, nil, nil, fixedHidden(hidden), e.followees)
	return e
}

func newIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	return ids
}

// #220：关注流把「已加入的圈子」和「关注的用户」一起交给一次检索；只有一种时另一种为空。
func TestFollowing_ScopePassedToSearch(t *testing.T) {
	circles, followees := newIDs(2), newIDs(3)
	cases := []struct {
		name               string
		circles, followees []uuid.UUID
	}{
		{"circles and followees", circles, followees},
		{"only circles", circles, nil},
		{"only followees", nil, followees},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newFollowingEnv(tc.circles, tc.followees, nil)
			e.searcher.ids = newIDs(1)
			page, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if e.searcher.calls != 1 || !slices.Equal(e.searcher.circles, tc.circles) || !slices.Equal(e.searcher.authors, tc.followees) {
				t.Fatalf("search scope: calls=%d circles=%v authors=%v", e.searcher.calls, e.searcher.circles, e.searcher.authors)
			}
			if len(page.Posts) != 1 || page.Posts[0].ID != e.searcher.ids[0] {
				t.Fatalf("posts = %+v", page.Posts)
			}
			if e.circles.gotLimit != 200 {
				t.Fatalf("joined circle limit = %d, want 200", e.circles.gotLimit)
			}
		})
	}
}

// 0 个圈子且 0 个关注：空页，不查 ES。
func TestFollowing_NothingJoinedNorFollowed(t *testing.T) {
	e := newFollowingEnv(nil, []uuid.UUID{}, newIDs(1))
	page, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.searcher.calls != 0 {
		t.Fatal("empty scope must not hit the searcher")
	}
	if page.Posts == nil || len(page.Posts) != 0 || page.HasMore || page.SearchAfter != "" {
		t.Fatalf("want empty page, got %+v", page)
	}
}

// 查看者不可见的私密圈交给检索排除（即使作者是关注的人），回填也按查看者过滤。
func TestFollowing_ExcludesHiddenCircles(t *testing.T) {
	hidden := newIDs(2)
	for _, tc := range []struct {
		name               string
		circles, followees []uuid.UUID
	}{
		{"with circles", newIDs(1), newIDs(1)},
		{"followees only", nil, newIDs(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newFollowingEnv(tc.circles, tc.followees, hidden)
			viewer := uuid.New()
			e.searcher.ids = newIDs(1)
			if _, err := e.svc.GetHomeFeed(context.Background(), viewer, "following", 10, 0, "", nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(e.searcher.hidden, hidden) || e.hydrator.viewer != viewer {
				t.Fatalf("hidden=%v viewer=%v", e.searcher.hidden, e.hydrator.viewer)
			}
		})
	}
}

// 关注 ID 读取失败：降级为仅圈子，不报错。
func TestFollowing_FolloweeLookupFailureDegradesToCircles(t *testing.T) {
	circles := newIDs(2)
	e := newFollowingEnv(circles, newIDs(1), nil)
	e.followees.err = errors.New("pg down")
	e.followees.ids = nil
	e.searcher.ids = newIDs(1)

	page, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil)
	if err != nil {
		t.Fatalf("followee lookup failure must not fail the feed: %v", err)
	}
	if e.searcher.calls != 1 || !slices.Equal(e.searcher.circles, circles) || len(e.searcher.authors) != 0 {
		t.Fatalf("want circles-only search, got circles=%v authors=%v", e.searcher.circles, e.searcher.authors)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("posts = %+v", page.Posts)
	}

	// 没有加入任何圈子时，降级后的范围为空 → 空页（同样不报错、不查 ES）。
	e = newFollowingEnv(nil, nil, nil)
	e.followees.err = errors.New("pg down")
	page, err = e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil)
	if err != nil || e.searcher.calls != 0 || len(page.Posts) != 0 || page.HasMore {
		t.Fatalf("want empty page without search: err=%v calls=%d page=%+v", err, e.searcher.calls, page)
	}
}

// 已加入圈子读取失败时，仍按关注的用户返回。
func TestFollowing_CircleLookupFailureKeepsFollowees(t *testing.T) {
	followees := newIDs(2)
	e := newFollowingEnv(nil, followees, nil)
	e.circles.err = errors.New("redis down")
	e.searcher.ids = newIDs(1)
	if _, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil); err != nil {
		t.Fatal(err)
	}
	if len(e.searcher.circles) != 0 || !slices.Equal(e.searcher.authors, followees) {
		t.Fatalf("circles=%v authors=%v", e.searcher.circles, e.searcher.authors)
	}
}

// 翻页方式不变：游标原样透传，下一页游标与 HasMore 来自检索结果；帖子按检索顺序返回并补交互态。
func TestFollowing_PaginationAndOrder(t *testing.T) {
	e := newFollowingEnv(newIDs(1), newIDs(1), nil)
	hits := newIDs(2)
	e.searcher.ids = hits
	e.searcher.next = []interface{}{"2026-11-10T08:00:00Z", hits[1].String()}
	e.svc = NewRecommendService(e.searcher, e.circles, nil, nil, e.hydrator,
		interactionSet{liked: map[uuid.UUID]bool{hits[1]: true}, shared: map[uuid.UUID]bool{hits[0]: true}}, nil, nil, fixedHidden(nil), e.followees)
	cursor := []interface{}{"2026-11-10T09:00:00Z", "prev"}

	page, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 2, 0, "", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if e.searcher.size != 2 || !slices.Equal(e.searcher.searchAfter, cursor) {
		t.Fatalf("size=%d searchAfter=%v", e.searcher.size, e.searcher.searchAfter)
	}
	if len(page.Posts) != 2 || page.Posts[0].ID != hits[0] || page.Posts[1].ID != hits[1] {
		t.Fatalf("posts out of search order: %+v", page.Posts)
	}
	if page.Posts[0].IsLiked || !page.Posts[1].IsLiked || !page.Posts[0].IsShared || page.Posts[1].IsShared {
		t.Fatalf("interaction flags not filled: %+v", page.Posts)
	}
	if !page.HasMore || page.SearchAfter != `["2026-11-10T08:00:00Z","`+hits[1].String()+`"]` {
		t.Fatalf("hasMore=%v searchAfter=%s", page.HasMore, page.SearchAfter)
	}

	// 不满页 → 没有更多；size 越界回落 20。
	e.searcher.ids, e.searcher.next = hits[:1], nil
	page, err = e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 500, 0, "", nil)
	if err != nil || page.HasMore || e.searcher.size != 20 {
		t.Fatalf("err=%v hasMore=%v size=%d", err, page.HasMore, e.searcher.size)
	}
}

// 检索失败仍然报错（只有关注 ID 读取失败才降级）。
func TestFollowing_SearchErrorPropagates(t *testing.T) {
	e := newFollowingEnv(newIDs(1), newIDs(1), nil)
	e.searcher.err = errors.New("es down")
	if _, err := e.svc.GetHomeFeed(context.Background(), uuid.New(), "following", 10, 0, "", nil); err == nil {
		t.Fatal("want search error")
	}
}

// 访客访问关注流返回 ErrLoginRequired，不读任何依赖。
func TestFollowing_GuestRequiresLogin(t *testing.T) {
	e := newFollowingEnv(newIDs(1), newIDs(1), nil)
	_, err := e.svc.GetHomeFeed(context.Background(), uuid.Nil, "following", 10, 0, "", nil)
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if e.searcher.calls != 0 || e.followees.calls != 0 {
		t.Fatal("guest request must not read followees or search")
	}
}
