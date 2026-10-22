package httputil

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"interestBar/pkg/shared/appctx"
	sharedomain "interestBar/pkg/shared/domain"
)

// recordCtx 只实现 SearchError 用到的响应方法，其余方法由内嵌的 nil 接口兜底（调用即 panic）。
type recordCtx struct {
	appctx.AppContext
	status int
	body   any
}

func (r *recordCtx) JSON(code int, v any) { r.status, r.body = code, v }
func (r *recordCtx) Abort()               {}

func TestSearchError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"overloaded", sharedomain.ErrSearchOverloaded, http.StatusServiceUnavailable},
		{"wrapped overloaded", fmt.Errorf("search failed: %w", sharedomain.ErrSearchOverloaded), http.StatusServiceUnavailable},
		{"other error", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &recordCtx{}
			SearchError(c, tc.err, "Failed to search")
			if c.status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", c.status, tc.wantStatus)
			}
		})
	}
}
