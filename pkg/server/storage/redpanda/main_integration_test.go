//go:build integration

package redpanda_test

import (
	"testing"

	"interestBar/pkg/logger"
	"interestBar/pkg/shared/testkit"

	"go.uber.org/zap"
)

// 集成测试构建下本包唯一的 TestMain（单元测试构建用 main_test.go）。
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop() // 包内单元测试依赖；testkit 加载配置时替换为 Warn 级日志
	testkit.Main(m)
}
