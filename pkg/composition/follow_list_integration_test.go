//go:build integration

// follow_list_integration_test.go 关注 / 粉丝 / 互关列表与列表隐私开关经 composition 装配后的端到端集成测试（#218）：
// 真实 PG + Redis，用户简要信息走 followUserBriefReader → UserFacade.GetBriefs，隐私开关走
// followListVisibilityReader → user 域 SettingService（不直接查 user_setting 表）。
// 覆盖：私密时本人 / 他人 / 访客三种访问结果与 stats.list_visible、列表项的 relation 与已禁用用户过滤、
// 经真实游标翻页与作用域校验、取关后再关注的排序、互关列表、批量关系。
package composition

import (
	"errors"
	"slices"
	"testing"

	followapp "interestBar/pkg/domains/follow/application"
	followdomain "interestBar/pkg/domains/follow/domain"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

type followListEnv struct {
	*followEnv
	settings userapp.SettingService
}

func setupFollowList(t *testing.T) *followListEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	deps := NewDeps()
	settings := newUserSettingService(deps)
	// 不关心通知：发布器用空实现，原因见 setupFollow（follow_integration_test.go）。
	svc := newFollowServiceWithEvents(deps, userapp.NewUserFacade(newUserService(deps)), settings, noFollowEvents{})
	return &followListEnv{followEnv: &followEnv{db: db, svc: svc}, settings: settings}
}

// users 插入 n 个正常用户（清理见 insertUser），并注册设置行的清理。
func (e *followListEnv) users(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = e.insertUser(t, 0, 1, 0)
		testkit.CleanupRows(t, "domains.user_setting", "user_id", ids[i])
	}
	return ids
}

func (e *followListEnv) mustFollow(t *testing.T, follower, followee uuid.UUID) {
	t.Helper()
	if _, err := e.svc.Follow(t.Context(), follower, followee); err != nil {
		t.Fatalf("follow %s -> %s: %v", follower, followee, err)
	}
}

func (e *followListEnv) setVisibility(t *testing.T, userID uuid.UUID, v int) {
	t.Helper()
	if _, err := e.settings.UpdateSettings(t.Context(), userID, userapp.UpdateSettingsInput{FollowListVisibility: &v}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
}

func listUserIDs(vo *followapp.FollowListVO) []string {
	ids := make([]string, len(vo.Items))
	for i := range vo.Items {
		ids[i] = vo.Items[i].User.ID
	}
	return ids
}

func idStrings(ids ...uuid.UUID) []string {
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = ids[i].String()
	}
	return out
}

