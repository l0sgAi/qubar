// Package application 提供 user 领域的应用服务层。
//
// 职责：
//   - 用例编排（用户 CRUD、搜索、缓存策略、资料更新）；
//   - 通过 UserFacade 接口向其他领域暴露"用户精简视图"查询能力。
//
// 跨领域通信约定：post/comment/circle 等领域需要用户信息时，
// 依赖本包的 UserFacade 接口，由 composition 注入实现，避免直接 import
// user 领域的 domain/infrastructure 包。
package application

import (
	"context"
	"strings"
	"time"

	"interestBar/pkg/domains/user/domain"
	"interestBar/pkg/util/password"

	"github.com/google/uuid"
)

// UserBrief 是给跨领域调用的用户精简视图（与 domain.UserBrief 对应）。
//
// 定义在 application 层而非 domain 层，是因为"对外暴露什么字段"是用例决策，
// domain.UserBrief 是纯粹的值对象，application.UserBrief 是契约。
type UserBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// UserFacade 是 user 领域给其他领域的跨领域查询接口。
//
// 任何需要"组装用户信息"的领域（post/comment/circle）都应依赖此接口，
// 而非直接查询数据库。这样：
//   - 解耦：被调用方不感知 user 的存储实现；
//   - 可测：mock UserFacade 即可测试组装逻辑；
//   - 未来拆服务：把 UserFacade 实现换成 RPC client 即可。
type UserFacade interface {
	// GetBriefs 批量获取用户精简视图。入参为 UUIDv7 字符串列表，
	// 返回以 ID 字符串为 key 的 map。未找到的用户不会出现在 map 里。
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// GetBrief 获取单个用户精简视图。未找到返回 nil, nil。
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
	// SearchBriefs 按关键字搜索用户精简视图（username 拼写容错 + email 分词匹配，
	// 复用 ES 用户搜索链路）。返回按相关性排序的有序列表（最多 limit 个，
	// limit<=0 或 >100 按 100 处理，与 ES SearchUsers 的 size 语义一致）
	// 与命中总数 total（track_total_hits）；total > len(列表) 表示截断。
	// keyword 为空返回空列表。
	// 供跨领域"按用户名/邮箱过滤成员"类场景使用（如圈子成员管理搜索）。
	SearchBriefs(ctx context.Context, keyword string, limit int) ([]UserBrief, int64, error)
	// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（仅含圈内机器人与
	// 不可被 @ 的机器人，普通用户/可 @ 的全局机器人不出现在 map 里）。
	// 供 post/comment 域 mention 兜底剔除越圈、停用或已删除的机器人。
	GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error)
}

// AgentMentionScope 机器人账号的 @提及 可见范围（users.agent_circle_id / agent_mention_disabled 投影）。
type AgentMentionScope struct {
	// CircleID 绑定圈子ID；uuid.Nil=全局机器人。
	CircleID uuid.UUID
	// Mentionable 能否被 @：机器人停用或已删除时为 false。
	Mentionable bool
}

// UserListItemVO 用户列表项（搜索结果用）。
type UserListItemVO struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url"`
	Gender     int8   `json:"gender"`
	Role       int8   `json:"role"`
	CreateTime string `json:"create_time"`
}

// UserSearchResult 用户搜索结果。
type UserSearchResult struct {
	Total       int64            `json:"total"`
	Size        int              `json:"size"`
	SearchAfter string           `json:"search_after"`
	Users       []UserListItemVO `json:"data"`
}

// UpdateProfileInput 修改资料的入参（指针字段允许部分更新）。
type UpdateProfileInput struct {
	Username  *string
	AvatarURL *string
	Phone     *string
	Gender    *int
	Birthdate *time.Time
	// Password / ConfirmPassword 重置密码：两者须同时提供、相等且达到
	// password.MinLength；任一为 nil 表示本次不改密码（纯重置语义，不校验旧密码）。
	Password        *string
	ConfirmPassword *string
}

