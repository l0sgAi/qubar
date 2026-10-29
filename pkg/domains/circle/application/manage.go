// manage.go 圈子管理用例（角色变更/禁言/拉黑/移出/资料编辑）。
//
// 权限核心规则：只能管理角色严格低于自己的成员（owner > admin > member），
// owner 不可被禁言/拉黑/降级（转让除外）。
//
// 计数一致性：仅 status 跨「在圈」边界的迁移触发 member_count 增减（拉黑 / 移出 -1），
// 复用 join/leave 的 write-behind 链路（Redis Incr/Decr 真值 + Redpanda 事件落库 +
// joined ZSET 增删）。禁言仍是成员，不计数。入圈审核已下线（#168）。
// P1：通知扇出（notice 域）与审计表暂未接入。
package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// maxMuteHours 禁言时长上限（30 天），防误操作造成事实上的永久封口。
const maxMuteHours = 720

// memberSearchMaxUsers 成员搜索关键词最多解析的用户数。
// circle_member 对 (circle_id, user_id) 唯一 → 过滤后结果集 ≤ 此值，
// 游标翻页在该集合内精确进行；超过则提示细化关键词。
const memberSearchMaxUsers = 100

// manageKeywordMaxRunes 可管理圈子列表关键词长度上限（rune 计），控制台过滤框防超长输入。
const manageKeywordMaxRunes = 50

// ===== 跨域 Facade / 端口（圈子级 AI 机器人管理支撑，见 docs/circle-agent-manage-design.md）=====

// CircleMemberRoleReader circle 域对外暴露的成员角色只读 Facade。
//
// 供 aiagent 域经 composition 桥接做圈内机器人管理权限判断（aiagent 侧声明
// CircleRoleReader 端口，桥接器薄转发本接口）。独立声明而非让 aiagent import 本包。
type CircleMemberRoleReader interface {
	// GetMemberRole 返回用户在圈内的 (role, status)；非成员/圈子不存在 ok=false
	//（不区分两种情况，防成员身份探测）。直查 member 记录（无缓存：角色变更即时生效；
	// GetMember 含惰性解禁自愈）。
	GetMemberRole(ctx context.Context, circleID, userID uuid.UUID) (role, status int16, ok bool, err error)
}

// NewCircleMemberRoleReader 从 MemberRepository 构造跨域角色读取 Facade。
func NewCircleMemberRoleReader(memberRepo domain.MemberRepository) CircleMemberRoleReader {
	return &circleMemberRoleReader{repo: memberRepo}
}

type circleMemberRoleReader struct {
	repo domain.MemberRepository
}

// GetMemberRole 直查 member 记录（无缓存）。
func (f *circleMemberRoleReader) GetMemberRole(ctx context.Context, circleID, userID uuid.UUID) (int16, int16, bool, error) {
	m, err := f.repo.GetMember(ctx, circleID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	return m.Role, m.Status, true, nil
}

// CircleAgentCounter 跨域端口：批量统计各圈已绑定 AI 代理数（可管理圈子列表
// agent_count 回填用）。代理数据属 aiagent 领域——circle 域声明端口，
// composition 桥接 aiagent.AgentRepository.CountByCircleIDs 实现（方向反转桥接）。
type CircleAgentCounter interface {
	CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error)
}

// ===== 可管理圈子列表（AI 代理管理控制台）=====

// ManagedCircleItem 我可管理的圈子列表项。
//
// AgentCount 为该圈已绑定 AI 代理数（实时统计，上限 5；统计失败降级为 0）。
type ManagedCircleItem struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Description string    `json:"description"`
	MemberCount int       `json:"member_count"`
	PostCount   int       `json:"post_count"`
	JoinType    int16     `json:"join_type"`
	Status      int16     `json:"status"`
	// MyRole 调用者在该圈的角色（20=admin / 30=owner），驱动 UI 能力差异
	//（如仅圈主可删圈/转让/改代理凭据）。
	MyRole     int16     `json:"my_role"`
	AgentCount int       `json:"agent_count"`
	CreateTime time.Time `json:"create_time"`
}

// ManagedCircleListResult 可管理圈子列表（offset 分页，形状对齐 aiagent.AgentListResult）。
type ManagedCircleListResult struct {
	Total int64               `json:"total"`
	Page  int                 `json:"page"`
	Size  int                 `json:"size"`
	Data  []ManagedCircleItem `json:"data"`
}

