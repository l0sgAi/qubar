// invite.go 私密圈邀请制（#169）：邀请链接的创建 / 列表 / 撤销 / 预览 / 接受。
//
// 接受邀请复用 JoinCircle 的状态机与副作用（joinMember），只是跳过 join_type 门槛；
// POST /circle/join 对私密圈仍然拒绝。使用次数由 InviteLinkRepository.ConsumeUse 的条件 UPDATE 原子递增，
// 已是成员不消耗；消耗后入圈失败（并发重复接受、并发拉黑）归还本次消耗。
//
// 私密圈改为公开时：UpdateCircleProfile 批量撤销该圈链接；校验时也检查圈子当前 join_type，
// 批量撤销失败时链接同样不可用（双保险）。
package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

const (
	// inviteTokenBytes token 随机字节数（base64url 无填充编码后 43 字符）。
	inviteTokenBytes = 32
	// inviteTokenMaxLen 入参 token 长度上限：超长输入直接按无效处理，不做 hash 与查询。
	inviteTokenMaxLen = 128
	// defaultInviteExpireHours / maxInviteExpireHours 链接有效期默认 7 天，最长 30 天。
	defaultInviteExpireHours = 168
	maxInviteExpireHours     = 720
	// maxInviteUses 单个链接 max_uses 上限（不传表示不限次数）。
	maxInviteUses = 10000
)

// CreateInviteLinkInput 创建邀请链接入参。ExpireHours=0 取默认 7 天；MaxUses=nil 表示不限次数。
type CreateInviteLinkInput struct {
	ExpireHours int
	MaxUses     *int
}

// InviteLinkCreated 创建结果。Token 为明文，只在这一次响应中出现。
type InviteLinkCreated struct {
	ID         uuid.UUID `json:"id"`
	Token      string    `json:"token"`
	ExpireTime time.Time `json:"expire_time"`
	MaxUses    *int      `json:"max_uses"`
}

// InviteLinkItem 管理端链接列表项（不含 token）。
type InviteLinkItem struct {
	ID            uuid.UUID `json:"id"`
	ExpireTime    time.Time `json:"expire_time"`
	MaxUses       *int      `json:"max_uses"`
	UsedCount     int       `json:"used_count"`
	Revoked       bool      `json:"revoked"`
	Expired       bool      `json:"expired"`
	CreatorID     uuid.UUID `json:"creator_id"`
	CreatorName   string    `json:"creator_name"`
	CreatorAvatar string    `json:"creator_avatar"`
	CreateTime    time.Time `json:"create_time"`
}

