package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

var (
	okCheck   Check = func(context.Context) error { return nil }
	failCheck Check = func(context.Context) error { return errors.New("dial tcp 10.0.0.5:5432: refused") }
	slowCheck Check = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
)

func TestReady(t *testing.T) {
	cases := []struct {
		name     string
		c        Checker
		wantOK   bool
		wantFail []string
	}{
		{"all ok", Checker{Required: map[string]Check{"postgres": okCheck, "redis": okCheck}}, true, nil},
		{"required down", Checker{Required: map[string]Check{"postgres": failCheck, "redis": okCheck}}, false, []string{"postgres"}},
		{"optional down stays ready", Checker{
			Required: map[string]Check{"postgres": okCheck},
			Optional: map[string]Check{"elasticsearch": failCheck},
		}, true, []string{"elasticsearch"}},
		{"timeout counts as fail", Checker{Timeout: 20 * time.Millisecond, Required: map[string]Check{"redis": slowCheck}}, false, []string{"redis"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, rep := tc.c.Ready(context.Background())
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.wantOK, rep)
			}
			for _, name := range tc.wantFail {
				if rep.Checks[name] != "fail" {
					t.Errorf("check %s = %q, want fail", name, rep.Checks[name])
				}
			}
		})
	}
}

func TestEndpoints(t *testing.T) {
	h := server.Default(server.WithHostPorts("127.0.0.1:0"))
	Register(h, &Checker{
		Required: map[string]Check{"postgres": failCheck},
		Optional: map[string]Check{"elasticsearch": okCheck},
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, PathLive, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", w.Code)
	}

	w = ut.PerformRequest(h.Engine, http.MethodGet, PathReady, nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503", w.Code)
	}
	body := w.Body.Bytes()
	var rep Report
	if err := json.Unmarshal(body, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Status != "unavailable" || rep.Checks["postgres"] != "fail" || rep.Checks["elasticsearch"] != "ok" {
		t.Fatalf("report = %+v", rep)
	}
	if strings.Contains(string(body), "10.0.0.5") {
		t.Fatal("response must not leak error details")
	}
}
