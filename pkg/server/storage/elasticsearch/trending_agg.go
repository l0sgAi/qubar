package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// ===== 热点榜聚合（trending 领域） =====
//
// 设计见 docs/trending-design.md §三。与 AggregateActiveCircles（post.go:586）同族：
// 都在 post 索引上做窗口化聚合。差异：
//   - active-circles：terms on circle_id + order doc_count（按窗口内发帖数）
//   - trending：圈子/用户榜用 terms + 子聚合 sum(hot) 按 Σhot 排序；帖子榜直接 hits 按 hot desc。
//
// 前置条件：post 索引 circle_id / user_id 须为 keyword、create_time 须为 date、hot 须为 numeric。
// 不合规需加 ES index template（见 docs/active-circles-design.md §4.2）。

const (
	// trendingTermsSize terms 聚合最大桶数（榜单容量上限）。超出由 sum_other_doc_count → Truncated。
	trendingTermsSize = 500
)

// TrendingScoredItem 聚合产出的「实体 ID + 热度分」。
type TrendingScoredItem struct {
	ID    string  // post_id / circle_id / user_id
	Score float64 // post 榜=post.hot；circle/user 榜=Σhot
}

// TrendingAggResult 趋势聚合结果。
type TrendingAggResult struct {
	Items     []TrendingScoredItem
	Total     int64 // 近似活跃实体总数（cardinality，仅 circle/user 维度有意义）
	Truncated bool  // 触达 terms.size 上限（sum_other_doc_count > 0）
}

// AggregateTrending 热点榜聚合（3 维度 × 2 窗口统一入口）。
//
//	dimension = "post"   → hits 按 hot desc（最热帖）
//	dimension = "circle" → terms on circle_id + 子聚合 sum(hot)（Σhot 最高的圈子）
//	dimension = "user"   → terms on user_id  + 子聚合 sum(hot)（Σhot 最高的用户）
//	window    = "24h" | "7d" | ""（空串=无窗口，兜底用全局热门，见 docs/trending-fallback-design.md）
//	size      = 入榜条数（job 传 top_n，如 100）
//
// 返回有序 TrendingScoredItem（score 降序）。
func AggregateTrending(ctx context.Context, dimension, window string, size int) (*TrendingAggResult, error) {
	if size <= 0 || size > trendingTermsSize {
		size = trendingTermsSize
	}

	filter := []map[string]interface{}{
		{"term": map[string]interface{}{"deleted": 0}},
		{"term": map[string]interface{}{"status": 1}},
	}
	// 仅当指定窗口才附加 range create_time；window="" 表示无窗口（全局兜底）。
	if window != "" {
		windowGTE, ok := trendingWindowGTE(window)
		if !ok {
			return nil, fmt.Errorf("unsupported trending window: %q", window)
		}
		filter = append(filter, map[string]interface{}{
			"range": map[string]interface{}{"create_time": map[string]interface{}{"gte": windowGTE}},
		})
	}

	var searchQuery map[string]interface{}
	switch dimension {
	case "post":
		searchQuery = trendingPostQuery(filter, size)
	case "circle":
		searchQuery = trendingTermsQuery(filter, "circle_id", "by_circle", size)
	case "user":
		searchQuery = trendingTermsQuery(filter, "user_id", "by_user", size)
	default:
		return nil, fmt.Errorf("unsupported trending dimension: %q", dimension)
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal trending query: %w", err)
	}

	postIndex := GetPostIndexName()
	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(postIndex),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate trending (%s/%s): %w", dimension, window, err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch trending aggregate error: %s", res.String())
	}

	switch dimension {
	case "post":
		return parseTrendingHitsResponse(res)
	default:
		return parseTrendingTermsResponse(res)
	}
}

// trendingWindowGTE 把窗口枚举映射为 ES range gte 表达式。未知窗口返回 false。
func trendingWindowGTE(window string) (string, bool) {
	switch window {
	case "24h":
		return "now-24h/h", true
	case "7d":
		return "now-7d/d", true
	default:
		return "", false
	}
}

