// Package infrastructure 提供 follow 领域基础设施层实现：PostgreSQL 持久化。
//
// 文件按职责拆分：follow_relation_repo_pg.go（关注 / 取关 / 关系查询）、
// follow_stat_repo_pg.go（计数读取 / 重算）。新增能力以新增文件为主。
package infrastructure

import (
	"interestBar/pkg/domains/follow/domain"

	"gorm.io/gorm"
)

// followRepoPG 基于 GORM 的 follow 仓储，同时实现 RelationRepository 与 StatRepository。
type followRepoPG struct {
	db *gorm.DB
}

// NewRelationRepository 构造关注关系仓储。
func NewRelationRepository(db *gorm.DB) domain.RelationRepository {
	return &followRepoPG{db: db}
}

// NewStatRepository 构造关注计数仓储。
func NewStatRepository(db *gorm.DB) domain.StatRepository {
	return &followRepoPG{db: db}
}
