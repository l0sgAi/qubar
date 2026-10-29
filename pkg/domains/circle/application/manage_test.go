// Package application 的可管理圈子列表单元测试。
//
// 覆盖：page/size 规整（clamp）、空结果形状（data 为 [] 而非 null）、
// 关键词规整（trim + SanitizeForPg + 50 rune 截断）、实体→DTO 映射。
// memberRepo 用嵌入接口的最小 fake（仅 ListManagedCircles 被触达）。
package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// TestMain 测试装配：agent_count 回填的降级路径会记 Error 日志，注入 nop logger
// （生产由启动流程初始化，单测环境 logger.Log 为 nil）。
func TestMain(m *testing.M) {
	logger.InitLogger()
	m.Run()
}

// fakeManagedMemberRepo 记录入参并返回预置结果的最小 MemberRepository fake。
type fakeManagedMemberRepo struct {
	domain.MemberRepository // 未实现的方法不会被 ListManagedCircles 路径触达

	gotUserID  uuid.UUID
	gotKeyword string
	gotOffset  int
	gotSize    int

	circles []domain.ManagedCircle
	total   int64
}

func (f *fakeManagedMemberRepo) ListManagedCircles(ctx context.Context, userID uuid.UUID, keyword string, offset, size int) ([]domain.ManagedCircle, int64, error) {
	f.gotUserID, f.gotKeyword, f.gotOffset, f.gotSize = userID, keyword, offset, size
	return f.circles, f.total, nil
}

// newManagedListSvc 构造仅用于 ListManagedCircles 的 service（其余依赖不触达，传 nil）。
func newManagedListSvc(memberRepo *fakeManagedMemberRepo) *circleServiceImpl {
	return NewCircleService(nil, memberRepo, nil, nil, nil, nil, nil, nil).(*circleServiceImpl)
}

// TestListManagedCircles_PageSizeClamp page<=0→1；size<=0||>100→20；offset=(page-1)*size。
func TestListManagedCircles_PageSizeClamp(t *testing.T) {
	cases := []struct {
		name       string
		page, size int
		wantPage   int
		wantSize   int
		wantOffset int
	}{
		{"zero defaults to 1/20", 0, 0, 1, 20, 0},
		{"negative page", -5, 10, 1, 10, 0},
		{"oversize clamps to 20", 2, 500, 2, 20, 20},
		{"mid page", 3, 50, 3, 50, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeManagedMemberRepo{}
			svc := newManagedListSvc(repo)

			res, err := svc.ListManagedCircles(context.Background(), uuid.MustParse("0192a0d0-0000-7000-8000-000000000001"), "", tc.page, tc.size)
			if err != nil {
				t.Fatalf("ListManagedCircles failed: %v", err)
			}
			if repo.gotOffset != tc.wantOffset || repo.gotSize != tc.wantSize {
				t.Fatalf("repo got offset=%d size=%d, want offset=%d size=%d", repo.gotOffset, repo.gotSize, tc.wantOffset, tc.wantSize)
			}
			if res.Page != tc.wantPage || res.Size != tc.wantSize {
				t.Fatalf("result page=%d size=%d, want page=%d size=%d", res.Page, res.Size, tc.wantPage, tc.wantSize)
			}
		})
	}
}

// TestListManagedCircles_EmptyResultShape 空结果时 Data 必须是 [] 而非 null。
func TestListManagedCircles_EmptyResultShape(t *testing.T) {
	repo := &fakeManagedMemberRepo{}
	svc := newManagedListSvc(repo)

	res, err := svc.ListManagedCircles(context.Background(), uuid.New(), "", 1, 20)
	if err != nil {
		t.Fatalf("ListManagedCircles failed: %v", err)
	}
	if res.Data == nil {
		t.Fatal("Data must be non-nil empty slice, got nil (JSON 序列化为 null)")
	}
	if len(res.Data) != 0 {
		t.Fatalf("Data length = %d, want 0", len(res.Data))
	}
}

// TestListManagedCircles_KeywordSanitize 关键词 trim + SanitizeForPg（去 NUL/坏 UTF-8）+ 50 rune 截断。
func TestListManagedCircles_KeywordSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trimmed", "  圈子  ", "圈子"},
		{"strips NUL", "fo\x00o", "foo"},
		{"cjk unchanged", "游戏", "游戏"},
		{"over 50 runes truncated", strings.Repeat("圈", 60), strings.Repeat("圈", 50)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeManagedMemberRepo{}
			svc := newManagedListSvc(repo)

			if _, err := svc.ListManagedCircles(context.Background(), uuid.New(), tc.in, 1, 20); err != nil {
				t.Fatalf("ListManagedCircles failed: %v", err)
			}
			if repo.gotKeyword != tc.want {
				t.Fatalf("keyword = %q, want %q", repo.gotKeyword, tc.want)
			}
		})
	}
}