// UpdateProfileResult 修改资料的返回值。
type UpdateProfileResult struct {
	ID        uuid.UUID  `json:"id"`
	Username  string     `json:"username"`
	Email     string     `json:"email"`
	AvatarURL string     `json:"avatar_url"`
	Phone     string     `json:"phone"`
	Gender    int        `json:"gender"`
	Birthdate *time.Time `json:"birthdate"`
}

// UserSearcher 是用户搜索的抽象（由 infrastructure 提供 ES 实现）。
//
// 抽象出来是为了让 UserService 依赖接口而非直接 import ES 包。
type UserSearcher interface {
	// Search 按关键字搜索用户，支持 search_after 分页。
	// circleID 为圈子作用域（Nil=全站：排除圈内机器人；非 Nil=圈内@：本圈机器人可见）。
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}, circleID uuid.UUID) (*UserSearchResult, error)
}

// UserService 是 user 领域的应用服务接口。
type UserService interface {
	// GetByID 获取用户详情（带缓存）。
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error)
	// GetByIDStr 获取用户详情，入参为 loginID 字符串（/user/get 等会话接口用）。
	GetByIDStr(ctx context.Context, loginID string) (*domain.SysUser, error)
	// GetByEmail 按邮箱查询（无缓存，登录/注册流程用）。
	GetByEmail(ctx context.Context, email string) (*domain.SysUser, error)
	// UpdateProfile 修改用户资料（部分字段更新）。
	UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*UpdateProfileResult, error)
	// Search 搜索用户。searchAfter 为已解析的分页游标（可为 nil）。
	// circleID 为圈子作用域（Nil=全站；非 Nil=圈内@选人，见 UserSearcher.Search）。
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}, circleID uuid.UUID) (*UserSearchResult, error)
	// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（仅含 agent_circle_id 非 NULL
	// 或 agent_mention_disabled=1 的行，普通用户/可 @ 的全局机器人不出现在 map 里）。
	// 直查 repo 不走缓存（mention 校验低频，数据新鲜度优先）。
	// 供 post/comment 域 mention 兜底剔除越圈、停用或已删除的机器人。
	GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error)
	// SetAgentMentionable 写机器人账号能否被 @（users.agent_mention_disabled，ai_agent 状态的投影），
	// 并刷新 userinfo 缓存。供 aiagent 域创建后停用/启用/删除机器人时调用（幂等）。
	// 不动 users.status：GetBriefs 跳过非正常用户，改 status 会让机器人历史评论丢失作者信息。
	SetAgentMentionable(ctx context.Context, userID uuid.UUID, mentionable bool) error
}

type userServiceImpl struct {
	repo     domain.UserRepository
	cache    domain.UserCache
	searcher UserSearcher
}

// NewUserService 构造一个 UserService。
func NewUserService(repo domain.UserRepository, cache domain.UserCache, searcher UserSearcher) UserService {
	return &userServiceImpl{repo: repo, cache: cache, searcher: searcher}
}

// NewUserFacade 从 UserService 构造一个 UserFacade。
//
// 通常在 composition 层调用：先把 userService 构造好，
// 再用 NewUserFacade(svc) 产出注入给 post/circle 等领域的 facade 实例。
func NewUserFacade(svc UserService) UserFacade {
	return &userFacadeAdapter{svc: svc}
}

// userFacadeAdapter 把 UserService 适配为 UserFacade。
type userFacadeAdapter struct {
	svc UserService
}

// GetBriefs 批量获取用户精简视图。
func (f *userFacadeAdapter) GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error) {
	if len(userIDs) == 0 {
		return make(map[string]UserBrief), nil
	}

	ids := make([]uuid.UUID, 0, len(userIDs))
	for _, s := range userIDs {
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return make(map[string]UserBrief), nil
	}

	// 这里复用 GetByID（带缓存），避免一次性拉全表。
	// 若后续需要更高效的批量查询，可在 UserService 增加 GetByIDs 方法。
	// 当前优先保证缓存命中率，与旧 controller 行为一致。
	result := make(map[string]UserBrief, len(ids))
	for _, id := range ids {
		user, err := f.svc.GetByID(ctx, id)
		if err != nil || user == nil {
			continue
		}
		if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
			continue
		}
		result[id.String()] = UserBrief{
			ID:        user.ID.String(),
			Username:  user.Username,
			AvatarURL: user.AvatarURL,
		}
	}
	return result, nil
}

