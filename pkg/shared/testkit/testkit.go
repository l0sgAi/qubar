// Package testkit 是集成测试（//go:build integration，make test-integration）的公共夹具：
// 连接 deploy/compose/dev 开发栈（make dev-up），初始化基础设施层依赖的全局单例
// （conf.Config / logger.Log / pgsql.DB / redis.Client / Redpanda producer），并提供数据隔离与清理助手。
//
// 用法：
//
//	func TestMain(m *testing.M) { testkit.Main(m) }
//
//	func TestXxx(t *testing.T) {
//		t.Parallel()
//		db := testkit.PG(t)    // 依赖不可达时 Skip（QUBAR_TEST_REQUIRE_STACK=1 时直接失败）
//		testkit.Redis(t)
//		id := sharedomain.NewID()
//		testkit.CleanupRows(t, "domains.post", "id", id)
//	}
//
// 隔离约定（同一个开发栈上可并行、可反复执行）：
//   - PG：每条测试数据用 sharedomain.NewID() 生成随机 UUIDv7，并用 CleanupRows 在 t.Cleanup 删除；
//     断言只针对自己插入的 ID，不做全表计数。
//   - Redis：默认使用独立库 15（QUBAR_TEST_REDIS_DB 覆盖），不碰 make dev-run 用的 0 号库；
//     key 都由随机 ID 拼出，用 CleanupKeys 删除。
//   - Redpanda：每个测试进程把全部 topic / consumer group 改名为 <原名>_it_<runID> 并预先创建，
//     进程结束（Main）时删除，不会与开发中的应用或其他测试进程互相消费。
package testkit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 环境变量。配置项本身仍可用 QUBAR_*（如 QUBAR_PGSQL_PORT）覆盖，见 pkg/conf/env.go。
const (
	// EnvConfig 配置文件路径，默认 <仓库根>/configs/config.dev.yaml。
	EnvConfig = "QUBAR_TEST_CONFIG"
	// EnvRequireStack =1 时依赖不可达直接失败而不是 Skip（CI 用，防止整套测试被静默跳过）。
	EnvRequireStack = "QUBAR_TEST_REQUIRE_STACK"
	// EnvRedisDB 测试使用的 Redis 库号，默认 DefaultRedisDB。
	EnvRedisDB = "QUBAR_TEST_REDIS_DB"
)

// DefaultRedisDB 测试默认 Redis 库号（开发应用用 0 号库）。
const DefaultRedisDB = 15

const dialTimeout = 3 * time.Second

var (
	confOnce sync.Once
	confErr  error

	pgOnce sync.Once
	pgErr  error

	redisOnce sync.Once
	redisErr  error

	rpOnce   sync.Once
	rpErr    error
	rpTopics []string
	rpGroups []string

	// runID 本进程唯一后缀（UUIDv7 末 12 位随机部分），用于 Redpanda topic / group 改名。
	runID = func() string { s := sharedomain.NewID().String(); return s[len(s)-12:] }()
)

// Main 在包的 TestMain 中调用：运行测试，结束后删除本进程创建的 Redpanda topic / group 并关闭连接。
func Main(m *testing.M) {
	code := m.Run()
	teardown()
	os.Exit(code)
}

// RunID 返回本测试进程的唯一后缀。
func RunID() string { return runID }

// PG 返回连接开发栈 PostgreSQL（qubar_web_app 运行时角色）的 *gorm.DB，同时设置 pgsql.DB 全局单例。
func PG(t testing.TB) *gorm.DB {
	t.Helper()
	requireConfig(t)
	pgOnce.Do(func() { pgErr = openPG() })
	if pgErr != nil {
		unavailable(t, "postgres", pgErr)
	}
	return pgsql.DB
}

// Redis 连接开发栈 Redis（独立库号）并预加载全部 Lua 脚本，设置 redis.Client 全局单例。
func Redis(t testing.TB) {
	t.Helper()
	requireConfig(t)
	redisOnce.Do(func() { redisErr = openRedis() })
	if redisErr != nil {
		unavailable(t, "redis", redisErr)
	}
}

// Redpanda 把 conf.Config.Redpanda 的全部 topic / consumer group 改为本进程专用名称、预先创建 topic，
// 并初始化全部 producer。返回 broker 列表。
func Redpanda(t testing.TB) []string {
	t.Helper()
	requireConfig(t)
	rpOnce.Do(func() { rpErr = openRedpanda() })
	if rpErr != nil {
		unavailable(t, "redpanda", rpErr)
	}
	return conf.Config.Redpanda.Brokers
}

// CleanupRows 在测试结束时删除 table 中 column IN values 的行。
// 运行时角色只有 DML 权限（无 TRUNCATE），这里按 ID 精确删除，不影响其他并行测试的数据。
func CleanupRows(t testing.TB, table, column string, values ...any) {
	t.Helper()
	db := PG(t)
	t.Cleanup(func() {
		if len(values) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// table / column 来自测试代码常量，非用户输入
		sql := fmt.Sprintf("DELETE FROM %s WHERE %s IN ?", table, column)
		if err := db.WithContext(ctx).Exec(sql, values).Error; err != nil {
			t.Errorf("cleanup %s: %v", table, err)
		}
	})
}

