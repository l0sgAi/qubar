// invite_email.go 私密圈指定 email 邀请（#169）：添加 / 列表 / 撤销（管理端），我的邀请 / 接受或拒绝（被邀请人）。
//
// 被邀请人身份按当前用户的 users.email 匹配（经 UserEmailReader 跨域读取）：注册时邮箱已过 OTP 验证，
// OAuth 邮箱来自身份提供方，系统也没有修改邮箱的入口。接受的处理与邀请链接相同（joinByInvite）：
// 拉黑拒绝、已是成员幂等成功。
package application

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

const (
	// maxInviteEmailsPerAdd 单次添加的邮箱上限。
	maxInviteEmailsPerAdd = 50
	// maxInviteEmailLen 与 users.email varchar(100) 一致。
	maxInviteEmailLen = 100
)

// 添加邮箱时被忽略条目的原因。
const (
	InviteEmailIgnoredInvalid   = "invalid"         // 格式不合法 / 超长
	InviteEmailIgnoredDuplicate = "duplicate"       // 本次请求内重复（规整后）
	InviteEmailIgnoredExisting  = "already_invited" // 已有待接受或已接受的邀请
)

// UserEmailReader 跨域端口：读取用户 email（composition 桥接 user Facade；用户不存在返回空串）。
type UserEmailReader interface {
	GetEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

// InviteEmailIgnored 添加时被忽略的条目（Email 为请求原文）。
type InviteEmailIgnored struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// InviteEmailAddResult 添加结果：Added 为本次新建或重新置为待接受的邮箱（规整后）。
type InviteEmailAddResult struct {
	Added   []string             `json:"added"`
	Ignored []InviteEmailIgnored `json:"ignored"`
}

// InviteEmailItem 管理端 email 邀请列表项。
type InviteEmailItem struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	Status      int16     `json:"status"`
	InviterID   uuid.UUID `json:"inviter_id"`
	InviterName string    `json:"inviter_name"`
	CreateTime  time.Time `json:"create_time"`
	UpdateTime  time.Time `json:"update_time"`
}

// InviteEmailListResult 管理端 email 邀请列表（offset 分页）。
type InviteEmailListResult struct {
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
	Data  []InviteEmailItem `json:"data"`
}

// MyInviteItem 我收到的待接受邀请。
type MyInviteItem struct {
	InviteID     uuid.UUID `json:"invite_id"`
	CircleID     uuid.UUID `json:"circle_id"`
	CircleName   string    `json:"circle_name"`
	CircleAvatar string    `json:"circle_avatar"`
	InviterName  string    `json:"inviter_name"`
	CreateTime   time.Time `json:"create_time"`
}

// MyInviteListResult 我收到的邀请（offset 分页）。
type MyInviteListResult struct {
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
	Data  []MyInviteItem `json:"data"`
}

// SetInviteEmailRepository 注入 email 邀请仓储。
func (s *circleServiceImpl) SetInviteEmailRepository(repo domain.InviteEmailRepository) {
	s.inviteEmails = repo
}

// SetUserEmailReader 注入用户 email 读取端口（composition 桥接 user 域）。
func (s *circleServiceImpl) SetUserEmailReader(r UserEmailReader) { s.userEmails = r }

