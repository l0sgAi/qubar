package redpanda

import (
	"context"
	"fmt"
	"time"

	"interestBar/pkg/logger"
)

// 读错误退避参数。
//
// kafka-go 的 ReadMessage/FetchMessage 在无数据时阻塞而非返回错误，
// 因此读循环拿到的错误都是真实的 broker/网络故障（如 Redpanda 重启时拨号超时）。
// 旧实现对含 "timeout" 的错误睡 30 分钟，会让整个 topic 停摆；改为有界指数退避。
const (
	readBackoffMin = time.Second
	readBackoffMax = 30 * time.Second
)

// readBackoff 读错误的有界指数退避：1s 起倍增，封顶 30s；成功读取后 Reset。
// 非并发安全，每个读循环各持一个。
type readBackoff struct {
	cur time.Duration
}

// Next 返回本次应等待的时长，并推进到下一档。
func (b *readBackoff) Next() time.Duration {
	if b.cur < readBackoffMin {
		b.cur = readBackoffMin
		return b.cur
	}
	b.cur *= 2
	if b.cur > readBackoffMax {
		b.cur = readBackoffMax
	}
	return b.cur
}

// Reset 成功读取后归零，下次出错从 1s 重新开始。
func (b *readBackoff) Reset() {
	b.cur = 0
}

// waitAfterReadError 记录读错误并按退避等待。
// 返回 false 表示 ctx 已取消（关停），调用方应退出读循环。
func waitAfterReadError(ctx context.Context, b *readBackoff, consumer string, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	d := b.Next()
	logger.Log.Warn(fmt.Sprintf("Failed to read %s message, retrying in %v: %s", consumer, d, err.Error()))
	return sleepOrDone(ctx, d)
}