// GetBrief 获取单个用户精简视图。
func (f *userFacadeAdapter) GetBrief(ctx context.Context, userID string) (*UserBrief, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, nil
	}
	user, err := f.svc.GetByID(ctx, id)
	if err != nil || user == nil {
		return nil, err
	}
	if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
		return nil, nil
	}
	return &UserBrief{
		ID:        user.ID.String(),
		Username:  user.Username,
		AvatarURL: user.AvatarURL,
	}, nil
}

// SearchBriefs 按关键字搜索用户精简视图（username 拼写容错 + email 分词匹配，
// 复用 ES 用户搜索链路）。
//
// 直接映射 ES 结果（ID/Username/AvatarURL），不再回源刷新——调用方列表组装
// 通常会再走 GetBriefs 拿新鲜展示信息，此处只承担"关键词 → 候选 ID 集"职责。
// 返回命中总数 total（track_total_hits）：total > len(列表) 表示按 limit 截断，
// 供调用方提示"细化关键词"。email 仅用于匹配，不落入 UserBrief（不向调用方暴露邮箱）。
func (f *userFacadeAdapter) SearchBriefs(ctx context.Context, keyword string, limit int) ([]UserBrief, int64, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, 0, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	res, err := f.svc.Search(ctx, keyword, limit, nil, uuid.Nil)
	if err != nil {
		return nil, 0, err
	}
	out := make([]UserBrief, 0, len(res.Users))
	for _, u := range res.Users {
		out = append(out, UserBrief{ID: u.ID, Username: u.Username, AvatarURL: u.AvatarURL})
	}
	return out, res.Total, nil
}

// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（薄转发 UserService 同名方法）。
func (f *userFacadeAdapter) GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error) {
	return f.svc.GetAgentMentionScopes(ctx, userIDs)
}

// GetByID 获取用户详情，先查缓存再回源 DB。
//
// 行为与旧 controller.GetUserDetail 一致：
//  1. 先查缓存（命中且 status=1, deleted=0 直接返回）；
//  2. 缓存未命中查 DB；
//  3. 校验 status/deleted；
//  4. 回写缓存（失败不影响主流程）。
func (s *userServiceImpl) GetByID(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error) {
	// 1. 缓存
	if cached, _ := s.cache.GetUser(ctx, userID); cached != nil {
		if cached.Status != domain.UserStatusActive || cached.Deleted != domain.UserNotDeleted {
			return nil, nil
		}
		return cached, nil
	}

	// 2. DB
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	// 3. 校验
	if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
		return nil, nil
	}

	// 4. 回写缓存
	_ = s.cache.SetUser(ctx, userID, user)

	return user, nil
}

// GetByEmail 按邮箱查询（无缓存）。
func (s *userServiceImpl) GetByEmail(ctx context.Context, email string) (*domain.SysUser, error) {
	return s.repo.GetByEmail(ctx, email)
}

// GetByIDStr 接收 loginID 字符串，先 parse 成 uuid 再走 GetByID。
// 无法 parse 时返回 nil, nil（视为未登录/无效）。
func (s *userServiceImpl) GetByIDStr(ctx context.Context, loginID string) (*domain.SysUser, error) {
	id, err := uuid.Parse(loginID)
	if err != nil {
		return nil, nil
	}
	return s.GetByID(ctx, id)
}

