package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/google/uuid"
)

// UserDocument 用户文档结构
type UserDocument struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url"`
	Gender     int8   `json:"gender"`
	Role       int8   `json:"role"`
	Status     int8   `json:"status"`
	Deleted    int8   `json:"deleted"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
}

// UserListResponse 用户列表响应
type UserListResponse struct {
	Users       []UserDocument `json:"users"`
	Total       int64          `json:"total"`
	Size        int            `json:"size"`
	SearchAfter []interface{}  `json:"search_after,omitempty"` // 用于获取下一页
}

// SearchUsers 搜索用户
// keyword: 搜索关键字，为空时返回所有符合条件的用户，优先使用 username 字段检索，其次使用 email 字段检索
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// circleID: 圈子作用域（@选人场景）。Nil=全站（排除所有圈内机器人）；
// 非 Nil=圈内（可见普通用户+全局机器人+本圈机器人，排除他圈机器人）。
// 返回：用户列表响应（包含用户列表、总数、分页信息）
func SearchUsers(ctx context.Context, keyword string, size int, searchAfter []interface{}, circleID uuid.UUID) (*UserListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	searchQuery := buildUserSearchQuery(keyword, size, circleID)

	// 添加 search_after 参数（如果提供）
	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	// 使用用户索引名称
	userIndex := GetUserIndexName()

	res, err := Client.Search(
		Client.Search.WithContext(ctx),
		Client.Search.WithIndex(userIndex),
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

	return parseUserSearchResponse(res, size)
}

// userBaseFilterConditions 基础过滤条件：过滤已删除、仅启用状态用户。
func userBaseFilterConditions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"term": map[string]interface{}{
				"deleted": 0, // 过滤掉已删除的用户
			},
		},
		{
			"term": map[string]interface{}{
				"status": 1, // 只返回启用的用户
			},
		},
	}
}

// agentCircleScopeCondition 圈内机器人 @提及 作用域过滤条件（users.agent_circle_id，
// CDC 自 PG 同步；存量文档无该字段 → must_not exists 天然命中，零重建索引）。
// circleID == Nil（全站@/用户搜索页）：排除所有圈内机器人；
// circleID 非 Nil（圈内@）：普通用户 + 全局机器人 + 本圈机器人可见，他圈机器人排除。
// 两路均放 must（filter 语义，不参与算分）。
func agentCircleScopeCondition(circleID uuid.UUID) map[string]interface{} {
	if circleID == uuid.Nil {
		return map[string]interface{}{
			"bool": map[string]interface{}{
				"must_not": map[string]interface{}{
					"exists": map[string]interface{}{"field": "agent_circle_id"},
				},
			},
		}
	}
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"should": []map[string]interface{}{
				{
					"bool": map[string]interface{}{
						"must_not": map[string]interface{}{
							"exists": map[string]interface{}{"field": "agent_circle_id"},
						},
					},
				},
				{
					"term": map[string]interface{}{"agent_circle_id": circleID.String()},
				},
			},
			"minimum_should_match": 1,
		},
	}
}

// buildUserSearchQuery 构建用户搜索 DSL。keyword 为空时按 id 倒序全量翻页；
// 非空时按 _score 相关性排序。circleID 语义见 agentCircleScopeCondition。
func buildUserSearchQuery(keyword string, size int, circleID uuid.UUID) map[string]interface{} {
	mustConditions := append(userBaseFilterConditions(), agentCircleScopeCondition(circleID))

	if keyword == "" {
		// 无关键字时，返回所有符合条件的用户，按id倒序(UUIDv7 字典序 == 时间序)
		return map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": mustConditions,
				},
			},
			"size": size,
			"sort": []map[string]interface{}{
				{
					"id": map[string]interface{}{
						"order": "desc",
					},
				},
			},
		}
	}

	// 关键字搜索：bool should 三路召回（minimum_should_match=1）——
	//  1-2. fuzzyShouldClauses：username 分词容错 + username.keyword 子串包含；
	//  3. email match：保留邮箱整串分词匹配（不做容错、不做子串，隐私面不扩大）。
	// 按 _score 排序，id 倒序作同分 tiebreaker。
	shouldClauses := append(fuzzyShouldClauses(keyword, "username"),
		map[string]interface{}{
			"match": map[string]interface{}{
				"email": map[string]interface{}{
					"query": keyword,
				},
			},
		})
	searchConditions := []map[string]interface{}{
		{
			"bool": map[string]interface{}{
				"should":               shouldClauses,
				"minimum_should_match": 1,
			},
		},
	}
	searchConditions = append(searchConditions, mustConditions...)

	return map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": searchConditions,
			},
		},
		"size": size,
		"sort": []map[string]interface{}{
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
		},
	}
}

// parseUserSearchResponse 解析用户搜索响应
func parseUserSearchResponse(res *esapi.Response, size int) (*UserListResponse, error) {
	var searchResult map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	hits := searchResult["hits"].(map[string]interface{})
	total := int64(hits["total"].(map[string]interface{})["value"].(float64))
	hitsList := hits["hits"].([]interface{})

	documents := make([]UserDocument, 0, len(hitsList))
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

		// 辅助函数：安全地从map中获取int8值
		getInt8 := func(key string) int8 {
			if val, ok := sourceMap[key]; ok && val != nil {
				if num, ok := val.(float64); ok {
					return int8(num)
				}
			}
			return 0
		}

		doc := UserDocument{
			ID:         getString("id"),
			Username:   getString("username"),
			Email:      getString("email"),
			AvatarURL:  getString("avatar_url"),
			Gender:     getInt8("gender"),
			Role:       getInt8("role"),
			Status:     getInt8("status"),
			Deleted:    getInt8("deleted"),
			CreateTime: getString("create_time"),
			UpdateTime: getString("update_time"),
		}
		documents = append(documents, doc)
	}

	// 如果有更多结果，返回 search_after 用于下一页
	response := &UserListResponse{
		Users: documents,
		Total: total,
		Size:  size,
	}
	if len(nextSearchAfter) > 0 && len(documents) == size {
		response.SearchAfter = nextSearchAfter
	}

	return response, nil
}