// CleanupKeys 在测试结束时删除 Redis key。
func CleanupKeys(t testing.TB, keys ...string) {
	t.Helper()
	Redis(t)
	t.Cleanup(func() {
		if len(keys) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := redispkg.Client.Del(ctx, keys...).Err(); err != nil {
			t.Errorf("cleanup redis keys: %v", err)
		}
	})
}

// Eventually 每 interval 调用 cond 直到返回 true，超过 timeout 则 Fatal（用于等待异步消费者落库）。
func Eventually(t testing.TB, timeout, interval time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v: %s", timeout, msg)
		}
		time.Sleep(interval)
	}
}

// ===== 内部 =====

func unavailable(t testing.TB, dep string, err error) {
	t.Helper()
	if os.Getenv(EnvRequireStack) == "1" {
		t.Fatalf("集成测试依赖 %s 不可用：%v", dep, err)
	}
	t.Skipf("跳过：集成测试依赖 %s 不可用（先 make dev-up；%s=1 时改为失败）：%v", dep, EnvRequireStack, err)
}

func requireConfig(t testing.TB) {
	t.Helper()
	confOnce.Do(func() { confErr = loadConfig() })
	if confErr != nil {
		t.Fatalf("加载集成测试配置失败：%v", confErr)
	}
}

func loadConfig() (err error) {
	path := os.Getenv(EnvConfig)
	if path == "" {
		root, rerr := repoRoot()
		if rerr != nil {
			return rerr
		}
		path = filepath.Join(root, "configs", "config.dev.yaml")
	}
	// conf.InitConfig 出错时 panic（启动语义），测试里转成 error
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("load %s: %v", path, r)
		}
	}()
	conf.InitConfig(path, "")
	if err := checkLocal(conf.Config); err != nil {
		return err
	}

	// 只输出 Warn 以上，避免基础设施层的 Info 日志淹没测试输出
	encCfg := zap.NewDevelopmentEncoderConfig()
	logger.Log = zap.New(zapcore.NewCore(zapcore.NewConsoleEncoder(encCfg), zapcore.AddSync(os.Stderr), zapcore.WarnLevel))
	return nil
}

// checkLocal 拒绝连接非本机地址或 15432（ssh -L 到生产库的本地端口），防止 QUBAR_* 覆盖误指向生产。
func checkLocal(c *conf.AppConfig) error {
	if c.Pgsql.Port == "15432" {
		return errors.New("pgsql.port=15432 是 ssh 隧道到生产库的端口，集成测试拒绝连接")
	}
	hosts := []string{c.Pgsql.Path, c.Redis.Host}
	for _, b := range c.Redpanda.Brokers {
		h, _, err := net.SplitHostPort(b)
		if err != nil {
			return fmt.Errorf("invalid redpanda broker %q: %w", b, err)
		}
		hosts = append(hosts, h)
	}
	for _, h := range hosts {
		if !isLoopback(h) {
			return fmt.Errorf("集成测试只连接本机开发栈，拒绝非本机地址 %q", h)
		}
	}
	return nil
}

func isLoopback(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// repoRoot 从工作目录向上找 go.mod（go test 的工作目录是被测包目录）。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above working directory")
		}
		dir = parent
	}
}

func openPG() error {
	p := conf.Config.Pgsql
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s connect_timeout=%d %s",
		p.Path, p.Username, p.Password, p.DbName, p.Port, int(dialTimeout.Seconds()), p.Config)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(20)

	// 迁移是否已执行（make dev-up 会执行；单独起栈需 make dev-migrate）
	var post *string
	if err := db.Raw("SELECT to_regclass('domains.post')::text").Scan(&post).Error; err != nil {
		_ = sqlDB.Close()
		return err
	}
	if post == nil {
		_ = sqlDB.Close()
		return errors.New("domains.post 不存在：先 make dev-migrate")
	}
	pgsql.DB = db
	return nil
}

func openRedis() error {
	r := conf.Config.Redis
	dbIndex := DefaultRedisDB
	if v := os.Getenv(EnvRedisDB); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s=%q: %w", EnvRedisDB, v, err)
		}
		dbIndex = n
	}
	if err := redispkg.InitRedis(fmt.Sprintf("%s:%d", r.Host, r.Port), r.Password, dbIndex); err != nil {
		return err
	}
	for _, load := range []func() error{
		redispkg.InitLikeLuaScripts,
		redispkg.InitCollectLuaScripts,
		redispkg.InitViewLuaScripts,
		redispkg.InitHistoryLuaScripts,
		redispkg.InitHotLuaScripts,
	} {
		if err := load(); err != nil {
			return err
		}
	}
	return nil
}