// fakeCircleAgentCounter 预设统计结果的 CircleAgentCounter fake（agent_count 回填用）。
type fakeCircleAgentCounter struct {
	counts map[uuid.UUID]int
	err    error
	gotIDs []uuid.UUID
}

func (f *fakeCircleAgentCounter) CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	f.gotIDs = circleIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.counts, nil
}

// TestListManagedCircles_AgentCountBackfill agent_count 经端口实时回填；
// 未注入端口（nil）降级为 0；端口报错降级为 0，不阻断列表。
func TestListManagedCircles_AgentCountBackfill(t *testing.T) {
	ownerID := uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	circleA := uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	circleB := uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb")
	now := time.Now().Truncate(time.Second)

	mkRepo := func() *fakeManagedMemberRepo {
		return &fakeManagedMemberRepo{
			circles: []domain.ManagedCircle{
				{Circle: domain.Circle{ID: circleA, Name: "圈A", CreateTime: now}, MyRole: domain.MemberRoleOwner},
				{Circle: domain.Circle{ID: circleB, Name: "圈B", CreateTime: now}, MyRole: domain.MemberRoleAdmin},
			},
			total: 2,
		}
	}

	t.Run("counter injected backfills counts", func(t *testing.T) {
		repo := mkRepo()
		svc := newManagedListSvc(repo)
		counter := &fakeCircleAgentCounter{counts: map[uuid.UUID]int{circleA: 3}}
		svc.SetAgentCounter(counter)

		res, err := svc.ListManagedCircles(context.Background(), ownerID, "", 1, 20)
		if err != nil {
			t.Fatalf("ListManagedCircles failed: %v", err)
		}
		if len(counter.gotIDs) != 2 {
			t.Fatalf("counter got %d circle ids, want 2", len(counter.gotIDs))
		}
		if res.Data[0].AgentCount != 3 {
			t.Fatalf("circleA AgentCount = %d, want 3", res.Data[0].AgentCount)
		}
		if res.Data[1].AgentCount != 0 {
			t.Fatalf("circleB AgentCount = %d, want 0（不在统计结果中的圈按 0）", res.Data[1].AgentCount)
		}
	})

	t.Run("counter error degrades to zero", func(t *testing.T) {
		repo := mkRepo()
		svc := newManagedListSvc(repo)
		svc.SetAgentCounter(&fakeCircleAgentCounter{err: errors.New("db down")})

		res, err := svc.ListManagedCircles(context.Background(), ownerID, "", 1, 20)
		if err != nil {
			t.Fatalf("ListManagedCircles must not fail on counter error: %v", err)
		}
		for _, item := range res.Data {
			if item.AgentCount != 0 {
				t.Fatalf("AgentCount = %d, want 0（降级）", item.AgentCount)
			}
		}
	})
}

// TestListManagedCircles_MapsEntityToItem 实体→DTO 映射：my_role 透传；
// 未注入 agentCounter 端口时 agent_count 降级为 0。
func TestListManagedCircles_MapsEntityToItem(t *testing.T) {
	ownerID := uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	circleID := uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	now := time.Now().Truncate(time.Second)

	repo := &fakeManagedMemberRepo{
		circles: []domain.ManagedCircle{
			{
				Circle: domain.Circle{
					ID: circleID, Name: "管理圈", Slug: "admin", Description: "描述",
					MemberCount: 12, PostCount: 34, JoinType: domain.CircleJoinTypeDirect,
					Status: domain.CircleStatusNormal, CreateTime: now,
				},
				MyRole: domain.MemberRoleOwner,
			},
			{
				Circle: domain.Circle{
					ID: uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb"), Name: "被封圈",
					Status: domain.CircleStatusBanned, CreateTime: now,
				},
				MyRole: domain.MemberRoleAdmin,
			},
		},
		total: 2,
	}
	svc := newManagedListSvc(repo)

	res, err := svc.ListManagedCircles(context.Background(), ownerID, "", 1, 20)
	if err != nil {
		t.Fatalf("ListManagedCircles failed: %v", err)
	}
	if res.Total != 2 || len(res.Data) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", res.Total, len(res.Data))
	}
	first := res.Data[0]
	if first.ID != circleID || first.Name != "管理圈" || first.Slug != "admin" ||
		first.MemberCount != 12 || first.PostCount != 34 || first.MyRole != domain.MemberRoleOwner ||
		!first.CreateTime.Equal(now) {
		t.Fatalf("unexpected first item: %+v", first)
	}
	if first.AgentCount != 0 {
		t.Fatalf("AgentCount = %d, want 0（未注入 agentCounter 端口时降级为 0）", first.AgentCount)
	}
	if res.Data[1].Status != domain.CircleStatusBanned || res.Data[1].MyRole != domain.MemberRoleAdmin {
		t.Fatalf("banned circle item should carry status+my_role: %+v", res.Data[1])
	}
}
