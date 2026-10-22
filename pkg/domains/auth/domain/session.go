package domain

import (
	"context"
	"time"
)

// 终端类型（客户端登录时传的 device 字段）。
//
// 上线多端同时在线（#106）之前，sa-token 的 device 就是这两个值本身；
// 之后 device 改为 "<type>:<device_id>"（见 TerminalDevice），已签发的旧 token 照常使用直到过期。
const (
	DeviceTypeWeb    = "web"
	DeviceTypeMobile = "mobile"
)

// TerminalDevice 返回传给 sa-token 的 device 名："<type>:<device_id>"，例如 "web:0192…"。
//
// sa-token 每个 (loginID, device) 只保存一个 token：同一终端重新登录替换自己的旧 token，
// 不同终端互不影响。deviceID 必须是校验过的标准 UUID（不含 ":"），否则会破坏 OAuth state 的解析。
func TerminalDevice(deviceType, deviceID string) string {
	return deviceType + ":" + deviceID
}

// TerminalMeta 是一个在线终端的元数据，随 token 写入会话索引。
type TerminalMeta struct {
	Type      string `json:"type"`       // 终端类型 web / mobile
	DeviceID  string `json:"device_id"`  // 客户端持久化的终端 UUID
	LoginTime int64  `json:"login_time"` // 登录时间（Unix 秒）
	IP        string `json:"ip"`         // 登录时的客户端 IP
	UA        string `json:"ua"`         // User-Agent 摘要（截断）
}

// SessionIndex 是用户在线终端的索引（由 infrastructure 提供 Redis 实现）。
//
// sa-token 只提供按 (loginID, device) 查 token 的映射，列出某账号全部 token 需要全库 SCAN；
// 这里自己维护一份按登录时间排序的 token 列表，用于终端上限与注销全部终端。
// 索引是软状态：可能残留已失效的 token（由调用方核对后 Remove），也可能因 TTL 到期漏掉仍在续期的 token。
type SessionIndex interface {
	// Add 登记 loginID 的一个 token（按写入时间排序），并把整个索引的 TTL 刷新为 ttl。
	Add(ctx context.Context, loginID, token string, meta TerminalMeta, ttl time.Duration) error
	// List 按登录时间从早到晚返回 loginID 索引里的全部 token；没有索引时返回空切片。
	List(ctx context.Context, loginID string) ([]string, error)
	// Remove 从索引里删除给定 token（含元数据），不存在的 token 忽略。
	Remove(ctx context.Context, loginID string, tokens ...string) error
	// Clear 删除 loginID 的整个索引。
	Clear(ctx context.Context, loginID string) error
}
