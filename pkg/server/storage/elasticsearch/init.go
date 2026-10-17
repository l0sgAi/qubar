package elasticsearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/elastic/go-elasticsearch/v8"
)

var Client *elasticsearch.Client

// InitElasticsearch 初始化 Elasticsearch 客户端并创建索引
func InitElasticsearch() error {
	cfg := elasticsearch.Config{
		Addresses: []string{conf.Config.Elasticsearch.URL},
	}

	var err error
	Client, err = elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// 测试连接
	res, err := Client.Info()
	if err != nil {
		return fmt.Errorf("failed to get elasticsearch info: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	logger.Log.Info("Elasticsearch connected successfully")

	// 确保 index template 存在（必须在任何索引创建/删除之前）
	// template 用于规范 pg.domains.* 的字段映射，避免 Debezium CDC 自动建索引时
	// 把 id 等 *_id 字段推断为 text，导致排序/聚合触发 fielddata 错误。
	if err := ensureIndexTemplate(); err != nil {
		return fmt.Errorf("failed to ensure index template: %w", err)
	}

	// 创建圈子索引
	if err := createCircleIndex(); err != nil {
		return fmt.Errorf("failed to create circle index: %w", err)
	}

	return nil
}

// ensureIndexTemplate 确保 pg.domains.* 的 index template 存在且为期望配置。
//
// 背景：数据通过 Debezium CDC 同步进 ES，CDC 通常比 qubar 先启动并自动建索引，
// 此时 ES 的动态映射会把所有字符串字段推断为 text，导致：
//   - id / *_id 字段无法排序（Fielddata is disabled 错误）
//   - 数值字段被推断为 long，缺失分词配置
//   - 需要中文搜索的字段用不上 IK 分词器
//
// index template 会在任何匹配 pg.domains.* 的索引被创建时自动套用，
// 无论创建者是 CDC 还是 qubar，都能保证字段映射一致。
//
// 注意：template 只对创建之后的新索引生效，已有索引需要先删除再由 CDC 重建。
func ensureIndexTemplate() error {
	templateName := "pg_domains_template"

	// 动态模板规则（按顺序匹配，先命中先生效）：
	//   1. id / *_id        → keyword        (UUIDv7，需要支持 sort/search_after)
	//   2. 全文检索字段      → text + IK 分词  + .keyword 子字段 (中文搜索 + 精确匹配)
	//   3. *_time           → date           (时间字段统一为日期类型)
	//   4. 其余字符串        → keyword        (URL/slug/phone 等，避免无意义分词)
	//   5. 布尔/数值         → 保持 ES 自动推断 (long/boolean 等，符合预期)
	template := map[string]interface{}{
		"index_patterns": []string{conf.Config.Elasticsearch.IndexPrefix + ".*"},
		// priority 必须高于 Kibana/APM 等内置模板（默认 100），确保 pg.domains.* 优先套用本模板
		"priority": 200,
		"template": map[string]interface{}{
			"settings": map[string]interface{}{
				"number_of_shards":   1,
				"number_of_replicas": 0,
				"refresh_interval":   conf.Config.Elasticsearch.RefreshInterval,
			},
			"mappings": map[string]interface{}{
				// dynamic_templates：匹配「动态写入」的字段（即 mapping 里没显式声明的字段）
				// 注意：使用正则的规则必须声明 "match_pattern": "regex"，
				// 否则 ES 会把模式字符串当作字面字段名匹配（不识别 ^ $ | 等元字符）。
				"dynamic_templates": []map[string]interface{}{
					{
						"ids_as_keyword": map[string]interface{}{
							"match_mapping_type": "string",
							"match_pattern":      "regex",
							// 匹配 id, circle_id, user_id, creator_id, category_id 等
							"match": "^(id|.*_id)$",
							"mapping": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
					{
						// 需要中文全文检索的字段：索引时用 ik_max_word（最细粒度，召回高），
						// 搜索时用 ik_smart（智能切分，避免过度分词）。
						// 同时挂 .keyword 子字段用于精确匹配/排序/聚合。
						"fulltext_with_ik": map[string]interface{}{
							"match_mapping_type": "string",
							"match_pattern":      "regex",
							"match":              "^(username|email|name|title|summary|description|content)$",
							"mapping": map[string]interface{}{
								"type":            "text",
								"analyzer":        "ik_max_word",
								"search_analyzer": "ik_smart",
								"fields": map[string]interface{}{
									"keyword": map[string]interface{}{
										"type":         "keyword",
										"ignore_above": 256,
									},
								},
							},
						},
					},
					{
						"timestamps_as_date": map[string]interface{}{
							"match_mapping_type": "string",
							"match_pattern":      "regex",
							"match":              ".*_time$",
							"mapping": map[string]interface{}{
								"type": "date",
							},
						},
					},
					{
						// 兜底：其余字符串统一为 keyword，避免被推断为 text 后无意义分词
						// （如 avatar_url / slug / phone / 各类 ID 字符串等）
						"strings_as_keyword": map[string]interface{}{
							"match_mapping_type": "string",
							"mapping": map[string]interface{}{
								"type":         "keyword",
								"ignore_above": 512,
							},
						},
					},
				},
			},
		},
	}

	templateJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal index template: %w", err)
	}

	// 使用 PUT /_index_template/{name} 创建/覆盖 template（幂等，重启重复调用也安全）
	res, err := Client.Indices.PutIndexTemplate(templateName, bytes.NewReader(templateJSON))
	if err != nil {
		return fmt.Errorf("failed to put index template: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return fmt.Errorf("failed to put index template: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Index template '%s' ensured for pattern '%s.*'", templateName, conf.Config.Elasticsearch.IndexPrefix))
	return nil
}

// createCircleIndex 创建圈子索引（如果不存在）
func createCircleIndex() error {
	indexName := GetCircleIndexName()

	// 检查索引是否已存在
	res, err := Client.Indices.Exists([]string{indexName})
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.StatusCode == 200 {
		logger.Log.Info(fmt.Sprintf("Index '%s' already exists", indexName))
		return nil
	}

	// 定义索引映射
	mapping := map[string]interface{}{
		"settings": map[string]interface{}{
			"number_of_shards":   1,
			"number_of_replicas": 0,
			"refresh_interval":   conf.Config.Elasticsearch.RefreshInterval,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type": "keyword",
				},
				"name": map[string]interface{}{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
					"fields": map[string]interface{}{
						"keyword": map[string]interface{}{
							"type":         "keyword",
							"ignore_above": 256,
						},
					},
				},
				"slug": map[string]interface{}{
					"type":         "keyword",
					"ignore_above": 256,
				},
				"avatar_url": map[string]interface{}{
					"type":         "keyword",
					"ignore_above": 500,
				},
				"description": map[string]interface{}{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
				},
				"hot": map[string]interface{}{
					"type": "integer",
				},
				"category_id": map[string]interface{}{
					"type": "keyword",
				},
				"member_count": map[string]interface{}{
					"type": "integer",
				},
				"post_count": map[string]interface{}{
					"type": "integer",
				},
				"create_time": map[string]interface{}{
					"type": "date",
				},
				"update_time": map[string]interface{}{
					"type": "date",
				},
				"status": map[string]interface{}{
					"type": "short",
				},
				"deleted": map[string]interface{}{
					"type": "short",
				},
				"join_type": map[string]interface{}{
					"type": "short",
				},
			},
		},
	}

	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("failed to marshal mapping: %w", err)
	}

	// 创建索引
	res, err = Client.Indices.Create(
		indexName,
		Client.Indices.Create.WithBody(bytes.NewReader(mappingJSON)),
		Client.Indices.Create.WithPretty(),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer func() { _ = res.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if res.IsError() {
		return fmt.Errorf("failed to create index: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Index '%s' created successfully", indexName))
	return nil
}