// trendingPostQuery 帖子榜：hits 按 hot desc（窗口内最热帖）。
//
// 不用 rank_score 时间衰减（那是首页信息流 hot tab 的长期热度语义），
// 本榜已由 create_time 时间窗显式圈定近期，直接用原始 hot 排序语义更直白。
func trendingPostQuery(filter []map[string]interface{}, size int) map[string]interface{} {
	return map[string]interface{}{
		"size": size,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{"filter": filter},
		},
		"sort": []map[string]interface{}{
			{"hot": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}}, // 二级稳定序
		},
		"_source": []string{"id", "hot"},
	}
}

// trendingTermsQuery 圈子/用户榜：terms 聚合 + 子聚合 sum(hot)，按 Σhot 降序。
func trendingTermsQuery(filter []map[string]interface{}, field, aggName string, size int) map[string]interface{} {
	return map[string]interface{}{
		"size": 0, // 不要 hits，只要聚合桶
		"query": map[string]interface{}{
			"bool": map[string]interface{}{"filter": filter},
		},
		"aggs": map[string]interface{}{
			aggName: map[string]interface{}{
				"terms": map[string]interface{}{
					"field": field,
					"size":  trendingTermsSize,
					"order": map[string]interface{}{"hot_sum": "desc"}, // 按 Σhot 而非 doc_count
				},
				"aggs": map[string]interface{}{
					"hot_sum": map[string]interface{}{"sum": map[string]interface{}{"field": "hot"}},
				},
			},
			"active_total": map[string]interface{}{
				"cardinality": map[string]interface{}{
					"field":               field,
					"precision_threshold": 1000,
				},
			},
		},
	}
}

// parseTrendingHitsResponse 解析帖子榜（hits 路径）。
func parseTrendingHitsResponse(res *esapi.Response) (*TrendingAggResult, error) {
	var raw map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse trending hits response: %w", err)
	}

	result := &TrendingAggResult{}
	hits, ok := raw["hits"].(map[string]interface{})
	if !ok {
		return result, nil
	}
	hitList, _ := hits["hits"].([]interface{})

	result.Items = make([]TrendingScoredItem, 0, len(hitList))
	for _, h := range hitList {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		source, _ := hm["_source"].(map[string]interface{})
		if source == nil {
			continue
		}
		id, _ := source["id"].(string)
		if id == "" {
			continue
		}
		score := 0.0
		if v, ok := source["hot"].(float64); ok {
			score = v
		}
		result.Items = append(result.Items, TrendingScoredItem{ID: id, Score: score})
	}
	return result, nil
}

// parseTrendingTermsResponse 解析圈子/用户榜（terms 聚合桶路径）。
func parseTrendingTermsResponse(res *esapi.Response) (*TrendingAggResult, error) {
	var raw map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse trending terms response: %w", err)
	}

	result := &TrendingAggResult{}
	aggs, ok := raw["aggregations"].(map[string]interface{})
	if !ok {
		return result, nil
	}

	// active_total.value（cardinality 近似活跃实体总数）
	if at, ok := aggs["active_total"].(map[string]interface{}); ok {
		if v, ok := at["value"].(float64); ok {
			result.Total = int64(v)
		}
	}

	// 桶可能在 by_circle 或 by_user，统一遍历定位。
	var bc map[string]interface{}
	if v, ok := aggs["by_circle"].(map[string]interface{}); ok {
		bc = v
	} else if v, ok := aggs["by_user"].(map[string]interface{}); ok {
		bc = v
	}
	if bc == nil {
		return result, nil
	}

	// sum_other_doc_count > 0：还有桶被 terms.size 截断 → 标记 truncated。
	if sodc, ok := bc["sum_other_doc_count"].(float64); ok && sodc > 0 {
		result.Truncated = true
	}

	buckets, _ := bc["buckets"].([]interface{})
	result.Items = make([]TrendingScoredItem, 0, len(buckets))
	for _, b := range buckets {
		bm, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		key, _ := bm["key"].(string)
		if key == "" {
			continue
		}
		score := 0.0
		if hs, ok := bm["hot_sum"].(map[string]interface{}); ok {
			if v, ok := hs["value"].(float64); ok {
				score = v
			}
		}
		result.Items = append(result.Items, TrendingScoredItem{ID: key, Score: score})
	}
	return result, nil
}
