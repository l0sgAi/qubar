package redis

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// 私信发送频率限制（#242，设计见 docs/message-design.md 第十二节）：三个固定窗口计数，一次 EVALSHA 原子完成
// 「检查 → 占用」，多实例共享同一组 key，并发请求不会突破上限。
//
// 计数 +1 后若 key 没有 TTL（第一次计数，或异常残留的无 TTL key）就设 TTL，窗口从第一次计数起算；
// 不对已有 TTL 的 key 续期（否则持续发送时窗口永不到期，见 docs/design/design-rate-limit-otp-hardening.md §2.5）。

// acquireMessageSendRateScript 检查并占用一次发送配额。
//
// KEYS[1] = message:rate:min:{sender}
// KEYS[2] = message:rate:day:{sender}
// KEYS[3] = message:rate:newconv:{sender}
// ARGV[1] = 每分钟上限；ARGV[2] = 每天上限；ARGV[3] = 每天新会话上限
// ARGV[4] = 每分钟窗口毫秒；ARGV[5] = 每天窗口毫秒
//
// 返回 {exceeded, newconv_reserved}：exceeded 0 = 放行（每分钟、每天计数已 +1），1 = 超出每分钟上限，
// 2 = 超出每天上限（超限时什么都不写）；放行时新会话计数未满则 +1 并返回 newconv_reserved = 1，已满不动返回 0。
const acquireMessageSendRateScript = `
local function count(k)
    return tonumber(redis.call('GET', k) or '0')
end
local function incr(k, ttl)
    redis.call('INCR', k)
    if redis.call('PTTL', k) < 0 then
        redis.call('PEXPIRE', k, ttl)
    end
end
if count(KEYS[1]) >= tonumber(ARGV[1]) then
    return {1, 0}
end
if count(KEYS[2]) >= tonumber(ARGV[2]) then
    return {2, 0}
end
incr(KEYS[1], ARGV[4])
incr(KEYS[2], ARGV[5])
local reserved = 0
if count(KEYS[3]) < tonumber(ARGV[3]) then
    incr(KEYS[3], ARGV[5])
    reserved = 1
end
return {0, reserved}
`

// releaseMessageSendRateScript 退还占用：每个 key 存在且 > 0 时 -1，不改 TTL。
// key 已过期（窗口已重置）时不写，避免 DECR 凭空建出一个 -1 且没有 TTL 的 key。
//
// KEYS = 要退还的计数 key（1~3 个）
const releaseMessageSendRateScript = `
for _, k in ipairs(KEYS) do
    if tonumber(redis.call('GET', k) or '0') > 0 then
        redis.call('DECR', k)
    end
end
return 0
`

var (
	acquireMessageSendRate = redis.NewScript(acquireMessageSendRateScript)
	releaseMessageSendRate = redis.NewScript(releaseMessageSendRateScript)
)

// MessageSendRateExceeded 私信发送频率判定结果里的超限窗口。
type MessageSendRateExceeded int

const (
	// MessageSendRateOK 未超限，已占用配额。
	MessageSendRateOK MessageSendRateExceeded = iota
	// MessageSendRatePerMinute 超出每分钟上限。
	MessageSendRatePerMinute
	// MessageSendRatePerDay 超出每天上限。
	MessageSendRatePerDay
)

// AcquireMessageSendRate 原子地检查并占用 senderID 的一次私信发送配额（每分钟 / 每天各 +1），
// 同时在新会话计数未满时预占 1 个新会话名额（newConvReserved = true）。超限时什么都不写。
// 上限由调用方传入（已兜底为正数）。
func AcquireMessageSendRate(
	ctx context.Context, senderID uuid.UUID, perMinute, perDay, newConvPerDay int,
) (exceeded MessageSendRateExceeded, newConvReserved bool, err error) {
	id := senderID.String()
	keys := []string{GetMessageRateMinuteKey(id), GetMessageRateDayKey(id), GetMessageRateNewConvKey(id)}
	res, err := acquireMessageSendRate.Run(ctx, Client, keys,
		perMinute, perDay, newConvPerDay,
		MessageRateMinuteWindow.Milliseconds(), MessageRateDayWindow.Milliseconds(),
	).Int64Slice()
	if err != nil {
		return MessageSendRateOK, false, fmt.Errorf("failed to acquire message send rate: %w", err)
	}
	if len(res) != 2 {
		return MessageSendRateOK, false, fmt.Errorf("failed to acquire message send rate: unexpected reply %v", res)
	}
	return MessageSendRateExceeded(res[0]), res[1] == 1, nil
}

// ReleaseMessageSendRate 退还 AcquireMessageSendRate 占用的配额：message 为 true 时退还每分钟 / 每天计数，
// newConv 为 true 时退还预占的新会话名额。两者都为 false 时不访问 Redis。
func ReleaseMessageSendRate(ctx context.Context, senderID uuid.UUID, message, newConv bool) error {
	id := senderID.String()
	keys := make([]string, 0, 3)
	if message {
		keys = append(keys, GetMessageRateMinuteKey(id), GetMessageRateDayKey(id))
	}
	if newConv {
		keys = append(keys, GetMessageRateNewConvKey(id))
	}
	if len(keys) == 0 {
		return nil
	}
	if err := releaseMessageSendRate.Run(ctx, Client, keys).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to release message send rate: %w", err)
	}
	return nil
}