// ListManagedCircles 列出"我可管理 AI 代理的圈子"（owner/admin，offset 分页）。
//
// 权限模型：登录即可调用，查询本身即权限过滤（WHERE role IN (20,30) AND status=normal），
// 无 ensureAdmin 式门槛；平台管理员（users.role=1）不特殊处理——其全局控制台是 /agent/list。
// keyword 非空时按圈子 name/description 子串过滤（trim + SanitizeForPg + 50 rune 截断）；
// 含非正常状态圈子（banned/pending），UI 依据 status 置灰。不缓存：角色变更须立即可见。
func (s *circleServiceImpl) ListManagedCircles(ctx context.Context, operatorID uuid.UUID, keyword string, page, size int) (*ManagedCircleListResult, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	keyword = sanitizeManageKeyword(keyword)

	circles, total, err := s.memberRepo.ListManagedCircles(ctx, operatorID, keyword, (page-1)*size, size)
	if err != nil {
		return nil, err
	}

	// 回填各圈 AI 代理数（实时 COUNT：表小 + idx_ai_agent_circle 部分索引，成本低；
	// 端口未注入或查询失败降级为 0，不阻断列表）。
	agentCounts := make(map[uuid.UUID]int, len(circles))
	if s.agentCounter != nil && len(circles) > 0 {
		ids := make([]uuid.UUID, 0, len(circles))
		for i := range circles {
			ids = append(ids, circles[i].ID)
		}
		counts, err := s.agentCounter.CountByCircleIDs(ctx, ids)
		if err != nil {
			logger.Log.Error("Failed to count circle agents for managed list: " + err.Error())
		} else {
			agentCounts = counts
		}
	}

	items := make([]ManagedCircleItem, 0, len(circles))
	for i := range circles {
		c := &circles[i]
		items = append(items, ManagedCircleItem{
			ID:          c.ID,
			Name:        c.Name,
			Slug:        c.Slug,
			AvatarURL:   c.AvatarURL,
			Description: c.Description,
			MemberCount: c.MemberCount,
			PostCount:   c.PostCount,
			JoinType:    c.JoinType,
			Status:      c.Status,
			MyRole:      c.MyRole,
			AgentCount:  agentCounts[c.ID],
			CreateTime:  c.CreateTime,
		})
	}
	return &ManagedCircleListResult{Total: total, Page: page, Size: size, Data: items}, nil
}

// sanitizeManageKeyword 规整管理列表关键词：trim → SanitizeForPg（防 PG UTF8 错误）
// → 超 50 rune 截断（控制台过滤场景截断优于报错）。
func sanitizeManageKeyword(kw string) string {
	kw = utils.SanitizeForPg(strings.TrimSpace(kw))
	if utf8.RuneCountInString(kw) > manageKeywordMaxRunes {
		runes := []rune(kw)
		kw = string(runes[:manageKeywordMaxRunes])
	}
	return kw
}

// ===== 管理端 DTO =====

