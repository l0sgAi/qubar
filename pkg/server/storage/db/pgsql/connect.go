package pgsql

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// DBHolder 持有 *gorm.DB 的引用，供 composition 层依赖注入使用。
//
// 过渡期：当前它只是读取全局单例 DB，避免领域包直接 import 全局变量；
// 后续重构会把它改造为真正的持有者（在 composition 层构造连接后注入）。
type DBHolder struct{}

// Get 返回当前 *gorm.DB 实例（即全局 DB）。
func (h *DBHolder) Get() *gorm.DB { return DB }

// InitDB initializes the database connection using PostgreSQL.
func InitDB() {
	p := conf.Config.Pgsql
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s %s",
		p.Path, p.Username, p.Password, p.DbName, p.Port, p.Config)

	var logMode gormlogger.Interface
	if p.LogMode == "debug" {
		logMode = gormlogger.Default.LogMode(gormlogger.Info)
	} else {
		logMode = gormlogger.Default.LogMode(gormlogger.Error)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logMode,
	})
	if err != nil {
		if logger.Log != nil {
			logger.Log.Error("Failed to connect to database: " + err.Error())
		} else {
			fmt.Println("Failed to connect to database: " + err.Error())
		}
		os.Exit(1)
	}

	// Connection Pool Configuration
	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(p.MaxIdleConns)
	sqlDB.SetMaxOpenConns(p.MaxOpenConns)

	// 表结构由 migrations/ 下的版本化迁移（golang-migrate）管理，发布时以 DB owner 角色执行。
	// 运行时连接用的是最小权限角色（如 qubar_web_app），并非表 owner，
	// 不具备 ALTER 权限，因此这里不做 AutoMigrate——这与 post/circle/user/
	// like/category 等领域的做法保持一致（它们也都只依赖 SQL 脚本建表）。
	DB = db
	if logger.Log != nil {
		logger.Log.Info("Database connection successful")
	}
}
