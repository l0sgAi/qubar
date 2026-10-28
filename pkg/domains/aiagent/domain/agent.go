// Package domain 存放 aiagent 领域的纯领域模型：实体、值对象、常量、
// Repository 接口与哨兵错误。
//
// 依赖规则：本包不得 import 任何 gorm/redis/hertz 等基础设施或框架库，
// 也不得 import 其他领域包（domains/circle 等）。DDL 见 docs/pgsql-ddl/ai-agent.md。
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// AiAgent AI 回复机器人聚合根（表 domains.ai_agent）。
//
// CircleID == nil 为平台全局机器人（超管经 /agent/* 维护，参与回复触发链路）；
// CircleID 非 nil 为圈子级机器人（该圈 owner/admin 经 /circle/agent/* 维护，
// 创建后不可变，只回复本圈帖子）。跨作用域互不可见（查询侧守卫）。
type AiAgent struct {
	ID                uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Name              string         `json:"name" gorm:"column:name;type:varchar(50);not null"`
	AvatarURL         string         `json:"avatar_url,omitempty" gorm:"column:avatar_url;type:varchar(500)"`
	LinkedUserID      uuid.UUID      `json:"linked_user_id" gorm:"column:linked_user_id;type:uuid;not null"`    // 机器人以该系统用户身份发评论
	CircleID          *uuid.UUID     `json:"circle_id,omitempty" gorm:"column:circle_id;type:uuid"`             // 绑定圈子ID;nil=平台全局机器人
	CreatorID         uuid.UUID      `json:"creator_id" gorm:"column:creator_id;type:uuid"`                     // 创建者用户ID(审计;存量行/全局机器人为零值)
	APIProtocol       string         `json:"api_protocol" gorm:"column:api_protocol;type:varchar(20);not null"` // openai/anthropic/gemini/ollama（应用层白名单）
	BaseURL           string         `json:"base_url,omitempty" gorm:"column:base_url;type:varchar(500);not null;default:''"`
	APIKeyEnc         string         `json:"-" gorm:"column:api_key;type:varchar(512)"` // AES-GCM 密文，永不 JSON 回显
	Model             string         `json:"model" gorm:"column:model;type:varchar(100);not null"`
	LLMParams         LLMParamsJSON  `json:"llm_params" gorm:"column:llm_params;type:jsonb;not null;default:'{}'::jsonb"`
	SystemPrompt      string         `json:"system_prompt,omitempty" gorm:"column:system_prompt;type:text;not null;default:''"`
	FilterPrompt      string         `json:"filter_prompt,omitempty" gorm:"column:filter_prompt;type:text;not null;default:''"` // 回复判定条件（空=不判定直接回复，仅关键词触发生效）
	TriggerMode       TriggerMode    `json:"trigger_mode" gorm:"column:trigger_mode;type:smallint;not null;default:1"`
	TriggerKeywords   KeywordsJSON   `json:"trigger_keywords" gorm:"column:trigger_keywords;type:jsonb;not null;default:'[]'::jsonb"`
	MaxRepliesPerHour int            `json:"max_replies_per_hour" gorm:"column:max_replies_per_hour;not null;default:30"`   // 0=不限
	MinIntervalSec    int            `json:"min_interval_sec" gorm:"column:min_interval_sec;not null;default:60"`           // 0=不限
	AutoConfig        AutoConfigJSON `json:"auto_config" gorm:"column:auto_config;type:jsonb;not null;default:'{}'::jsonb"` // 自主行为配置(auto_reply 等子对象，与 TriggerMode 互不影响)
	Status            int16          `json:"status" gorm:"column:status;type:smallint;not null;default:1"`
	Deleted           int16          `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime        time.Time      `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime        time.Time      `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (AiAgent) TableName() string { return "domains.ai_agent" }

// 机器人状态常量。
const (
	AgentStatusDisabled = 0 // 停用
	AgentStatusEnabled  = 1 // 启用
)

// MaxAgentsPerCircle 每圈可创建的机器人数量上限。
// 本期硬编码；如需运营可调再提升为 conf 配置项。
const MaxAgentsPerCircle = 5

// TriggerMode 触发模式枚举（trigger_mode 列，smallint）。
//
// 定义为类型化枚举而非裸 int 常量，编码与语义的映射集中在 String/Valid；
// 与 int16 字段比较/赋值因底层类型相同可直接进行（Go 可赋值性规则）。
type TriggerMode int16

// 触发模式枚举值。
const (
	TriggerModeAllPost TriggerMode = 1 // 全部新帖（agent-reply 链路 P2 待实现，本期不生效）
	TriggerModeKeyword TriggerMode = 2 // 评论关键词触发
	TriggerModeManual  TriggerMode = 3 // 管理员手动触发
)

// String 返回触发模式的语义名（未知编码返回 "unknown"）。
func (m TriggerMode) String() string {
	switch m {
	case TriggerModeAllPost:
		return "all_post"
	case TriggerModeKeyword:
		return "keyword"
	case TriggerModeManual:
		return "manual"
	default:
		return "unknown"
	}
}

// Valid 报告触发模式编码是否合法（1-3）。
func (m TriggerMode) Valid() bool {
	return m >= TriggerModeAllPost && m <= TriggerModeManual
}

// API 协议常量。当前仅 openai/anthropic 开放入库（application.validateProtocol 白名单）；
// gemini/ollama 为 P2 预留，实现前仅作常量保留。
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	ProtocolGemini    = "gemini"
	ProtocolOllama    = "ollama"
)

// LLMParamsJSON 通用 LLM 参数（jsonb map，如 {"temperature":0.7,"top_p":1}）。
//
// 实现 sql.Scanner / driver.Valuer，语义与 post.MediaExtraJSON 一致。
type LLMParamsJSON map[string]interface{}

// Scan 实现 sql.Scanner 接口。
func (p *LLMParamsJSON) Scan(value interface{}) error {
	if value == nil {
		*p = make(LLMParamsJSON)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("LLMParamsJSON: cannot scan non-[]byte value")
	}
	if string(bytes) == "{}" {
		*p = make(LLMParamsJSON)
		return nil
	}
	return json.Unmarshal(bytes, p)
}

// Value 实现 driver.Valuer 接口。
func (p LLMParamsJSON) Value() (driver.Value, error) {
	if len(p) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}

// KeywordsJSON 触发关键词列表（jsonb string 数组）。
type KeywordsJSON []string

// Scan 实现 sql.Scanner 接口。
func (k *KeywordsJSON) Scan(value interface{}) error {
	if value == nil {
		*k = make(KeywordsJSON, 0)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("KeywordsJSON: cannot scan non-[]byte value")
	}
	if string(bytes) == "{}" {
		*k = make(KeywordsJSON, 0)
		return nil
	}
	return json.Unmarshal(bytes, k)
}

// Value 实现 driver.Valuer 接口。
func (k KeywordsJSON) Value() (driver.Value, error) {
	if len(k) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(k)
}