// 私密开关：他人与访客看两个列表都是 ErrFollowListPrivate，本人不受影响；计数、relation、互关列表不受影响。
func TestFollowList_PrivateListsOwnerOtherGuest(t *testing.T) {
	t.Parallel()
	e := setupFollowList(t)
	ctx := t.Context()
	u := e.users(t, 3)
	owner, friend, other := u[0], u[1], u[2]
	e.mustFollow(t, owner, friend)
	e.mustFollow(t, friend, owner)
	e.mustFollow(t, other, owner)

	lists := map[string]func(viewer uuid.UUID) (*followapp.FollowListVO, error){
		"following": func(v uuid.UUID) (*followapp.FollowListVO, error) { return e.svc.ListFollowing(ctx, v, owner, "", 0) },
		"followers": func(v uuid.UUID) (*followapp.FollowListVO, error) { return e.svc.ListFollowers(ctx, v, owner, "", 0) },
	}
	viewers := map[string]uuid.UUID{"owner": owner, "other": other, "guest": uuid.Nil}
	wantSize := map[string]int{"following": 1, "followers": 2}

	assertAccess := func(private bool) {
		t.Helper()
		for listName, list := range lists {
			for who, viewer := range viewers {
				vo, err := list(viewer)
				if private && who != "owner" {
					if !errors.Is(err, followdomain.ErrFollowListPrivate) {
						t.Errorf("private=%v %s by %s: err = %v, want ErrFollowListPrivate", private, listName, who, err)
					}
					continue
				}
				if err != nil || len(vo.Items) != wantSize[listName] {
					t.Errorf("private=%v %s by %s: vo=%+v err=%v, want %d items", private, listName, who, vo, err, wantSize[listName])
				}
			}
		}
		// stats：计数与 relation 始终公开，list_visible 跟随开关（本人恒为 true）
		for who, viewer := range viewers {
			vo, err := e.svc.GetStats(ctx, viewer, owner)
			if err != nil {
				t.Fatalf("stats by %s: %v", who, err)
			}
			wantRelation := followdomain.RelationNone
			if who == "other" {
				wantRelation = followdomain.RelationFollowing
			}
			if vo.FollowingCount != 1 || vo.FollowerCount != 2 || vo.Relation != wantRelation || vo.ListVisible != (!private || who == "owner") {
				t.Errorf("private=%v stats by %s = %+v", private, who, vo)
			}
		}
	}

	// 没有设置行：默认公开
	if s, err := e.settings.GetSettings(ctx, owner); err != nil || s.FollowListVisibility != 0 {
		t.Fatalf("default settings = %+v err=%v, want public", s, err)
	}
	assertAccess(false)

	e.setVisibility(t, owner, 1)
	assertAccess(true)
	// 互关列表只有本人能调，不受开关影响
	if vo, err := e.svc.ListMutual(ctx, owner, "", 0); err != nil || !slices.Equal(listUserIDs(vo), idStrings(friend)) {
		t.Errorf("mutual with private lists: vo=%+v err=%v, want [friend]", vo, err)
	}
	// 开关只影响自己的列表：owner 仍然能看别人公开的列表
	if vo, err := e.svc.ListFollowing(ctx, owner, other, "", 0); err != nil || len(vo.Items) != 1 {
		t.Errorf("owner viewing a public list: vo=%+v err=%v", vo, err)
	}

	e.setVisibility(t, owner, 0)
	assertAccess(false)
}

// 列表项：用户简要信息 + 关注时间 + 查看者与该用户的 relation；已禁用用户不出现，计数不调整。
func TestFollowList_ItemsRelationAndInactiveUsers(t *testing.T) {
	t.Parallel()
	e := setupFollowList(t)
	ctx := t.Context()
	u := e.users(t, 5)
	owner, viewer, a, b, gone := u[0], u[1], u[2], u[3], u[4]
	for _, target := range []uuid.UUID{a, b, gone, viewer} {
		e.mustFollow(t, owner, target)
	}
	e.mustFollow(t, viewer, a) // viewer 关注 a
	e.mustFollow(t, b, viewer) // b 关注 viewer
	e.mustFollow(t, viewer, gone)
	e.mustFollow(t, gone, viewer) // 互关，但 gone 之后被禁用

	if err := e.db.WithContext(ctx).Exec("UPDATE domains.users SET status = 0 WHERE id = ?", gone).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if err := redispkg.Client.Del(ctx, redispkg.GetUserInfoKey(gone)).Err(); err != nil {
		t.Fatalf("drop user cache: %v", err)
	}

	vo, err := e.svc.ListFollowing(ctx, viewer, owner, "", 0)
	if err != nil {
		t.Fatalf("ListFollowing: %v", err)
	}
	// 关注顺序 a, b, gone, viewer → 倒序 viewer, (gone 被过滤), b, a
	if !slices.Equal(listUserIDs(vo), idStrings(viewer, b, a)) || vo.HasMore || vo.NextCursor != "" {
		t.Fatalf("items = %v has_more=%v, want [viewer b a]", listUserIDs(vo), vo.HasMore)
	}
	wantRel := []followdomain.Relation{followdomain.RelationNone, followdomain.RelationFollowedBy, followdomain.RelationFollowing}
	for i, item := range vo.Items {
		if item.Relation != wantRel[i] || item.User.Username != "it217 follow" || item.FollowTime.IsZero() {
			t.Errorf("item %d = %+v, want relation %d with brief and follow_time", i, item, wantRel[i])
		}
	}
	// 关注时间倒序
	if !vo.Items[0].FollowTime.After(vo.Items[1].FollowTime) || !vo.Items[1].FollowTime.After(vo.Items[2].FollowTime) {
		t.Errorf("follow_time not descending: %v", vo.Items)
	}

	// 访客：relation 恒为 0
	if vo, err = e.svc.ListFollowing(ctx, uuid.Nil, owner, "", 0); err != nil || len(vo.Items) != 3 {
		t.Fatalf("guest list: vo=%+v err=%v", vo, err)
	}
	for _, item := range vo.Items {
		if item.Relation != followdomain.RelationNone {
			t.Errorf("guest relation with %s = %d, want 0", item.User.ID, item.Relation)
		}
	}

	// 计数不因过滤而调整：列表 3 人，关注数仍是 4
	e.assertStats(t, uuid.Nil, owner, 4, 0, followdomain.RelationNone)
	// 互关列表同样过滤已禁用用户
	if vo, err = e.svc.ListMutual(ctx, viewer, "", 0); err != nil || len(vo.Items) != 0 {
		t.Errorf("mutual of viewer: vo=%+v err=%v, want empty (the only mutual user is disabled)", vo, err)
	}
	// 已禁用用户自己的列表：404
	if _, err = e.svc.ListFollowers(ctx, viewer, gone, "", 0); !errors.Is(err, followdomain.ErrUserNotFound) {
		t.Errorf("list of a disabled user: err = %v, want ErrUserNotFound", err)
	}
}