// InviteLinkListResult 链接列表（offset 分页）。
type InviteLinkListResult struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`
	Data  []InviteLinkItem `json:"data"`
}

// InvitePreviewVO 邀请预览：只给圈子名称、头像、成员数。
type InvitePreviewVO struct {
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	MemberCount int    `json:"member_count"`
}

// InviteAcceptResult 接受邀请结果。AlreadyMember=true 表示本来就是成员（幂等成功，未消耗次数）。
type InviteAcceptResult struct {
	CircleID      uuid.UUID `json:"circle_id"`
	AlreadyMember bool      `json:"already_member"`
}

// SetInviteLinkRepository 注入邀请链接仓储（composition 装配；未注入时邀请接口返回错误）。
func (s *circleServiceImpl) SetInviteLinkRepository(repo domain.InviteLinkRepository) {
	s.inviteLinks = repo
}

// CreateInviteLink 生成邀请链接（admin+，仅私密圈）。
func (s *circleServiceImpl) CreateInviteLink(ctx context.Context, operatorID, circleID uuid.UUID, input CreateInviteLinkInput) (*InviteLinkCreated, error) {
	if err := s.requireInviteRepo(); err != nil {
		return nil, err
	}
	hours := input.ExpireHours
	if hours == 0 {
		hours = defaultInviteExpireHours
	}
	if hours < 1 || hours > maxInviteExpireHours {
		return nil, errInvalidInviteExpire
	}
	if input.MaxUses != nil && (*input.MaxUses < 1 || *input.MaxUses > maxInviteUses) {
		return nil, errInvalidInviteMaxUses
	}

	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return nil, err
	}
	if circle.JoinType != domain.CircleJoinTypePrivate {
		return nil, errInviteRequiresPrivate
	}

	token, err := newInviteToken()
	if err != nil {
		return nil, err
	}
	link := &domain.CircleInviteLink{
		ID:         sharedomain.NewID(),
		CircleID:   circleID,
		CreatorID:  operatorID,
		TokenHash:  domain.HashInviteToken(token),
		ExpireTime: time.Now().Add(time.Duration(hours) * time.Hour),
		MaxUses:    input.MaxUses,
	}
	if err := s.inviteLinks.Create(ctx, link); err != nil {
		return nil, err
	}
	return &InviteLinkCreated{ID: link.ID, Token: token, ExpireTime: link.ExpireTime, MaxUses: link.MaxUses}, nil
}

// ListInviteLinks 圈内邀请链接列表（admin+，含已撤销 / 已过期，按创建时间倒序）。
func (s *circleServiceImpl) ListInviteLinks(ctx context.Context, operatorID, circleID uuid.UUID, page, size int) (*InviteLinkListResult, error) {
	if err := s.requireInviteRepo(); err != nil {
		return nil, err
	}
	page, size = normalizePage(page, size)
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return nil, err
	}
	if _, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return nil, err
	}

	links, total, err := s.inviteLinks.ListByCircle(ctx, circleID, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	creatorIDs := make([]string, 0, len(links))
	for i := range links {
		creatorIDs = append(creatorIDs, links[i].CreatorID.String())
	}
	creators := s.userBriefs(ctx, creatorIDs)
	now := time.Now()
	items := make([]InviteLinkItem, 0, len(links))
	for i := range links {
		l := &links[i]
		b := creators[l.CreatorID.String()]
		items = append(items, InviteLinkItem{
			ID: l.ID, ExpireTime: l.ExpireTime, MaxUses: l.MaxUses, UsedCount: l.UsedCount,
			Revoked: l.Revoked != 0, Expired: !l.ExpireTime.After(now),
			CreatorID: l.CreatorID, CreatorName: b.Username, CreatorAvatar: b.AvatarURL,
			CreateTime: l.CreateTime,
		})
	}
	return &InviteLinkListResult{Total: total, Page: page, Size: size, Data: items}, nil
}

// RevokeInviteLink 撤销邀请链接（admin+；已撤销幂等成功）。
func (s *circleServiceImpl) RevokeInviteLink(ctx context.Context, operatorID, linkID uuid.UUID) error {
	if err := s.requireInviteRepo(); err != nil {
		return err
	}
	link, err := s.inviteLinks.GetByID(ctx, linkID)
	if err != nil {
		return err
	}
	if _, err := s.requireManageRole(ctx, link.CircleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return err
	}
	return s.inviteLinks.Revoke(ctx, linkID, operatorID)
}

// PreviewInvite 邀请预览（访客可读）。无效、过期、撤销、用尽、圈子不可用一律 errInviteInvalid（404），不区分原因。
func (s *circleServiceImpl) PreviewInvite(ctx context.Context, token string) (*InvitePreviewVO, error) {
	_, circle, err := s.loadUsableInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	memberCount, _, _ := s.getStatsWithRecovery(ctx, circle.ID)
	return &InvitePreviewVO{Name: circle.Name, AvatarURL: circle.AvatarURL, MemberCount: memberCount}, nil
}

// AcceptInvite 凭邀请链接加入私密圈。
//
//   - 已是成员（正常 / 禁言）：幂等成功，不消耗次数——只要 token 对应的链接存在，即使已用尽 / 过期 / 撤销
//     （成员重复点开同一链接不报错；对成员本人不泄露额外信息）；
//   - 链接不可用 / 圈子不可用：errInviteInvalid；拉黑：errBannedFromCircle；
//   - 否则先 ConsumeUse 原子占用一次，再走 joinByInvite（无记录新建 / 已退出改回正常 + 计数 / 事件 / ZSET）；
//     入圈失败归还本次占用。并发重复接受时后到者得到「已是成员」，同样按幂等成功返回。
func (s *circleServiceImpl) AcceptInvite(ctx context.Context, userID uuid.UUID, token string) (*InviteAcceptResult, error) {
	link, circle, err := s.lookupInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	result := &InviteAcceptResult{CircleID: circle.ID}

	member, err := s.memberRepo.GetMember(ctx, circle.ID, userID)
	if err != nil && !errors.Is(err, domain.ErrMemberNotFound) {
		return nil, err
	}
	if member != nil && domain.IsJoinedStatus(member.Status) {
		result.AlreadyMember = true
		return result, nil
	}
	if !inviteUsable(link, circle) {
		return nil, errInviteInvalid
	}
	if member != nil && member.Status == domain.MemberStatusBanned {
		return nil, errBannedFromCircle
	}

	if err := s.inviteLinks.ConsumeUse(ctx, link.ID); err != nil {
		if errors.Is(err, domain.ErrInviteLinkUnusable) {
			return nil, errInviteInvalid
		}
		return nil, err
	}
	if err := s.joinByInvite(ctx, userID, circle.ID); err != nil {
		if rerr := s.inviteLinks.ReleaseUse(context.WithoutCancel(ctx), link.ID); rerr != nil {
			logger.Log.Error("Failed to release invite link use: " + rerr.Error())
		}
		if IsAlreadyMemberErr(err) {
			result.AlreadyMember = true
			return result, nil
		}
		return nil, err
	}
	return result, nil
}

// loadUsableInvite 查链接并校验可用（见 inviteUsable），任一不满足返回 errInviteInvalid。
func (s *circleServiceImpl) loadUsableInvite(ctx context.Context, token string) (*domain.CircleInviteLink, *domain.Circle, error) {
	link, circle, err := s.lookupInvite(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if !inviteUsable(link, circle) {
		return nil, nil, errInviteInvalid
	}
	return link, circle, nil
}

// lookupInvite 按 token_hash 查链接及其圈子（不校验可用性）。token 为空 / 超长、链接或圈子不存在返回 errInviteInvalid。
func (s *circleServiceImpl) lookupInvite(ctx context.Context, token string) (*domain.CircleInviteLink, *domain.Circle, error) {
	if err := s.requireInviteRepo(); err != nil {
		return nil, nil, err
	}
	if token == "" || len(token) > inviteTokenMaxLen {
		return nil, nil, errInviteInvalid
	}
	link, err := s.inviteLinks.GetByTokenHash(ctx, domain.HashInviteToken(token))
	if err != nil {
		if errors.Is(err, domain.ErrInviteLinkNotFound) {
			return nil, nil, errInviteInvalid
		}
		return nil, nil, err
	}
	circle, err := s.repo.GetByID(ctx, link.CircleID)
	if err != nil {
		if errors.Is(err, domain.ErrCircleNotFound) {
			return nil, nil, errInviteInvalid
		}
		return nil, nil, err
	}
	return link, circle, nil
}

// inviteUsable 链接未撤销、未过期、未用尽，且圈子状态正常、仍是私密圈。
func inviteUsable(link *domain.CircleInviteLink, circle *domain.Circle) bool {
	return link.Usable(time.Now()) &&
		circle.Status == domain.CircleStatusNormal && circle.JoinType == domain.CircleJoinTypePrivate
}

// revokeInviteLinksOnPublic 私密圈改为公开后批量撤销该圈邀请链接（best-effort：
// 失败只记日志，loadUsableInvite 的 join_type 检查仍会拒绝这些链接）。
func (s *circleServiceImpl) revokeInviteLinksOnPublic(ctx context.Context, operatorID, circleID uuid.UUID) {
	if s.inviteLinks == nil {
		return
	}
	if _, err := s.inviteLinks.RevokeByCircle(ctx, circleID, operatorID); err != nil {
		logger.Log.Error("Failed to revoke invite links after circle became public: " + err.Error())
	}
}

func (s *circleServiceImpl) requireInviteRepo() error {
	if s.inviteLinks == nil {
		return errInviteUnavailable
	}
	return nil
}

// newInviteToken 32 字节密码学随机数，base64url（无填充）编码。
func newInviteToken() (string, error) {
	b := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