// normalizeInviteEmail trim + 小写，并校验为不带显示名的单个地址（≤100 字符）。
func normalizeInviteEmail(raw string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || utf8.RuneCountInString(e) > maxInviteEmailLen {
		return "", false
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || addr.Name != "" {
		return "", false
	}
	return e, true
}

// AddInviteEmails 批量添加 email 邀请（admin+，仅私密圈，单次 1–50 个）。
// 不合法与重复项被忽略并在结果里返回；已撤销 / 已拒绝的重置为待接受；邮箱暂无注册用户也允许添加。
func (s *circleServiceImpl) AddInviteEmails(ctx context.Context, operatorID, circleID uuid.UUID, emails []string) (*InviteEmailAddResult, error) {
	if s.inviteEmails == nil {
		return nil, errInviteUnavailable
	}
	if len(emails) == 0 || len(emails) > maxInviteEmailsPerAdd {
		return nil, errInvalidInviteEmails
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

	result := &InviteEmailAddResult{Added: []string{}, Ignored: []InviteEmailIgnored{}}
	seen := make(map[string]bool, len(emails))
	valid := make([]string, 0, len(emails))
	raws := make(map[string]string, len(emails)) // 规整后 → 请求原文（未添加时回报原文）
	for _, raw := range emails {
		e, ok := normalizeInviteEmail(raw)
		switch {
		case !ok:
			result.Ignored = append(result.Ignored, InviteEmailIgnored{Email: raw, Reason: InviteEmailIgnoredInvalid})
		case seen[e]:
			result.Ignored = append(result.Ignored, InviteEmailIgnored{Email: raw, Reason: InviteEmailIgnoredDuplicate})
		default:
			seen[e] = true
			raws[e] = raw
			valid = append(valid, e)
		}
	}

	added, err := s.inviteEmails.AddPending(ctx, circleID, operatorID, valid)
	if err != nil {
		return nil, err
	}
	addedSet := make(map[string]bool, len(added))
	for _, e := range added {
		addedSet[e] = true
	}
	for _, e := range valid { // 按请求顺序回报
		if addedSet[e] {
			result.Added = append(result.Added, e)
		} else {
			result.Ignored = append(result.Ignored, InviteEmailIgnored{Email: raws[e], Reason: InviteEmailIgnoredExisting})
		}
	}
	return result, nil
}

// ListInviteEmails 圈内 email 邀请列表（admin+；status=-1 不过滤，否则 0–3）。
func (s *circleServiceImpl) ListInviteEmails(ctx context.Context, operatorID, circleID uuid.UUID, status int16, page, size int) (*InviteEmailListResult, error) {
	if s.inviteEmails == nil {
		return nil, errInviteUnavailable
	}
	if status < -1 || status > domain.InviteEmailRejected {
		return nil, errInvalidInviteEmailStatus
	}
	page, size = normalizePage(page, size)
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return nil, err
	}
	if _, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return nil, err
	}
	rows, total, err := s.inviteEmails.ListByCircle(ctx, circleID, status, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	inviters := s.userBriefs(ctx, inviterIDs(rows))
	items := make([]InviteEmailItem, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		items = append(items, InviteEmailItem{
			ID: r.ID, Email: r.Email, Status: r.Status, InviterID: r.InviterID,
			InviterName: inviters[r.InviterID.String()].Username, CreateTime: r.CreateTime, UpdateTime: r.UpdateTime,
		})
	}
	return &InviteEmailListResult{Total: total, Page: page, Size: size, Data: items}, nil
}

// RevokeInviteEmail 撤销 email 邀请（admin+；仅待接受可撤销，已撤销幂等成功，已接受 / 已拒绝 409）。
func (s *circleServiceImpl) RevokeInviteEmail(ctx context.Context, operatorID, inviteID uuid.UUID) error {
	if s.inviteEmails == nil {
		return errInviteUnavailable
	}
	inv, err := s.inviteEmails.GetByID(ctx, inviteID)
	if err != nil {
		return err
	}
	if _, err := s.requireManageRole(ctx, inv.CircleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return err
	}
	if inv.Status == domain.InviteEmailRevoked {
		return nil
	}
	err = s.inviteEmails.UpdateStatus(ctx, inviteID, domain.InviteEmailPending, domain.InviteEmailRevoked, operatorID)
	if errors.Is(err, domain.ErrInviteEmailStateConflict) {
		// 并发撤销视为成功；已被接受 / 拒绝则冲突。
		if cur, gerr := s.inviteEmails.GetByID(ctx, inviteID); gerr == nil && cur.Status == domain.InviteEmailRevoked {
			return nil
		}
	}
	return err
}

// ListMyInvites 当前用户 email 收到的待接受邀请（附圈子名称 / 头像；已删除圈子不展示）。
func (s *circleServiceImpl) ListMyInvites(ctx context.Context, userID uuid.UUID, page, size int) (*MyInviteListResult, error) {
	page, size = normalizePage(page, size)
	empty := &MyInviteListResult{Page: page, Size: size, Data: []MyInviteItem{}}
	email, err := s.currentUserEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	if email == "" {
		return empty, nil
	}
	rows, total, err := s.inviteEmails.ListPendingByEmail(ctx, email, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	circleIDs := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		circleIDs = append(circleIDs, rows[i].CircleID)
	}
	circles, err := s.repo.GetByIDs(ctx, circleIDs)
	if err != nil {
		return nil, err
	}
	inviters := s.userBriefs(ctx, inviterIDs(rows))
	items := make([]MyInviteItem, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		c, ok := circles[r.CircleID]
		if !ok {
			continue
		}
		items = append(items, MyInviteItem{
			InviteID: r.ID, CircleID: r.CircleID, CircleName: c.Name, CircleAvatar: c.AvatarURL,
			InviterName: inviters[r.InviterID.String()].Username, CreateTime: r.CreateTime,
		})
	}
	empty.Total, empty.Data = total, items
	return empty, nil
}

// RespondInviteEmail 接受或拒绝发给自己 email 的邀请。
//
//   - 邀请不存在 / 不是发给自己的 / 已撤销 / 已拒绝 / 圈子不可用：errInviteInvalid（404，不区分）；
//   - 接受：已是成员幂等成功（邀请标记为已接受）；拉黑 errBannedFromCircle（邀请保持待接受）；
//     否则先 CAS 0→1 占住邀请，再 joinByInvite，入圈失败把邀请改回待接受。重复接受已接受的邀请幂等成功。
//   - 拒绝：CAS 0→3，返回的 AlreadyMember 恒 false。
func (s *circleServiceImpl) RespondInviteEmail(ctx context.Context, userID, inviteID uuid.UUID, accept bool) (*InviteAcceptResult, error) {
	email, err := s.currentUserEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	inv, err := s.inviteEmails.GetByID(ctx, inviteID)
	if errors.Is(err, domain.ErrInviteEmailNotFound) || (err == nil && (email == "" || inv.Email != email)) {
		return nil, errInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	circle, err := s.repo.GetByID(ctx, inv.CircleID)
	if errors.Is(err, domain.ErrCircleNotFound) || (err == nil && circle.Status != domain.CircleStatusNormal) {
		return nil, errInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	result := &InviteAcceptResult{CircleID: inv.CircleID}

	if !accept {
		if inv.Status != domain.InviteEmailPending {
			return nil, errInviteInvalid
		}
		if err := s.inviteEmails.UpdateStatus(ctx, inviteID, domain.InviteEmailPending, domain.InviteEmailRejected, userID); err != nil {
			if errors.Is(err, domain.ErrInviteEmailStateConflict) {
				return nil, errInviteInvalid
			}
			return nil, err
		}
		return result, nil
	}

	member, err := s.memberRepo.GetMember(ctx, inv.CircleID, userID)
	if err != nil && !errors.Is(err, domain.ErrMemberNotFound) {
		return nil, err
	}
	joined := member != nil && domain.IsJoinedStatus(member.Status)
	switch {
	case inv.Status == domain.InviteEmailAccepted && joined:
		result.AlreadyMember = true
		return result, nil
	case inv.Status != domain.InviteEmailPending:
		return nil, errInviteInvalid
	case joined:
		s.markInviteEmail(ctx, inviteID, domain.InviteEmailPending, domain.InviteEmailAccepted, userID)
		result.AlreadyMember = true
		return result, nil
	case member != nil && member.Status == domain.MemberStatusBanned:
		return nil, errBannedFromCircle
	}

	if err := s.inviteEmails.UpdateStatus(ctx, inviteID, domain.InviteEmailPending, domain.InviteEmailAccepted, userID); err != nil {
		if errors.Is(err, domain.ErrInviteEmailStateConflict) {
			return nil, errInviteInvalid
		}
		return nil, err
	}
	if err := s.joinByInvite(ctx, userID, inv.CircleID); err != nil {
		if IsAlreadyMemberErr(err) {
			result.AlreadyMember = true
			return result, nil
		}
		s.markInviteEmail(context.WithoutCancel(ctx), inviteID, domain.InviteEmailAccepted, domain.InviteEmailPending, userID)
		return nil, err
	}
	return result, nil
}

// markInviteEmail best-effort 状态迁移（幂等标记 / 补偿），失败只记日志。
func (s *circleServiceImpl) markInviteEmail(ctx context.Context, id uuid.UUID, from, to int16, operatorID uuid.UUID) {
	if err := s.inviteEmails.UpdateStatus(ctx, id, from, to, operatorID); err != nil && !errors.Is(err, domain.ErrInviteEmailStateConflict) {
		logger.Log.Error("Failed to update email invite status: " + err.Error())
	}
}

// currentUserEmail 当前用户 email（trim + 小写，与存储口径一致）；用户不存在返回空串。
func (s *circleServiceImpl) currentUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	if s.inviteEmails == nil || s.userEmails == nil {
		return "", errInviteUnavailable
	}
	email, err := s.userEmails.GetEmail(ctx, userID)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(email)), nil
}

// userBriefs 批量回填用户信息（user Facade 未注入或失败时降级为空，不阻断列表）。
func (s *circleServiceImpl) userBriefs(ctx context.Context, ids []string) map[string]UserBrief {
	if s.userFacade == nil || len(ids) == 0 {
		return map[string]UserBrief{}
	}
	briefs, err := s.userFacade.GetBriefs(ctx, ids)
	if err != nil {
		logger.Log.Error("Failed to load invite user briefs: " + err.Error())
		return map[string]UserBrief{}
	}
	return briefs
}

func inviterIDs(rows []domain.CircleInviteEmail) []string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].InviterID.String())
	}
	return ids
}

// normalizePage offset 分页参数规整：page 从 1 开始，size <=0 或 >100 回落 20。
func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	return page, size
}
