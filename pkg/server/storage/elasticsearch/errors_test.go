package elasticsearch

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

func fakeResponse(status int, body string) *esapi.Response {
	return &esapi.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestResponseError_Overloaded(t *testing.T) {
	t.Parallel()

	const rejected = `{"error":{"type":"es_rejected_execution_exception","reason":"rejected execution of search"},"status":429}`
	// 429 被包在 search_phase_execution_exception 里，外层状态码可能是 503。
	const wrapped = `{"error":{"type":"search_phase_execution_exception","root_cause":[{"type":"es_rejected_execution_exception"}]},"status":503}`

	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"429", http.StatusTooManyRequests, rejected},
		{"429 without body", http.StatusTooManyRequests, ""},
		{"wrapped in search_phase_execution_exception", http.StatusServiceUnavailable, wrapped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := responseError("elasticsearch search error", fakeResponse(tc.status, tc.body))
			if !IsSearchOverloaded(err) {
				t.Fatalf("want ErrSearchOverloaded, got %v", err)
			}
		})
	}
}

func TestResponseError_OtherErrors(t *testing.T) {
	t.Parallel()

	err := responseError("elasticsearch search error", fakeResponse(http.StatusBadRequest, `{"error":{"type":"parsing_exception"}}`))
	if err == nil || IsSearchOverloaded(err) {
		t.Fatalf("want plain error, got %v", err)
	}
}