// 用接口返回的游标翻完整个列表（真实 PG 时间精度下不重不漏）；游标不能跨列表 / 跨用户使用；取关后再关注排到最前。
func TestFollowList_CursorWalkScopeAndRefollowOrder(t *testing.T) {
	t.Parallel()
	e := setupFollowList(t)
	ctx := t.Context()
	u := e.users(t, 7)
	owner, others, fan := u[0], u[1:6], u[6]
	for _, other := range others {
		e.mustFollow(t, owner, other)
		e.mustFollow(t, other, owner)
	}
	e.mustFollow(t, fan, owner)

	walk := func(list func(cursor string) (*followapp.FollowListVO, error)) (ids []string, firstCursor string) {
		t.Helper()
		cursor := ""
		for range 10 {
			vo, err := list(cursor)
			if err != nil {
				t.Fatalf("list(%q): %v", cursor, err)
			}
			if len(vo.Items) > 2 {
				t.Fatalf("page has %d items, size 2", len(vo.Items))
			}
			ids = append(ids, listUserIDs(vo)...)
			if firstCursor == "" {
				firstCursor = vo.NextCursor
			}
			if !vo.HasMore {
				if vo.NextCursor != "" {
					t.Errorf("last page next_cursor = %q, want empty", vo.NextCursor)
				}
				return ids, firstCursor
			}
			cursor = vo.NextCursor
		}
		t.Fatal("list did not terminate")
		return nil, ""
	}
	following := func(c string) (*followapp.FollowListVO, error) {
		return e.svc.ListFollowing(ctx, uuid.Nil, owner, c, 2)
	}
	followers := func(c string) (*followapp.FollowListVO, error) {
		return e.svc.ListFollowers(ctx, uuid.Nil, owner, c, 2)
	}
	mutual := func(c string) (*followapp.FollowListVO, error) { return e.svc.ListMutual(ctx, owner, c, 2) }

	newestFirst := idStrings(others[4], others[3], others[2], others[1], others[0])
	gotFollowing, followingCursor := walk(following)
	if !slices.Equal(gotFollowing, newestFirst) {
		t.Errorf("following walk = %v, want %v", gotFollowing, newestFirst)
	}
	if got, _ := walk(followers); !slices.Equal(got, append(idStrings(fan), newestFirst...)) {
		t.Errorf("followers walk = %v, want fan then %v", got, newestFirst)
	}
	if got, _ := walk(mutual); !slices.Equal(got, newestFirst) {
		t.Errorf("mutual walk = %v, want %v", got, newestFirst)
	}

	// 游标作用域：关注列表的游标不能用在粉丝 / 互关列表或别人的关注列表上
	for name, misuse := range map[string]func() (*followapp.FollowListVO, error){
		"followers of same user": func() (*followapp.FollowListVO, error) { return followers(followingCursor) },
		"mutual of same user":    func() (*followapp.FollowListVO, error) { return mutual(followingCursor) },
		"following of another": func() (*followapp.FollowListVO, error) {
			return e.svc.ListFollowing(ctx, uuid.Nil, fan, followingCursor, 2)
		},
		"garbage": func() (*followapp.FollowListVO, error) { return following("not-a-cursor") },
	} {
		if _, err := misuse(); !errors.Is(err, followdomain.ErrInvalidCursor) {
			t.Errorf("cursor on %s: err = %v, want ErrInvalidCursor", name, err)
		}
	}

	// 取关最早关注的人再关注：排到最前
	if _, err := e.svc.Unfollow(ctx, owner, others[0]); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if got, _ := walk(following); !slices.Equal(got, newestFirst[:4]) {
		t.Errorf("following after unfollow = %v, want %v", got, newestFirst[:4])
	}
	e.mustFollow(t, owner, others[0])
	if got, _ := walk(following); !slices.Equal(got, append(idStrings(others[0]), newestFirst[:4]...)) {
		t.Errorf("following after refollow = %v, want others[0] first", got)
	}
}

