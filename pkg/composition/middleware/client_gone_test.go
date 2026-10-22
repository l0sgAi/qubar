package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type ctxKey struct{}

// TestDetachClientCancel 断开（连接 ctx 取消）只经 ClientGone 通知，下游 ctx 不被取消且保留原 value。
func TestDetachClientCancel(t *testing.T) {
	connCtx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "v"))
	defer cancel()

	var downstream context.Context
	c := app.NewContext(0)
	c.SetHandlers(app.HandlersChain{
		DetachClientCancel(),
		func(ctx context.Context, _ *app.RequestContext) { downstream = ctx },
	})
	c.Next(connCtx)

	if downstream == nil {
		t.Fatal("downstream handler not called")
	}
	if downstream.Value(ctxKey{}) != "v" {
		t.Fatal("downstream ctx should keep values")
	}
	gone := ClientGone(downstream)
	if gone == nil {
		t.Fatal("ClientGone should be set by middleware")
	}

	cancel()
	select {
	case <-gone:
	case <-time.After(time.Second):
		t.Fatal("ClientGone should fire after connection ctx canceled")
	}
	if downstream.Err() != nil {
		t.Fatalf("downstream ctx should not be canceled, got %v", downstream.Err())
	}
}

// TestClientGoneWithoutMiddleware 未挂中间件时返回 nil（select 永不触发，回落写失败检测）。
func TestClientGoneWithoutMiddleware(t *testing.T) {
	if ClientGone(context.Background()) != nil {
		t.Fatal("ClientGone should be nil without middleware")
	}
}
