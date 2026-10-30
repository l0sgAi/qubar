package application

import (
	"context"
	"slices"
	"testing"

	"interestBar/pkg/domains/recommend/domain"

	"github.com/google/uuid"
)

type visSearcher struct{ excluded []uuid.UUID }

func (s *visSearcher) Search(_ context.Context, _ string, _, exclude []uuid.UUID, _ int, _ []interface{}) ([]uuid.UUID, []interface{}, error) {
	s.excluded = exclude
	return []uuid.UUID{uuid.New()}, nil, nil
}

type visHydrator struct{ viewer uuid.UUID }

func (h *visHydrator) Hydrate(_ context.Context, viewer uuid.UUID, _ []uuid.UUID) ([]domain.FeedPostItem, error) {
	h.viewer = viewer
	return nil, nil
}

type noInteractions struct{}

func (noInteractions) BatchCheck(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]bool, map[uuid.UUID]bool, error) {
	return nil, nil, nil
}

type fixedHidden []uuid.UUID

func (f fixedHidden) HiddenCircleIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) { return f, nil }

// #170：hot / latest 流把查看者不可见的私密圈交给 ES must_not，回填按查看者过滤。
func TestSimpleFeed_ExcludesHiddenCircles(t *testing.T) {
	hidden := fixedHidden{uuid.New()}
	viewer := uuid.New()
	for _, tab := range []string{"hot", "latest"} {
		searcher, hydrator := &visSearcher{}, &visHydrator{}
		svc := NewRecommendService(searcher, nil, nil, nil, hydrator, noInteractions{}, nil, nil, hidden)
		if _, err := svc.GetHomeFeed(context.Background(), viewer, tab, 10, 0, "", nil); err != nil {
			t.Fatalf("%s: %v", tab, err)
		}
		if !slices.Equal(searcher.excluded, []uuid.UUID(hidden)) || hydrator.viewer != viewer {
			t.Fatalf("%s: excluded=%v viewer=%v", tab, searcher.excluded, hydrator.viewer)
		}
	}
}
