//go:build integration

// follow_integration_test.go follow 域经 composition 装配后的端到端集成测试（#217）：真实 PG + Redis，
// 目标用户校验走 followUserChecker → user Facade（与 UserService.GetByID 同一判定）。
// 覆盖：普通用户 / AI 机器人账号可被关注、不存在 / 已禁用 / 已删除用户 404、访客与四种关系的 stats、
// 对方被禁用后仍可取关。
package composition

import (
	"errors"
	"testing"

	"interestBar/pkg/conf"
	followapp "interestBar/pkg/domains/follow/application"
	followdomain "interestBar/pkg/domains/follow/domain"
	followinfra "interestBar/pkg/domains/follow/infrastructure"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type followEnv struct {
	db  *gorm.DB
	svc followapp.FollowService
}

func setupFollow(t *testing.T) *followEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	deps := NewDeps()
	// 这里的用例不关心通知，发布器用空实现：同进程的其他用例会启动通知 consumer，真发事件的话通知行要等
	// flush 窗口才落库，晚于本用例的清理，会留在共享开发库里。生产装配（newFollowService + Redpanda 发布器）
	// 的端到端链路见 follow_notice_integration_test.go。
	svc := followapp.NewFollowService(
		followinfra.NewRelationRepository(db),
		followinfra.NewStatRepository(db),
		&followUserChecker{delegate: userapp.NewUserFacade(newUserService(deps))},
		func() int { return conf.Config.Follow.MaxFollowingOrDefault() },
		noFollowEvents{},
	)
	return &followEnv{db: db, svc: svc}
}

// insertUser 插入一个用户（role 0=普通 / 2=AI 机器人；status 1=正常；deleted 0=未删除），注册行、缓存与关注数据的清理。
func (e *followEnv) insertUser(t *testing.T, role, status, deleted int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := e.db.Exec(`INSERT INTO domains.users (id, username, email, role, status, deleted) VALUES (?, 'it217 follow', ?, ?, ?, ?)`,
		id, id.String()+"@it.qubar.local", role, status, deleted).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	testkit.CleanupRows(t, "domains.user_follow", "follower_id", id)
	testkit.CleanupRows(t, "domains.user_follow_stat", "user_id", id)
	testkit.CleanupKeys(t, redispkg.GetUserInfoKey(id))
	return id
}

func (e *followEnv) assertStats(t *testing.T, viewer, target uuid.UUID, following, follower int, relation followdomain.Relation) {
	t.Helper()
	vo, err := e.svc.GetStats(t.Context(), viewer, target)
	if err != nil {
		t.Fatalf("GetStats(%s -> %s): %v", viewer, target, err)
	}
	if vo.FollowingCount != following || vo.FollowerCount != follower || vo.Relation != relation {
		t.Errorf("stats(%s -> %s) = %+v, want following %d / follower %d / relation %d", viewer, target, vo, following, follower, relation)
	}
}

