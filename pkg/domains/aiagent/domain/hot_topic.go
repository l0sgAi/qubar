package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// 网络搜索热点来源的哨兵错误（#100）。
var (
	// ErrSearchDisabled 平台未配置搜索服务（api_key 为空或 provider 不支持），调用直接返回。
	ErrSearchDisabled = errors.New("hot topic search is disabled")
	// ErrSearchQuotaExhausted 该机器人当日搜索配额已用尽（按自然日重置）。
	ErrSearchQuotaExhausted = errors.New("hot topic search daily quota exhausted")
)

// TopicItem 一条站外热点搜索结果。只含标题 / 摘要 / 链接 / 发布时间 / 来源站点，
// 不抓取 URL 指向的网页正文（SSRF 风险点，见 #100）；URL 仅作为文本存储 / 展示。
type TopicItem struct {
	Title       string
	Snippet     string
	URL         string    // 仅 http/https，未通过校验的条目被丢弃
	PublishedAt time.Time // 服务商未返回时为零值
	Source      string    // 来源站点名，缺省取 URL 的 host
}

// SearchOptions 单次搜索参数。
type SearchOptions struct {
	// AgentID 发起搜索的机器人，用于每日配额计数（必填）。
	AgentID uuid.UUID
	// MaxResults 最多返回条数，<=0 用平台配置 aiagent.search.max_results。
	MaxResults int
	// FreshnessHours 只要最近 N 小时内的结果，<=0 用平台配置 aiagent.search.freshness_hours。
	FreshnessHours int
	// DailyQuota 该机器人每日搜索次数上限，<=0 用平台配置 aiagent.search.daily_quota。
	DailyQuota int
}

// HotTopicSource 站外热点来源端口（由 infrastructure 按 aiagent.search.provider 选择实现）。
// 未启用时返回 ErrSearchDisabled，超出机器人每日配额返回 ErrSearchQuotaExhausted；
// 其余错误为服务商调用失败（错误信息不含 api_key）。
type HotTopicSource interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]TopicItem, error)
}

// SearchQuota 机器人每日搜索配额计数端口（Redis 实现）。
type SearchQuota interface {
	// Consume 占用一次当日配额；占用后超过 limit 返回 ErrSearchQuotaExhausted。
	// 调用前即计数：服务商调用失败也算一次（与回复链路"失败也算已触发"一致，防止重试刷费用）。
	Consume(ctx context.Context, agentID uuid.UUID, limit int) error
}
