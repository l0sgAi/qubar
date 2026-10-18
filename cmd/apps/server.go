package apps

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/router"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/elasticsearch"
	"interestBar/pkg/server/storage/redis"
	s3storage "interestBar/pkg/server/storage/s3"
	emailutil "interestBar/pkg/util/email"
	"interestBar/pkg/util/password"
)

// Run 启动顺序：配置/日志 → 存储与外部客户端 → 异步链路（Redpanda / Lua / syncer）
// → 路由并阻塞服务 → 收到信号后关停。各阶段内部顺序与错误处理见对应函数。
func Run(configPath, bootstrapPath string) {
	initCore(configPath, bootstrapPath)
	initStorage()
	initPipelines()
	serve()
	shutdown()
}

// initCore 加载配置、应用密码哈希参数、初始化日志并输出配置安全告警。
func initCore(configPath, bootstrapPath string) {
	// 1. Init Config (优先 Nacos，不可用时回退本地文件)
	conf.InitConfig(configPath, bootstrapPath)

	// 1.5 应用密码哈希参数（从 conf.Security.PasswordHash 注入）。
	// 任一字段为 0 时 password 包内部回退到默认值。
	ph := conf.Config.Security.PasswordHash
	password.SetParams(password.Params{
		Time:    ph.Time,
		Memory:  ph.Memory,
		Threads: ph.Threads,
		KeyLen:  ph.KeyLen,
		SaltLen: ph.SaltLen,
	})

	// 2. Init Logger
	logger.InitLogger()
	for _, w := range conf.Config.SecurityWarnings() {
		logger.Log.Warn("[config] " + w)
	}
}

// initStorage 初始化存储与外部客户端：DB / Redis / Sa-Token / S3 失败即 Fatal，
// Elasticsearch / Mailtrap 失败降级运行。
func initStorage() {
	// 3. Init DB
	pgsql.InitDB()

	// 4. Init Redis for caching
	redisAddr := fmt.Sprintf("%s:%d", conf.Config.Redis.Host, conf.Config.Redis.Port)
	if err := redis.InitRedis(redisAddr, conf.Config.Redis.Password, conf.Config.Redis.D); err != nil {
		logger.Log.Fatal("Failed to initialize Redis: " + err.Error())
	}
	logger.Log.Info("Redis initialized successfully for caching")

	// 5. Init Sa-Token (includes Redis connection)
	if err := auth.InitSaToken(); err != nil {
		logger.Log.Fatal("Failed to initialize Sa-Token: " + err.Error())
	}

	// 6. Init S3 Client for file storage
	if err := s3storage.InitS3Client(); err != nil {
		logger.Log.Fatal("Failed to initialize S3 client: " + err.Error())
	}

	// 7. Init Elasticsearch for full-text search
	if err := elasticsearch.InitElasticsearch(); err != nil {
		logger.Log.Warn("Failed to initialize Elasticsearch: " + err.Error())
		logger.Log.Info("Running without Elasticsearch search functionality")
	}

	// 7.5 Init Mailtrap email client
	if err := emailutil.InitEmail(); err != nil {
		logger.Log.Warn("Failed to initialize Mailtrap email client: " + err.Error())
		logger.Log.Info("Running without email sending functionality")
	}
}

// serve 初始化路由并启动 Web 服务，阻塞直到收到 SIGINT/SIGTERM。
func serve() {
	// 9. Init Router
	r := router.InitRouter()

	// 10. Run Server
	addr := fmt.Sprintf(":%d", conf.Config.Server.Port)
	logger.Log.Info("Server starting on " + addr)

	// Spin() 内部处理 SIGINT/SIGTERM 并优雅关停（阻塞直到收到信号）。
	r.Spin()
}
