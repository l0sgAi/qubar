package elasticsearch

import (
	"context"

	"github.com/google/uuid"
)

// homeFeedQuery 构造首页流（hot / latest / following）的 ES 查询体，三个 tab 共用同一套基础过滤。
//
// 基础过滤：deleted=0 且 status=1（只要已发布帖子）。
// 范围（circleIDs / authorIDs 都为空 = 全局）：
//   - 只有一个非空：对应的 terms 直接进 must（只给 circleIDs 时与历史查询体一致）；
//   - 两个都非空：进 should + minimum_should_match=1，即「圈子命中 或 作者命中」。
//     一次查询里每个文档至多命中一次，所以同一帖子不会重复出现。
//
// 作者字段是 user_id（索引模板把 id / *_id 动态映射成 keyword，值为 UUID 字符串）。
// excludeCircleIDs 走 must_not（见 excludeCircles），对两种范围条件都生效。
// sort: "latest"=create_time desc，其余=rank_score 时间衰减；两者都以 id desc 做 search_after 的决胜键。
func homeFeedQuery(sort string, circleIDs, authorIDs, excludeCircleIDs []uuid.UUID, size int, searchAfter []interface{}) map[string]interface{} {
	mustConditions := []map[string]interface{}{
		{"term": map[string]interface{}{"deleted": 0}},
		{"term": map[string]interface{}{"status": 1}},
	}

	var scope []map[string]interface{}
	if len(circleIDs) > 0 {
		scope = append(scope, map[string]interface{}{
			"terms": map[string]interface{}{"circle_id": uuidStrings(circleIDs)},
		})
	}
	if len(authorIDs) > 0 {
		scope = append(scope, map[string]interface{}{
			"terms": map[string]interface{}{"user_id": uuidStrings(authorIDs)},
		})
	}

	boolClause := map[string]interface{}{}
	if len(scope) == 1 {
		mustConditions = append(mustConditions, scope[0])
	} else if len(scope) > 1 {
		boolClause["should"] = scope
		boolClause["minimum_should_match"] = 1
	}
	boolClause["must"] = mustConditions

	var sortRules []map[string]interface{}
	var runtimeMappings map[string]interface{}

	switch sort {
	case "latest": // 最新：按发帖时间降序
		sortRules = []map[string]interface{}{
			{"create_time": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}},
		}
	default: // 近期热点：rank_score = hot / (age_hours + 2)^0.8
		runtimeMappings = map[string]interface{}{
			"rank_score": map[string]interface{}{
				"type": "double",
				"script": map[string]interface{}{
					"source": "double ageHours = (System.currentTimeMillis() - doc['create_time'].value.toInstant().toEpochMilli()) / 3600000.0; emit(doc['hot'].value / Math.pow(ageHours + 2, 0.8));",
				},
			},
		}
		sortRules = []map[string]interface{}{
			{"rank_score": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}},
		}
	}

	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{"bool": boolClause},
		"size":  size,
		"sort":  sortRules,
	}

	if runtimeMappings != nil {
		searchQuery["runtime_mappings"] = runtimeMappings
	}

	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	excludeCircles(searchQuery, excludeCircleIDs)
	return searchQuery
}

// SearchFollowingFeed 首页关注流检索（#220）：帖子所在圈子在 circleIDs 内，或作者在 authorIDs 内，
// 按发帖时间倒序、search_after 翻页；其余过滤与 SearchHomeFeed 的 latest 一致。
//
// circleIDs 与 authorIDs 都为空时直接返回空结果、不查 ES：范围为空的关注流不能退化成全局最新流。
// excludeCircleIDs：查看者不可见的私密圈（must_not），关注的人发在这些圈子里的帖子同样不返回。
func SearchFollowingFeed(ctx context.Context, circleIDs, authorIDs, excludeCircleIDs []uuid.UUID, size int, searchAfter []interface{}) (*PostListResponse, error) {
	return searchFollowingFeedInIndex(ctx, GetPostIndexName(), circleIDs, authorIDs, excludeCircleIDs, size, searchAfter)
}

// searchFollowingFeedInIndex SearchFollowingFeed 的实现，索引名由调用方给出（集成测试用临时索引）。
func searchFollowingFeedInIndex(ctx context.Context, postIndex string, circleIDs, authorIDs, excludeCircleIDs []uuid.UUID, size int, searchAfter []interface{}) (*PostListResponse, error) {
	if size <= 0 || size > 100 {
		size = 20
	}
	if len(circleIDs) == 0 && len(authorIDs) == 0 {
		return &PostListResponse{Posts: []PostDocument{}, Size: size}, nil
	}
	return runHomeFeedQuery(ctx, postIndex, homeFeedQuery("latest", circleIDs, authorIDs, excludeCircleIDs, size, searchAfter), size)
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
