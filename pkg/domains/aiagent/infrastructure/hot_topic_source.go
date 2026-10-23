package infrastructure

import (
	"context"
	"errors"
	"strings"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// aiagent.search 数值配置的兜底默认值（配置 <=0 时使用）。
const (
	defaultSearchTimeout    = 10 * time.Second
	defaultSearchMaxResult  = 10
	maxSearchMaxResult      = 50 // 博查单次上限
	defaultSearchFreshness  = 24
	defaultSearchDailyQuota = 20
)

// 调用参数错误（不消耗配额）。
var (
	errEmptySearchQuery = errors.New("hot topic search query is empty")
	errSearchNoAgent    = errors.New("hot topic search requires agent id")
)

// NewHotTopicSource 按 aiagent.search 配置构造热点来源：
//   - api_key 为空、provider 不支持或 endpoint 非法 → 返回"未启用"实现（启动不报错，调用返回 ErrSearchDisabled）
//   - 否则 = 服务商适配器 + 机器人每日配额（先计数后调用）
//
// allowPrivate 同 aiagent.allow_private_base_url：仅本地开发 / 测试允许 endpoint 指向内网或 http。
func NewHotTopicSource(cfg conf.AiAgentSearch, quota domain.SearchQuota, allowPrivate bool) domain.HotTopicSource {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return disabledHotTopicSource{}
	}
	timeout := defaultSearchTimeout
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	var provider domain.HotTopicSource
	switch p := strings.ToLower(strings.TrimSpace(cfg.Provider)); p {
	case "bocha":
		src, err := newBochaSource(cfg.Endpoint, strings.TrimSpace(cfg.APIKey), timeout, allowPrivate)
		if err != nil {
			warnSearchDisabled("invalid aiagent.search.endpoint", zap.Error(err))
			return disabledHotTopicSource{}
		}
		provider = src
	default:
		warnSearchDisabled("unsupported aiagent.search.provider", zap.String("provider", p))
		return disabledHotTopicSource{}
	}
	return &quotaHotTopicSource{
		inner:          provider,
		quota:          quota,
		maxResults:     positiveOr(cfg.MaxResults, defaultSearchMaxResult),
		freshnessHours: positiveOr(cfg.FreshnessHours, defaultSearchFreshness),
		dailyQuota:     positiveOr(cfg.DailyQuota, defaultSearchDailyQuota),
	}
}

// warnSearchDisabled 记录配置问题导致的搜索关闭（字段里不带 api_key）。
func warnSearchDisabled(msg string, fields ...zap.Field) {
	if logger.Log != nil {
		logger.Log.Warn("hot topic search disabled: "+msg, fields...)
	}
}

// disabledHotTopicSource 未启用时的实现：不发请求、不计配额。
type disabledHotTopicSource struct{}

func (disabledHotTopicSource) Search(context.Context, string, domain.SearchOptions) ([]domain.TopicItem, error) {
	return nil, domain.ErrSearchDisabled
}

// quotaHotTopicSource 补齐默认参数并按机器人扣每日配额，再调用服务商适配器。
type quotaHotTopicSource struct {
	inner          domain.HotTopicSource
	quota          domain.SearchQuota
	maxResults     int
	freshnessHours int
	dailyQuota     int
}

func (s *quotaHotTopicSource) Search(ctx context.Context, query string, opts domain.SearchOptions) ([]domain.TopicItem, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errEmptySearchQuery
	}
	if opts.AgentID == uuid.Nil {
		return nil, errSearchNoAgent
	}
	opts.MaxResults = min(positiveOr(opts.MaxResults, s.maxResults), maxSearchMaxResult)
	opts.FreshnessHours = positiveOr(opts.FreshnessHours, s.freshnessHours)
	opts.DailyQuota = positiveOr(opts.DailyQuota, s.dailyQuota)
	if err := s.quota.Consume(ctx, opts.AgentID, opts.DailyQuota); err != nil {
		return nil, err
	}
	return s.inner.Search(ctx, query, opts)
}

func positiveOr(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