// 普通用户与 AI 机器人账号都可以被关注；stats 对访客公开，relation 覆盖四种状态。
func TestFollow_FollowUsersAndBotsWithStats(t *testing.T) {
	t.Parallel()
	e := setupFollow(t)
	ctx := t.Context()
	a, b, bot := e.insertUser(t, 0, 1, 0), e.insertUser(t, 0, 1, 0), e.insertUser(t, 2, 1, 0)

	e.assertStats(t, uuid.Nil, b, 0, 0, followdomain.RelationNone)

	vo, err := e.svc.Follow(ctx, a, b)
	if err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("follow user: vo=%+v err=%v, want relation 1", vo, err)
	}
	if vo, err = e.svc.Follow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("repeat follow: vo=%+v err=%v", vo, err)
	}
	if vo, err = e.svc.Follow(ctx, a, bot); err != nil || vo.Relation != followdomain.RelationFollowing {
		t.Fatalf("follow bot: vo=%+v err=%v, want relation 1", vo, err)
	}

	e.assertStats(t, uuid.Nil, b, 0, 1, followdomain.RelationNone) // 访客
	e.assertStats(t, b, b, 0, 1, followdomain.RelationNone)        // 查看自己
	e.assertStats(t, a, b, 0, 1, followdomain.RelationFollowing)
	e.assertStats(t, b, a, 2, 0, followdomain.RelationFollowedBy)
	e.assertStats(t, a, bot, 0, 1, followdomain.RelationFollowing)

	if vo, err = e.svc.Follow(ctx, b, a); err != nil || vo.Relation != followdomain.RelationMutual {
		t.Fatalf("follow back: vo=%+v err=%v, want relation 3", vo, err)
	}
	e.assertStats(t, a, b, 1, 1, followdomain.RelationMutual)

	if vo, err = e.svc.Unfollow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationFollowedBy {
		t.Fatalf("unfollow: vo=%+v err=%v, want relation 2", vo, err)
	}
	if vo, err = e.svc.Unfollow(ctx, a, b); err != nil || vo.Relation != followdomain.RelationFollowedBy {
		t.Fatalf("repeat unfollow: vo=%+v err=%v", vo, err)
	}
	e.assertStats(t, a, b, 1, 0, followdomain.RelationFollowedBy)
	e.assertStats(t, uuid.Nil, a, 1, 1, followdomain.RelationNone)

	if _, err = e.svc.Follow(ctx, a, a); !errors.Is(err, followdomain.ErrCannotFollowSelf) {
		t.Errorf("follow self: err = %v, want ErrCannotFollowSelf", err)
	}
}

// 不存在 / 已禁用 / 已删除的用户：关注与 stats 都按不存在处理，且不产生任何关注数据。
func TestFollow_MissingDisabledDeletedTargets(t *testing.T) {
	t.Parallel()
	e := setupFollow(t)
	ctx := t.Context()
	a := e.insertUser(t, 0, 1, 0)

	for name, target := range map[string]uuid.UUID{
		"missing":  sharedomain.NewID(),
		"disabled": e.insertUser(t, 0, 0, 0),
		"deleted":  e.insertUser(t, 0, 1, 1),
	} {
		if _, err := e.svc.Follow(ctx, a, target); !errors.Is(err, followdomain.ErrUserNotFound) {
			t.Errorf("%s: follow err = %v, want ErrUserNotFound", name, err)
		}
		if _, err := e.svc.GetStats(ctx, a, target); !errors.Is(err, followdomain.ErrUserNotFound) {
			t.Errorf("%s: stats err = %v, want ErrUserNotFound", name, err)
		}
	}
	e.assertStats(t, uuid.Nil, a, 0, 0, followdomain.RelationNone)
}

// 关注之后对方被禁用：仍然可以取关，计数随之回落。
func TestFollow_UnfollowDisabledUser(t *testing.T) {
	t.Parallel()
	e := setupFollow(t)
	ctx := t.Context()
	a, b := e.insertUser(t, 0, 1, 0), e.insertUser(t, 0, 1, 0)

	if _, err := e.svc.Follow(ctx, a, b); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if err := e.db.WithContext(ctx).Exec("UPDATE domains.users SET status = 0 WHERE id = ?", b).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if err := redispkg.Client.Del(ctx, redispkg.GetUserInfoKey(b)).Err(); err != nil {
		t.Fatalf("drop user cache: %v", err)
	}

	if _, err := e.svc.Follow(ctx, a, b); !errors.Is(err, followdomain.ErrUserNotFound) {
		t.Errorf("follow disabled user: err = %v, want ErrUserNotFound", err)
	}
	e.assertStats(t, uuid.Nil, a, 1, 0, followdomain.RelationNone)

	vo, err := e.svc.Unfollow(ctx, a, b)
	if err != nil || vo.Relation != followdomain.RelationNone {
		t.Fatalf("unfollow disabled user: vo=%+v err=%v, want relation 0", vo, err)
	}
	e.assertStats(t, uuid.Nil, a, 0, 0, followdomain.RelationNone)
}
