package elasticsearch

import "github.com/google/uuid"

// excludeCircles 私密圈内容隔离（#170）：给帖子查询追加 must_not terms circle_id。
//
// 帖子文档没有圈子可见性字段（外部 CDC 写入，不改 mapping），调用方传入查看者不可见的圈子
// （所有私密圈 − 查看者已加入的私密圈）。searchQuery 须为 {"query": {"bool": {...}}} 形态；
// circleIDs 为空时不改动。与已有 must_not 子句合并。
func excludeCircles(searchQuery map[string]interface{}, circleIDs []uuid.UUID) {
	if len(circleIDs) == 0 {
		return
	}
	query, _ := searchQuery["query"].(map[string]interface{})
	boolClause, _ := query["bool"].(map[string]interface{})
	if boolClause == nil {
		// 构造方保证形态；走到这里是编码错误，宁可让查询失败也不静默漏过滤。
		panic("elasticsearch: excludeCircles requires a bool query")
	}
	ids := make([]string, 0, len(circleIDs))
	for _, id := range circleIDs {
		ids = append(ids, id.String())
	}
	mustNot, _ := boolClause["must_not"].([]map[string]interface{})
	boolClause["must_not"] = append(mustNot, map[string]interface{}{
		"terms": map[string]interface{}{"circle_id": ids},
	})
}