// CircleMemberItem 管理端成员列表项（含用户精简信息，由 UserFacade 组装）。
type CircleMemberItem struct {
	UserID      uuid.UUID  `json:"user_id"`
	Username    string     `json:"username,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	Role        int16      `json:"role"`
	Status      int16      `json:"status"`
	MuteEndTime *time.Time `json:"mute_end_time,omitempty"`
	JoinTime    time.Time  `json:"join_time"`
}

// CircleMemberListResult 成员列表分页结果（keyset 游标，空串 = 没有更多）。
//
// Truncated：keyword 模式解析到的候选用户数超过 memberSearchMaxUsers（100）时
// 为 true，表示结果可能不完整（超出部分按相关性丢弃）。前端应提示「细化关键词」。
// 仅 keyword 非空时可能为 true。
type CircleMemberListResult struct {
	Members   []CircleMemberItem `json:"members"`
	Size      int                `json:"size"`
	Cursor    string             `json:"cursor"`
	Truncated bool               `json:"truncated"`
}

// UpdateCircleProfileInput 编辑圈子资料入参（nil 字段 = 不更新）。
//
// 分字段权限：Name/Slug/JoinType/CategoryID 仅圈主可改；
// AvatarURL/CoverURL/Description/Rule 管理员及以上可改。
// Slug 传空串清除；CategoryID 指向 uuid.Nil 清除分类。
type UpdateCircleProfileInput struct {
	Name        *string
	Slug        *string
	AvatarURL   *string
	CoverURL    *string
	Description *string
	Rule        *string
	CategoryID  *uuid.UUID
	JoinType    *int16
}

// ===== 权限校验 =====

// requireManageRole 校验操作者是圈内管理者并返回其成员记录。
// requireRole 指定最低角色（admin/owner）；非成员视同无权限（不暴露成员身份）；
// 非正常状态（禁言/拉黑/退出）的管理者暂停管理权。
func (s *circleServiceImpl) requireManageRole(ctx context.Context, circleID, operatorID uuid.UUID, requireRole int16) (*domain.CircleMember, error) {
	operator, err := s.memberRepo.GetMember(ctx, circleID, operatorID)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return nil, errNotCircleAdmin
		}
		return nil, err
	}
	if operator.Role < requireRole {
		if requireRole == domain.MemberRoleOwner {
			return nil, errNotCircleOwner
		}
		return nil, errNotCircleAdmin
	}
	if operator.Status != domain.MemberStatusNormal {
		return nil, errNotCircleAdmin
	}
	return operator, nil
}

// loadManagedTarget 加载目标成员并校验层级：只能管理角色严格低于自己的人
// （目标角色 >= 操作者 → 403，含操作者本人）。
func (s *circleServiceImpl) loadManagedTarget(ctx context.Context, operator *domain.CircleMember, circleID, targetUserID uuid.UUID) (*domain.CircleMember, error) {
	target, err := s.memberRepo.GetMember(ctx, circleID, targetUserID)
	if err != nil {
		return nil, err // ErrMemberNotFound → 404
	}
	if target.Role >= operator.Role {
		return nil, errCannotManageTarget
	}
	return target, nil
}

// requireNormalTarget 校验目标为正常状态成员（禁言/任免/转让的前置状态）。
func requireNormalTarget(target *domain.CircleMember) error {
	if target.Status != domain.MemberStatusNormal {
		return domain.ErrMemberStateConflict
	}
	return nil
}

// ===== 成员列表 =====

// validMemberFilter 校验列表过滤参数：role ∈ {-1,10,20,30}；status ∈ {-1,0..4}。
// status=0（待审核）已废弃：入圈审核下线后恒为空列表，为兼容旧前端仍接受。
func validMemberFilter(role, status int16) bool {
	roleOK := role == -1 ||
		role == domain.MemberRoleMember || role == domain.MemberRoleAdmin || role == domain.MemberRoleOwner
	return roleOK && status >= -1 && status <= domain.MemberStatusLeft
}

// ListCircleMembers 管理端成员列表（admin+，可见全部状态含拉黑/退出）。
// 按角色（高→低）、加入时间（新→旧）keyset 分页；用户信息经 UserFacade 组装。
// keyword 非空时按用户名搜索（拼写容错，email 分词匹配亦参与召回）：先经 user 域
// 解析为候选用户集（≤memberSearchMaxUsers 个，username 权重 3 倍于 email，按相关性
// 排序；命中超上限时 Truncated=true，提示细化关键词），再以 user_id IN 过滤成员表——
// 成员表 (circle_id, user_id) 唯一，故结果集有限，游标翻页精确；翻页须带同一 keyword。
func (s *circleServiceImpl) ListCircleMembers(ctx context.Context, operatorID, circleID uuid.UUID, role, status int16, keyword, cursor string, size int) (*CircleMemberListResult, error) {
	if !validMemberFilter(role, status) {
		return nil, errInvalidMemberFilter
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return nil, err
	}
	if _, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return nil, err
	}

	// 关键词 → 候选用户集（跨域搜索在权限校验之后，避免未授权消耗 ES 查询）。
	keyword = strings.TrimSpace(keyword)
	var userIDs []uuid.UUID
	truncated := false
	if keyword != "" {
		if s.userFacade == nil {
			return nil, errUserSearchUnavailable
		}
		briefs, total, err := s.userFacade.SearchBriefs(ctx, keyword, memberSearchMaxUsers)
		if err != nil {
			logger.Log.Error("Failed to search users for member list: " + err.Error())
			return nil, errUserSearchUnavailable
		}
		// 命中总数超过解析上限：结果不完整，标记截断供前端提示。
		truncated = total > memberSearchMaxUsers
		if len(briefs) == 0 {
			// 关键词无命中用户：直接空页（不必查成员表）。
			return &CircleMemberListResult{Members: []CircleMemberItem{}, Size: size, Cursor: "", Truncated: false}, nil
		}
		userIDs = make([]uuid.UUID, 0, len(briefs))
		for _, b := range briefs {
			if id, err := uuid.Parse(b.ID); err == nil {
				userIDs = append(userIDs, id)
			}
		}
	}

	members, next, err := s.memberRepo.ListMembers(ctx, circleID, role, status, userIDs, cursor, size)
	if err != nil {
		return nil, err
	}

	// 批量组装用户精简信息（Facade 缺失或部分用户查询失败时降级为空）。
	briefs := make(map[string]UserBrief, len(members))
	if s.userFacade != nil && len(members) > 0 {
		ids := make([]string, 0, len(members))
		for i := range members {
			ids = append(ids, members[i].UserID.String())
		}
		if b, err := s.userFacade.GetBriefs(ctx, ids); err == nil {
			briefs = b
		}
	}

	items := make([]CircleMemberItem, 0, len(members))
	for i := range members {
		m := &members[i]
		item := CircleMemberItem{
			UserID:      m.UserID,
			Role:        m.Role,
			Status:      m.Status,
			MuteEndTime: m.MuteEndTime,
			JoinTime:    m.CreateTime,
		}
		if b, ok := briefs[m.UserID.String()]; ok {
			item.Username = b.Username
			item.AvatarURL = b.AvatarURL
		}
		items = append(items, item)
	}
	return &CircleMemberListResult{Members: items, Size: size, Cursor: next, Truncated: truncated}, nil
}

// ===== 角色管理 =====

// SetMemberRole 设为/取消管理员（仅圈主）。role 仅接受 10/20，转让圈主走 TransferOwner。
func (s *circleServiceImpl) SetMemberRole(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, role int16) error {
	if role != domain.MemberRoleMember && role != domain.MemberRoleAdmin {
		return errInvalidMemberRole
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleOwner)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	// 前置状态机：须为正常成员且当前角色不同（已是该角色 → 409）；repo CAS 兜底并发窗口。
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	if target.Role == role {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberRole(ctx, circleID, targetUserID, target.Role, role)
}

// TransferOwner 转让圈主（仅圈主；目标必须是正常状态的非圈主成员）。
func (s *circleServiceImpl) TransferOwner(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleOwner)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	return s.memberRepo.TransferOwner(ctx, circleID, operatorID, targetUserID)
}

// ===== 禁言 =====

// MuteMember 禁言（admin+；时长 1-720 小时）。禁言仍是成员：member_count 不变。
func (s *circleServiceImpl) MuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, durationHours int) error {
	if durationHours <= 0 || durationHours > maxMuteHours {
		return errInvalidMuteDuration
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	muteEnd := time.Now().Add(time.Duration(durationHours) * time.Hour)
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusNormal, domain.MemberStatusMuted, muteEnd)
}

// UnmuteMember 解除禁言（admin+；muted → normal，清空 mute_end_time）。
func (s *circleServiceImpl) UnmuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusMuted {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusMuted, domain.MemberStatusNormal, time.Time{})
}

// ===== 拉黑/解禁 =====

// BanMember 拉黑/踢出（admin+；normal/muted → banned）。
// 拉黑保留记录防重进（JoinCircle 对 banned 拒绝），计数副作用见 evictMember。
func (s *circleServiceImpl) BanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	return s.evictMember(ctx, operatorID, circleID, targetUserID, domain.MemberStatusBanned)
}

// RemoveMember 移出成员（admin+；normal/muted → left）。
// 与拉黑的区别是不留黑名单：公开圈可再直接加入，私密圈需重新被邀请。
func (s *circleServiceImpl) RemoveMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	return s.evictMember(ctx, operatorID, circleID, targetUserID, domain.MemberStatusLeft)
}

// evictMember 把在圈成员（normal/muted）迁到 toStatus（banned 或 left），拉黑与移出共用。
// 权限：admin+，且只能处理角色严格低于自己的人（不能处理自己 / 圈主）。
// 原状态都在圈（计数态）：member_count-1 + 发布事件 + joined ZSET 移除，与 LeaveCircle 同链路。
func (s *circleServiceImpl) evictMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, toStatus int16) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusNormal && target.Status != domain.MemberStatusMuted {
		return domain.ErrMemberStateConflict
	}

	err = s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID, target.Status, toStatus, time.Time{})
	if errors.Is(err, domain.ErrMemberStateConflict) && target.Status == domain.MemberStatusNormal {
		// 并发窗口：读到的 normal 已被并发禁言，按 muted 再 CAS 一次（两个源态都计数，副作用等价）。
		err = s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
			domain.MemberStatusMuted, toStatus, time.Time{})
	}
	if err != nil {
		return err
	}

	if err := s.decrMemberCountWithRecovery(ctx, circleID); err != nil {
		logger.Log.Error("Failed to update Redis member count: " + err.Error())
	}
	if err := s.publisher.PublishMemberCount(ctx, circleID, -1); err != nil {
		logger.Log.Error("Failed to publish member evict message: " + err.Error())
	}
	if err := s.joinedCache.Remove(ctx, targetUserID, circleID); err != nil {
		logger.Log.Error("Failed to remove joined circle from cache: " + err.Error())
	}
	return nil
}

// UnbanMember 解除拉黑（admin+；banned → left）。不回圈不计数：公开圈用户可再直接加入，
// 私密圈需重新被邀请（JoinCircle 对非公开圈的 left 拒绝）。
func (s *circleServiceImpl) UnbanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusBanned {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusBanned, domain.MemberStatusLeft, time.Time{})
}

// ===== 编辑圈子资料 =====

// UpdateCircleProfile 编辑圈子资料。分字段权限：name/slug/join_type/category_id
// 仅圈主；其余 admin+。成功后失效基础信息缓存；ES 由 CDC 追平（秒级延迟）。
func (s *circleServiceImpl) UpdateCircleProfile(ctx context.Context, operatorID, circleID uuid.UUID, input UpdateCircleProfileInput) error {
	fields := domain.CircleUpdateFields{
		Name: input.Name, Slug: input.Slug, AvatarURL: input.AvatarURL, CoverURL: input.CoverURL,
		Description: input.Description, Rule: input.Rule, CategoryID: input.CategoryID, JoinType: input.JoinType,
	}
	if fields.Name == nil && fields.Slug == nil && fields.AvatarURL == nil && fields.CoverURL == nil &&
		fields.Description == nil && fields.Rule == nil && fields.CategoryID == nil && fields.JoinType == nil {
		return errNoCircleUpdateField
	}

	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	if operator.Role != domain.MemberRoleOwner &&
		(fields.Name != nil || fields.Slug != nil || fields.JoinType != nil || fields.CategoryID != nil) {
		return errNotCircleOwner
	}

	// 字段规整与校验：文本先 SanitizeForPg（防 PG UTF8 错误），长度按 rune 计
	// （对齐 DDL varchar 限制与创建接口的 binding 校验）。
	if fields.Name != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Name))
		if v == "" || utf8.RuneCountInString(v) > 50 {
			return errInvalidCircleProfile
		}
		fields.Name = &v
		if v != circle.Name { // 未变更不预检（否则撞自己的唯一索引）
			exists, err := s.repo.ExistsByName(ctx, v)
			if err != nil {
				return err
			}
			if exists {
				return errCircleNameExists
			}
		}
	}
	if fields.Slug != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Slug))
		if utf8.RuneCountInString(v) > 60 {
			return errInvalidCircleProfile
		}
		fields.Slug = &v
		if v != "" && v != circle.Slug {
			exists, err := s.repo.ExistsBySlug(ctx, v)
			if err != nil {
				return err
			}
			if exists {
				return errCircleSlugExists
			}
		}
	}
	if fields.Description != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Description))
		if v == "" || utf8.RuneCountInString(v) > 2000 {
			return errInvalidCircleProfile
		}
		fields.Description = &v
	}
	if fields.Rule != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Rule))
		if utf8.RuneCountInString(v) > 2000 {
			return errInvalidCircleProfile
		}
		fields.Rule = &v
	}
	if fields.AvatarURL != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.AvatarURL))
		if utf8.RuneCountInString(v) > 500 {
			return errInvalidCircleProfile
		}
		fields.AvatarURL = &v
	}
	if fields.CoverURL != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.CoverURL))
		if utf8.RuneCountInString(v) > 500 {
			return errInvalidCircleProfile
		}
		fields.CoverURL = &v
	}
	if fields.JoinType != nil && !validJoinType(*fields.JoinType) {
		return errInvalidJoinType
	}

	if err := s.repo.Update(ctx, circleID, fields); err != nil {
		return err
	}
	// 失效基础信息缓存（best-effort；TTL 24h 兜底残余）。
	if err := s.baseCache.DeleteBase(ctx, circleID); err != nil {
		logger.Log.Error("Failed to invalidate circle base cache: " + err.Error())
	}
	if fields.JoinType != nil && *fields.JoinType != circle.JoinType {
		s.visibility.invalidatePrivateIDs(ctx)
	}
	return nil
}
