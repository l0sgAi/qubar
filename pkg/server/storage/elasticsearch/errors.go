package elasticsearch

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	sharedomain "interestBar/pkg/shared/domain"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// ErrSearchOverloaded ES 搜索线程池 / 队列已满（429 / es_rejected_execution_exception），
// 属于可重试的暂时错误，接口层应映射为 503 而不是 500。
// 与共享内核哨兵同一个值，handler 无需 import 本包即可 errors.Is 判断。
var ErrSearchOverloaded = sharedomain.ErrSearchOverloaded

// IsSearchOverloaded 判断错误链中是否包含 ES 过载错误。
func IsSearchOverloaded(err error) bool {
	return errors.Is(err, ErrSearchOverloaded)
}

// responseError 把 ES 错误响应转成 error：429 或响应体含 es_rejected_execution_exception
// （包括包在 search_phase_execution_exception 里的情况）时包装 ErrSearchOverloaded。
func responseError(prefix string, res *esapi.Response) error {
	body := res.String()
	if isOverloadedResponse(res.StatusCode, body) {
		return fmt.Errorf("%s: %w: %s", prefix, ErrSearchOverloaded, body)
	}
	return fmt.Errorf("%s: %s", prefix, body)
}

func isOverloadedResponse(status int, body string) bool {
	return status == http.StatusTooManyRequests || strings.Contains(body, "es_rejected_execution_exception")
}