func openRedpanda() error {
	rp := &conf.Config.Redpanda
	if len(rp.Brokers) == 0 {
		return errors.New("redpanda.brokers is empty")
	}
	conn, err := net.DialTimeout("tcp", rp.Brokers[0], dialTimeout)
	if err != nil {
		return err
	}
	_ = conn.Close()

	suffix := "_it_" + runID
	for _, p := range []*string{
		&rp.Topic, &rp.PostTopic, &rp.LikeEventTopic, &rp.CollectEventTopic, &rp.HistoryEventTopic,
		&rp.PostHotTopic, &rp.PostInteractionTopic, &rp.NoticeEventTopic,
	} {
		*p += suffix
		rpTopics = append(rpTopics, *p)
	}
	for _, p := range []*string{
		&rp.ConsumerGroup, &rp.PostConsumerGroup, &rp.LikeEventConsumerGroup, &rp.CollectEventConsumerGroup,
		&rp.HistoryEventConsumerGroup, &rp.PostHotConsumerGroup, &rp.PostInteractionConsumerGroup, &rp.NoticeEventConsumerGroup,
	} {
		*p += suffix
		rpGroups = append(rpGroups, *p)
	}
	// 秒级的点赞 / 收藏刷写间隔调到 1s，测试不必等 10s
	rp.LikeEventFlushInterval = 1
	rp.CollectEventFlushInterval = 1

	// 预建 topic：consumer 订阅不存在的 topic 会退避重试，拖慢测试
	topics := make([]kafka.TopicConfig, 0, len(rpTopics))
	for _, name := range rpTopics {
		topics = append(topics, kafka.TopicConfig{Topic: name, NumPartitions: 1, ReplicationFactor: 1})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := &kafka.Client{Addr: kafka.TCP(rp.Brokers...)}
	resp, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: topics})
	if err != nil {
		return fmt.Errorf("create topics: %w", err)
	}
	for name, terr := range resp.Errors {
		if terr != nil && !errors.Is(terr, kafka.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", name, terr)
		}
	}

	for _, initFn := range []func() error{
		redpanda.InitRedpandaProducer,
		redpanda.InitPostStatsProducer,
		redpanda.InitLikeEventProducer,
		redpanda.InitCollectEventProducer,
		redpanda.InitHistoryEventProducer,
		redpanda.InitPostHotProducer,
		redpanda.InitPostInteractionProducer,
		redpanda.InitNotificationEventProducer,
	} {
		if err := initFn(); err != nil {
			return err
		}
	}
	return nil
}

func teardown() {
	if len(rpTopics) > 0 {
		redpanda.StopLikeEventConsumerGlobal()
		redpanda.StopBatchConsumersGlobal()
		for _, closeFn := range []func() error{
			redpanda.CloseRedpandaProducer,
			redpanda.ClosePostStatsProducer,
			redpanda.CloseLikeEventProducer,
			redpanda.CloseCollectEventProducer,
			redpanda.CloseHistoryEventProducer,
			redpanda.ClosePostHotProducer,
			redpanda.ClosePostInteractionProducer,
			redpanda.CloseNotificationEventProducer,
		} {
			_ = closeFn()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		client := &kafka.Client{Addr: kafka.TCP(conf.Config.Redpanda.Brokers...)}
		deleteGroups(ctx, client)
		if _, err := client.DeleteTopics(ctx, &kafka.DeleteTopicsRequest{Topics: rpTopics}); err != nil {
			fmt.Fprintf(os.Stderr, "testkit: delete topics: %v\n", err)
		}
	}
	if redispkg.Client != nil {
		_ = redispkg.CloseRedis()
	}
	if pgsql.DB != nil {
		if sqlDB, err := pgsql.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

// deleteGroups 删除本进程的 consumer group。消费者停止后 reader 在后台 goroutine 里 Close，
// 要等进行中的 fetch 长轮询（MaxWait 默认 10s）结束才 LeaveGroup，期间删除返回 NonEmptyGroup，重试至多 ~20s；
// 从未加入过的 group 返回 GroupIdNotFound，忽略。
func deleteGroups(ctx context.Context, client *kafka.Client) {
	pending := rpGroups
	for attempt := 0; len(pending) > 0; attempt++ {
		resp, err := client.DeleteGroups(ctx, &kafka.DeleteGroupsRequest{GroupIDs: pending})
		if err != nil {
			fmt.Fprintf(os.Stderr, "testkit: delete consumer groups: %v\n", err)
			return
		}
		var retry []string
		for group, gerr := range resp.Errors {
			switch {
			case gerr == nil, errors.Is(gerr, kafka.GroupIdNotFound):
			case errors.Is(gerr, kafka.NonEmptyGroup) && attempt < 100:
				retry = append(retry, group)
			default:
				fmt.Fprintf(os.Stderr, "testkit: delete consumer group %s: %v\n", group, gerr)
			}
		}
		pending = retry
		if len(pending) > 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}
}