// 批量关系：返回请求里每个 ID 与当前用户的关系，不存在的用户与自己为 0；超过 100 个被拒绝。
func TestFollowList_BatchRelations(t *testing.T) {
	t.Parallel()
	e := setupFollowList(t)
	ctx := t.Context()
	u := e.users(t, 5)
	viewer, following, followedBy, mutual, none := u[0], u[1], u[2], u[3], u[4]
	e.mustFollow(t, viewer, following)
	e.mustFollow(t, followedBy, viewer)
	e.mustFollow(t, viewer, mutual)
	e.mustFollow(t, mutual, viewer)
	ghost := uuid.New()

	vo, err := e.svc.GetRelations(ctx, viewer, []uuid.UUID{following, followedBy, mutual, none, viewer, ghost})
	if err != nil {
		t.Fatalf("GetRelations: %v", err)
	}
	want := map[string]followdomain.Relation{
		following.String():  followdomain.RelationFollowing,
		followedBy.String(): followdomain.RelationFollowedBy,
		mutual.String():     followdomain.RelationMutual,
		none.String():       followdomain.RelationNone,
		viewer.String():     followdomain.RelationNone,
		ghost.String():      followdomain.RelationNone,
	}
	if len(vo.Relations) != len(want) {
		t.Errorf("relations = %v, want %v", vo.Relations, want)
	}
	for id, rel := range want {
		if got, ok := vo.Relations[id]; !ok || got != rel {
			t.Errorf("relation with %s = %d (present=%v), want %d", id, got, ok, rel)
		}
	}

	tooMany := make([]uuid.UUID, followapp.MaxRelationTargets+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	if _, err = e.svc.GetRelations(ctx, viewer, tooMany); !errors.Is(err, followdomain.ErrTooManyTargets) {
		t.Errorf("%d ids: err = %v, want ErrTooManyTargets", len(tooMany), err)
	}
	if vo, err = e.svc.GetRelations(ctx, viewer, tooMany[:followapp.MaxRelationTargets]); err != nil || len(vo.Relations) != followapp.MaxRelationTargets {
		t.Errorf("%d ids: %d relations err=%v, want all", followapp.MaxRelationTargets, len(vo.Relations), err)
	}
}