// UpdateProfile 修改用户资料。
//
// 校验逻辑与旧 controller.UpdateProfile 一致：
//   - 至少一个字段非 nil；
//   - username 非空；
//   - gender 在 [0,3]；
//   - birthdate 不在未来。
func (s *userServiceImpl) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*UpdateProfileResult, error) {
	if input.Username == nil && input.AvatarURL == nil && input.Phone == nil && input.Gender == nil && input.Birthdate == nil &&
		input.Password == nil && input.ConfirmPassword == nil {
		return nil, errAtLeastOneField
	}

	updateData := make(map[string]interface{})

	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if username == "" {
			return nil, errUsernameEmpty
		}
		updateData["username"] = username
	}
	if input.AvatarURL != nil {
		updateData["avatar_url"] = *input.AvatarURL
	}
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if phone == "" {
			updateData["phone"] = nil
		} else {
			updateData["phone"] = phone
		}
	}
	if input.Gender != nil {
		if *input.Gender < 0 || *input.Gender > 3 {
			return nil, errGenderRange
		}
		updateData["gender"] = *input.Gender
	}
	if input.Birthdate != nil {
		if input.Birthdate.After(time.Now()) {
			return nil, errBirthdateFuture
		}
		updateData["birthdate"] = *input.Birthdate
	}

	// 重置密码：两者必须同时提供（仅传一个 → 报错），长度达标且一致才写入哈希。
	// 不 trim：密码中的空格是有意义的字符；与 auth 注册保持一致。
	if input.Password != nil || input.ConfirmPassword != nil {
		if input.Password == nil || input.ConfirmPassword == nil {
			return nil, errPasswordIncomplete
		}
		if len(*input.Password) < password.MinLength {
			return nil, errPasswordTooShort
		}
		if *input.Password != *input.ConfirmPassword {
			return nil, errPasswordMismatch
		}
		hash, err := password.Hash(*input.Password)
		if err != nil {
			return nil, err
		}
		updateData["pwd"] = hash
	}

	if err := s.repo.UpdateFields(ctx, userID, updateData); err != nil {
		return nil, err
	}

	// 重新读取最新数据返回
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}

	// 刷新用户缓存：用更新后的最新数据覆盖旧缓存，
	// 避免后续 GetByID 回显到修改前的旧资料。
	// 缓存刷新失败不影响更新结果（DB 已是最新，下次 TTL 到期会自愈）。
	_ = s.cache.SetUser(ctx, userID, user)

	return &UpdateProfileResult{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
		Phone:     user.Phone,
		Gender:    user.Gender,
		Birthdate: user.Birthdate,
	}, nil
}

// Search 搜索用户。service 层纯透传（过滤全部下沉 ES），circleID 语义见 UserSearcher.Search。
func (s *userServiceImpl) Search(ctx context.Context, keyword string, size int, searchAfter []interface{}, circleID uuid.UUID) (*UserSearchResult, error) {
	return s.searcher.Search(ctx, keyword, size, searchAfter, circleID)
}

// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（仅含 agent_circle_id 非 NULL
// 或 agent_mention_disabled=1 的行）。直查 repo.GetByIDs 不走缓存：mention 兜底校验低频，
// 数据新鲜度优先（防越圈 / 停用机器人被 @ 依赖投影列即时一致）。
func (s *userServiceImpl) GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentMentionScope, error) {
	result := make(map[uuid.UUID]AgentMentionScope, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	users, err := s.repo.GetByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for id, u := range users {
		if u == nil || (u.AgentCircleID == nil && u.AgentMentionDisabled == 0) {
			continue
		}
		scope := AgentMentionScope{Mentionable: u.AgentMentionDisabled == 0}
		if u.AgentCircleID != nil {
			scope.CircleID = *u.AgentCircleID
		}
		result[id] = scope
	}
	return result, nil
}

// SetAgentMentionable 写机器人账号能否被 @（users.agent_mention_disabled：可 @ 写 0，不可 @ 写 1）。
// 幂等：重复写同值无副作用。写后重读最新行覆盖 userinfo 缓存，避免 GetByID 短期内回显旧实体
// （缓存刷新失败不影响结果，TTL 到期自愈）。只改投影列，不动 status / agent_circle_id。
func (s *userServiceImpl) SetAgentMentionable(ctx context.Context, userID uuid.UUID, mentionable bool) error {
	var disabled int16
	if !mentionable {
		disabled = 1
	}
	if err := s.repo.UpdateFields(ctx, userID, map[string]interface{}{"agent_mention_disabled": disabled}); err != nil {
		return err
	}
	if user, err := s.repo.GetByID(ctx, userID); err == nil && user != nil {
		_ = s.cache.SetUser(ctx, userID, user)
	}
	return nil
}
