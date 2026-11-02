package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// auto_config 顶层 key（每种自主行为一个子对象，按 key 独立写入，互不覆盖）。
const (
	// AutoConfigKeyAutoReply 自动回复高热度帖子 / 评论（#99）。
	AutoConfigKeyAutoReply = "auto_reply"
	// AutoConfigKeyAutoPost 全局机器人在 hot 圈自动发帖（#101）。
	AutoConfigKeyAutoPost = "auto_post"
)

// 自动回复目标类型（auto_reply.targets 取值）。
const (
	AutoReplyTargetPost    = "post"    // 回复帖子本身（顶层评论）
	AutoReplyTargetComment = "comment" // 回复评论（挂在该评论楼层内）
)

// AutoConfigJSON 机器人自主行为配置（ai_agent.auto_config，jsonb 对象）。
//
// 按顶层 key 保存原始 JSON：本域只解析认识的子对象（auto_reply），其余兄弟键
// （如后续的 auto_post）原样保留，读改写不会丢失。与 trigger_mode 互不影响。
type AutoConfigJSON map[string]json.RawMessage

// Scan 实现 sql.Scanner 接口。
func (c *AutoConfigJSON) Scan(value interface{}) error {
	if value == nil {
		*c = make(AutoConfigJSON)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("AutoConfigJSON: cannot scan non-[]byte value")
	}
	m := make(AutoConfigJSON)
	if err := json.Unmarshal(bytes, &m); err != nil {
		return err
	}
	*c = m
	return nil
}

// Value 实现 driver.Valuer 接口。
func (c AutoConfigJSON) Value() (driver.Value, error) {
	if len(c) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(c)
}

// AutoReply 解析 auto_reply 子对象。未配置返回零值（enabled=false）；
// 解析失败（库里被手工写坏）同样视为未开启，并返回错误供调用方记日志。
func (c AutoConfigJSON) AutoReply() (AutoReplyConfig, error) {
	return decodeSub[AutoReplyConfig](c, AutoConfigKeyAutoReply)
}

// AutoPost 解析 auto_post 子对象，语义同 AutoReply。
func (c AutoConfigJSON) AutoPost() (AutoPostConfig, error) {
	return decodeSub[AutoPostConfig](c, AutoConfigKeyAutoPost)
}

// decodeSub 解析某个子对象：未配置返回零值，解析失败返回零值 + 错误。
func decodeSub[T any](c AutoConfigJSON, key string) (T, error) {
	var cfg T
	raw, ok := c[key]
	if !ok || len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		var zero T
		return zero, err
	}
	return cfg, nil
}

// AutoReplyConfig 自动回复配置（auto_config.auto_reply）。
//
// 帖子候选：hot >= PostHotMin；评论候选：like_count >= CommentLikeMin 或
// reply_count >= CommentReplyMin（满足任一即可，阈值 0 表示该条件不参与）。
// 只看最近 WindowHours 小时内发布的内容；MaxPerHour 为自动回复每小时上限（0=不单独限制），
// 同时仍受机器人的 max_replies_per_hour / min_interval_sec 约束。
type AutoReplyConfig struct {
	Enabled         bool     `json:"enabled"`
	Targets         []string `json:"targets"` // post / comment
	PostHotMin      int      `json:"post_hot_min"`
	CommentLikeMin  int      `json:"comment_like_min"`
	CommentReplyMin int      `json:"comment_reply_min"`
	WindowHours     int      `json:"window_hours"`
	MaxPerHour      int      `json:"max_per_hour"`
}

// HasTarget 报告是否启用了某类目标。
func (c AutoReplyConfig) HasTarget(target string) bool {
	for _, t := range c.Targets {
		if t == target {
			return true
		}
	}
	return false
}

// AutoPostConfig 全局机器人自动发帖配置（auto_config.auto_post，#101）。
//
// 每轮到期判定：当前小时在 ActiveHours 内、距上次发帖尝试 >= IntervalMin 分钟、
// 当日（服务器本地时区自然日）尝试次数 < DailyMax；失败也计入（与回复链路"失败也算已触发"一致）。
// Topics 为空时用平台默认热点关键词（conf aiagent.autonomy.default_hot_topics）。
// RequireReview 为草稿审核预留（#104），本期只能为 false。
type AutoPostConfig struct {
	Enabled       bool     `json:"enabled"`
	IntervalMin   int      `json:"interval_min"`
	DailyMax      int      `json:"daily_max"`
	ActiveHours   []int    `json:"active_hours"` // [start, end) 小时，start > end 表示跨零点；空 = 全天
	Topics        []string `json:"topics"`
	RequireReview bool     `json:"require_review"`
}

// ActiveAt 报告 hour（0-23）是否在活跃时段内。
func (c AutoPostConfig) ActiveAt(hour int) bool {
	if len(c.ActiveHours) != 2 {
		return true
	}
	start, end := c.ActiveHours[0], c.ActiveHours[1]
	if start <= end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

// AutoConfigPatch auto_config 的顶层 key 合并补丁（UpdateFields 的 "auto_config" 字段值）。
//
// 仓储实现为 `auto_config || patch`（jsonb 顶层合并，原子执行）：只覆盖补丁里出现的 key，
// 不读改写整列，并发修改不同子对象（auto_reply / auto_post）互不覆盖。
type AutoConfigPatch map[string]json.RawMessage
