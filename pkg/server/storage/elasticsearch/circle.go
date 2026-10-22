package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/google/uuid"
)

// CircleDocument 圈子文档结构
type CircleDocument struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description"`
	Hot         int    `json:"hot"`
	CategoryID  string `json:"category_id"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreateTime  string `json:"create_time"` // 使用ISO 8601格式字符串
	Status      int16  `json:"status"`
	Deleted     int16  `json:"deleted"`
	JoinType    int16  `json:"join_type"`
	// 排序值（用于 search_after 分页）
	SortValues []interface{} `json:"sort_values,omitempty"`
}

// CircleListResponse 圈子列表响应
type CircleListResponse struct {
	Circles     []CircleDocument `json:"circles"`
	Total       int64            `json:"total"`
	Size        int              `json:"size"`
	SearchAfter []interface{}    `json:"search_after,omitempty"` // 用于获取下一页
}

// SearchCircles 搜索圈子
// keyword: 搜索关键字，为空时返回所有符合条件的圈子，优先使用 name 字段检索，其次使用 description 字段检索
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：圈子列表响应（包含圈子列表、总数、分页信息）
func SearchCircles(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按id倒序（最新的圈子id最大，在前），避免日期精度和范围问题
	sortRules := []map[string]interface{}{
		{
			"id": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	if keyword == "" {
		// 无关键字时，返回所有符合条件的圈子，按id倒序
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
					"must_not": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"join_type": 2, // 过滤掉私密圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时：bool should 召回（分词容错 + name.keyword 子串包含，
		// 见 fuzzyShouldClauses），name 权重是 description 的 3 倍，按 _score 排序。
		// should 内 match_phrase/name.keyword 精确命中仅作加分（minimum_should_match=0）。
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
			{
				"id": map[string]interface{}{
					"order": "desc",
				},
			},
		}

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"bool": map[string]interface{}{
								"should":               fuzzyShouldClauses(keyword, "name", "description"),
								"minimum_should_match": 1,
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
					"must_not": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"join_type": 2, // 过滤掉私密圈子
							},
						},
					},
					"should": []map[string]interface{}{
						{
							"match_phrase": map[string]interface{}{
								"name": map[string]interface{}{
									"query": keyword,
									"boost": 10.0,
								},
							},
						},
						{
							"term": map[string]interface{}{
								"name.keyword": map[string]interface{}{
									"value": keyword,
									"boost": 20.0,
								},
							},
						},
					},
					"minimum_should_match": 0,
				},
			},
			"size": size,
			"sort": sortWithScore,
		}
	}

	// 添加 search_after 参数（如果提供）
	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(GetCircleIndexName()),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
		Client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return nil, responseError("elasticsearch search error", res)
	}

	return parseCircleSearchResponse(res, size)
}

// SearchRandomCircles 随机获取圈子列表（侧栏推荐场景）。
// size: 返回数量，默认 20（<=0 或 >100 回落 20）
// 用 function_score + random_score（无 seed）实现随机排序，每次请求结果不同；
// 过滤条件与 SearchCircles 一致（正常状态、未删除、非私密）。无 search_after 分页。
func SearchRandomCircles(ctx context.Context, size int) (*CircleListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"function_score": map[string]interface{}{
				"query": map[string]interface{}{
					"bool": map[string]interface{}{
						"must": []map[string]interface{}{
							{
								"term": map[string]interface{}{
									"status": 1, // 只返回正常状态的圈子
								},
							},
							{
								"term": map[string]interface{}{
									"deleted": 0, // 过滤掉已删除的圈子
								},
							},
						},
						"must_not": []map[string]interface{}{
							{
								"term": map[string]interface{}{
									"join_type": 2, // 过滤掉私密圈子
								},
							},
						},
					},
				},
				"random_score": map[string]interface{}{},
			},
		},
		"size": size,
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal random circles query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(GetCircleIndexName()),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
		Client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search random circles: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return nil, responseError("elasticsearch search error", res)
	}

	return parseCircleSearchResponse(res, size)
}

// SearchMyCircles 搜索我加入的圈子
// circleIDs: 圈子ID列表（必须）
// keyword: 搜索关键字，为空时不过滤关键词
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：圈子列表响应（包含圈子列表、总数、分页信息）
func SearchMyCircles(ctx context.Context, circleIDs []uuid.UUID, keyword string, size int, searchAfter []interface{}) (*CircleListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 如果没有圈子ID，返回空结果
	if len(circleIDs) == 0 {
		return &CircleListResponse{
			Circles: []CircleDocument{},
			Total:   0,
			Size:    size,
		}, nil
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按id倒序（最新的圈子id最大，在前），避免日期精度和范围问题
	sortRules := []map[string]interface{}{
		{
			"id": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	// 将 circleIDs 转换为 []interface{} 供ES查询使用 (UUID 字符串)
	circleIDInterface := make([]interface{}, len(circleIDs))
	for i, id := range circleIDs {
		circleIDInterface[i] = id.String()
	}

	if keyword == "" {
		// 无关键字时，只按圈子ID列表过滤
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"terms": map[string]interface{}{
								"id": circleIDInterface, // 只返回已加入的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时：bool should 召回（分词容错 + name.keyword 子串包含，
		// 见 fuzzyShouldClauses），与公开圈子搜索同一匹配语义。
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
			{
				"id": map[string]interface{}{
					"order": "desc",
				},
			},
		}

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"terms": map[string]interface{}{
								"id": circleIDInterface, // 只返回已加入的圈子
							},
						},
						{
							"bool": map[string]interface{}{
								"should":               fuzzyShouldClauses(keyword, "name", "description"),
								"minimum_should_match": 1,
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
					"should": []map[string]interface{}{
						{
							"match_phrase": map[string]interface{}{
								"name": map[string]interface{}{
									"query": keyword,
									"boost": 10.0,
								},
							},
						},
						{
							"term": map[string]interface{}{
								"name.keyword": map[string]interface{}{
									"value": keyword,
									"boost": 20.0,
								},
							},
						},
					},
					"minimum_should_match": 0,
				},
			},
			"size": size,
			"sort": sortWithScore,
		}
	}
	// 添加 search_after 参数（如果提供）
	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(GetCircleIndexName()),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
		Client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return nil, responseError("elasticsearch search error", res)
	}

	return parseCircleSearchResponse(res, size)
}

// parseCircleSearchResponse 解析圈子搜索响应
func parseCircleSearchResponse(res *esapi.Response, size int) (*CircleListResponse, error) {
	var searchResult map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	hits := searchResult["hits"].(map[string]interface{})
	total := int64(hits["total"].(map[string]interface{})["value"].(float64))
	hitsList := hits["hits"].([]interface{})

	documents := make([]CircleDocument, 0, len(hitsList))
	var nextSearchAfter []interface{}

	for _, hit := range hitsList {
		hitMap := hit.(map[string]interface{})
		source := hitMap["_source"]
		sourceMap := source.(map[string]interface{})

		// 获取排序值（用于下一页）
		if sortArr, ok := hitMap["sort"].([]interface{}); ok {
			if len(sortArr) > 0 {
				nextSearchAfter = sortArr
			}
		}

		// 辅助函数：安全地从map中获取字符串值
		getString := func(key string) string {
			if val, ok := sourceMap[key]; ok && val != nil {
				if str, ok := val.(string); ok {
					return str
				}
			}
			return ""
		}

		// 辅助函数：安全地从map中获取整数值
		getInt := func(key string) int {
			if val, ok := sourceMap[key]; ok && val != nil {
				if num, ok := val.(float64); ok {
					return int(num)
				}
			}
			return 0
		}

		// 辅助函数：安全地从map中获取int16值
		getInt16 := func(key string) int16 {
			if val, ok := sourceMap[key]; ok && val != nil {
				if num, ok := val.(float64); ok {
					return int16(num)
				}
			}
			return 0
		}

		doc := CircleDocument{
			ID:          getString("id"),
			Name:        getString("name"),
			Slug:        getString("slug"),
			AvatarURL:   getString("avatar_url"),
			Description: getString("description"),
			Hot:         getInt("hot"),
			CategoryID:  getString("category_id"),
			MemberCount: getInt("member_count"),
			PostCount:   getInt("post_count"),
			CreateTime:  getString("create_time"),
			Status:      getInt16("status"),
			Deleted:     getInt16("deleted"),
			JoinType:    getInt16("join_type"),
		}
		documents = append(documents, doc)
	}

	// 如果有更多结果，返回 search_after 用于下一页
	response := &CircleListResponse{
		Circles: documents,
		Total:   total,
		Size:    size,
	}
	if len(nextSearchAfter) > 0 && len(documents) == size {
		response.SearchAfter = nextSearchAfter
	}

	return response, nil
}
