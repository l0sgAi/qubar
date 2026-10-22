package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// discover 发现页相关查询。
//
// 与 SearchHomeFeed / SearchCircles 的根本区别：用 function_score + random_score 打乱排序，
// 产出「发散性」随机采样（发现页本质），而非 hot / rank_score 的热度收敛。
// excludeCircleIDs 非空时以 must_not terms 过滤已加圈子（反气泡：登录态推气泡外内容）。
// 设计见 docs/discover-design.md §二、三。

// sampleDiscoverMaxSize 发现页单次采样上限（防爆；候选池重建按需取 pool_size）。
const sampleDiscoverMaxSize = 500

// SampleDiscoverPosts 发现页帖子随机采样（random_score）。
//
// excludeCircleIDs：登录用户已加圈子（反气泡排除）；nil/空=全局采样（匿名）。
// size：采样数量（<=0 || >500 回落 200）。
// 返回随机 postID 列表（每次结果不同，发散性来源）。
//
// 已交互帖（liked/collected/viewed）的排除在调用方（syncer/service）内存做：
// 数量可达上千，超出 ES terms 子句 1024 上限且性能随子句数下降；已有 Redis ZSET 数据，内存 set 过滤更稳。
func SampleDiscoverPosts(ctx context.Context, excludeCircleIDs []uuid.UUID, size int) ([]string, error) {
	if size <= 0 || size > sampleDiscoverMaxSize {
		size = 200
	}

	// 基础过滤：仅正常已发布、未删除帖（同 SearchHomeFeed）。
	filterClauses := []map[string]interface{}{
		{"term": map[string]interface{}{"deleted": 0}},
		{"term": map[string]interface{}{"status": 1}},
	}

	boolClause := map[string]interface{}{
		"filter": filterClauses,
	}
	// 反气泡：排除已加圈子（登录态）。匿名省略此 clause → 纯全局随机。
	if len(excludeCircleIDs) > 0 {
		ids := make([]string, 0, len(excludeCircleIDs))
		for _, c := range excludeCircleIDs {
			ids = append(ids, c.String())
		}
		boolClause["must_not"] = []map[string]interface{}{
			{"terms": map[string]interface{}{"circle_id": ids}},
		}
	}

	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			// function_score + random_score：每次打乱排序，发散来源。
			"function_score": map[string]interface{}{
				"query":      map[string]interface{}{"bool": boolClause},
				"functions":  []map[string]interface{}{{"random_score": map[string]interface{}{}}},
				"score_mode": "sum",
			},
		},
		"size":    size,
		"_source": []string{"id"}, // 只取 id，省传输
	}

	return sampleIDs(ctx, searchQuery, GetPostIndexName())
}

// SampleDiscoverCircles 发现页圈子随机采样（random_score）。
//
// excludeCircleIDs：已加圈子（反气泡）；nil/空=全局（匿名）。
// size：采样数量（<=0 || >500 回落 200）。
// 基础过滤同 SearchCircles：status=1、deleted=0、must_not join_type=2（排除私圈，展示不暴露私圈）。
func SampleDiscoverCircles(ctx context.Context, excludeCircleIDs []uuid.UUID, size int) ([]string, error) {
	if size <= 0 || size > sampleDiscoverMaxSize {
		size = 200
	}

	mustNot := []map[string]interface{}{
		{"term": map[string]interface{}{"join_type": 2}}, // 排除私圈
	}
	// 反气泡：排除已加圈子。
	if len(excludeCircleIDs) > 0 {
		ids := make([]string, 0, len(excludeCircleIDs))
		for _, c := range excludeCircleIDs {
			ids = append(ids, c.String())
		}
		mustNot = append(mustNot, map[string]interface{}{
			"terms": map[string]interface{}{"id": ids},
		})
	}

	boolClause := map[string]interface{}{
		"filter": []map[string]interface{}{
			{"term": map[string]interface{}{"status": 1}},
			{"term": map[string]interface{}{"deleted": 0}},
		},
		"must_not": mustNot,
	}

	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"function_score": map[string]interface{}{
				"query":      map[string]interface{}{"bool": boolClause},
				"functions":  []map[string]interface{}{{"random_score": map[string]interface{}{}}},
				"score_mode": "sum",
			},
		},
		"size":    size,
		"_source": []string{"id"},
	}

	return sampleIDs(ctx, searchQuery, GetCircleIndexName())
}

// sampleIDs 执行随机采样查询，提取 hits._source.id 列表。
//
// 复用 SearchHomeFeed/SearchCircles 的请求骨架（Client.Search + WithTrackTotalHits），
// 但只解析 id 字段（无需 sort_values / 全量 _source）。
func sampleIDs(ctx context.Context, searchQuery map[string]interface{}, indexName string) ([]string, error) {
	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal discover sample query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(indexName),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return nil, responseError("elasticsearch discover sample error", res)
	}

	var searchResult map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse discover sample response: %w", err)
	}

	hitsRaw, ok := searchResult["hits"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	hitsList, ok := hitsRaw["hits"].([]interface{})
	if !ok {
		return nil, nil
	}

	ids := make([]string, 0, len(hitsList))
	for _, hit := range hitsList {
		hitMap, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}
		source, ok := hitMap["_source"].(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := source["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
