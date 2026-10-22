//go:build !integration

package redpanda

import (
	"os"
	"testing"

	"interestBar/pkg/logger"

	"go.uber.org/zap"
)

// 集成测试构建（-tags integration）的 TestMain 在 main_integration_test.go（外部测试包，接 testkit）。
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}
