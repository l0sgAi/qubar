package application

import (
	"errors"
	"strings"

	"interestBar/pkg/domains/circle/domain"
)

// 领域层可识别错误。
// name/slug 冲突哨兵与 domain 共用：service 预检与 repo 唯一索引兜底（circle_repo_pg.Update）
// 返回同一错误，handler 谓词两条路径通吃。
var (
	errInvalidJoinType    = errors.New("join_type must be 0 (public) or 2 (private)")
	errCircleNameExists   = domain.ErrCircleNameExists
	errCircleSlugExists   = domain.ErrCircleSlugExists
	errCircleNotAvailable = errors.New("this circle is not available for joining")

	// join/leave 流程的错误（与旧 controller 错误信息对应）
	errAlreadyMember    = errors.New("already a member of this circle")
	errBannedFromCircle = errors.New("user is banned from this circle")
	errPrivateCircle    = errors.New("this circle is private and requires invitation")
	errOwnerCannotLeave = errors.New("circle owner cannot leave the circle")
	errNotMember        = errors.New("not a member of this circle")

	// errCircleContentForbidden 私密圈内容仅成员可见（圈内帖子列表对非成员 403）。
	errCircleContentForbidden = errors.New("this circle is private and its content is visible to members only")
)

// 圈子管理（circle management）的错误。
var (
	errNotCircleAdmin        = errors.New("circle admin privileges required")
	errNotCircleOwner        = errors.New("circle owner privileges required")
	errCannotManageTarget    = errors.New("cannot manage a member with equal or higher role")
	errInvalidMemberRole     = errors.New("role must be 10 (member) or 20 (admin); transfer ownership via /circle/manage/transfer")
	errInvalidMuteDuration   = errors.New("duration_hours must be between 1 and 720")
	errInvalidMemberFilter   = errors.New("role filter must be 10/20/30 or -1; status filter must be 0-4 or -1")
	errNoCircleUpdateField   = errors.New("at least one field is required to update")
	errInvalidCircleProfile  = errors.New("invalid circle profile field")
	errUserSearchUnavailable = errors.New("user search service unavailable")
)

// 错误判断函数（供 handler 层使用）。
func IsInvalidJoinTypeErr(err error) bool    { return errors.Is(err, errInvalidJoinType) }
func IsCircleNameExistsErr(err error) bool   { return errors.Is(err, errCircleNameExists) }
func IsCircleSlugExistsErr(err error) bool   { return errors.Is(err, errCircleSlugExists) }
func IsCircleNotAvailableErr(err error) bool { return errors.Is(err, errCircleNotAvailable) }
func IsAlreadyMemberErr(err error) bool      { return errors.Is(err, errAlreadyMember) }
func IsBannedFromCircleErr(err error) bool   { return errors.Is(err, errBannedFromCircle) }
func IsPrivateCircleErr(err error) bool      { return errors.Is(err, errPrivateCircle) }
func IsOwnerCannotLeaveErr(err error) bool   { return errors.Is(err, errOwnerCannotLeave) }
func IsNotMemberErr(err error) bool          { return errors.Is(err, errNotMember) }

// IsCircleContentForbiddenErr 私密圈内容对非成员不可见（→ 403）。
func IsCircleContentForbiddenErr(err error) bool { return errors.Is(err, errCircleContentForbidden) }

// 管理操作错误判断函数。
func IsNotCircleAdminErr(err error) bool        { return errors.Is(err, errNotCircleAdmin) }
func IsNotCircleOwnerErr(err error) bool        { return errors.Is(err, errNotCircleOwner) }
func IsCannotManageTargetErr(err error) bool    { return errors.Is(err, errCannotManageTarget) }
func IsInvalidMemberRoleErr(err error) bool     { return errors.Is(err, errInvalidMemberRole) }
func IsInvalidMuteDurationErr(err error) bool   { return errors.Is(err, errInvalidMuteDuration) }
func IsInvalidMemberFilterErr(err error) bool   { return errors.Is(err, errInvalidMemberFilter) }
func IsNoCircleUpdateFieldErr(err error) bool   { return errors.Is(err, errNoCircleUpdateField) }
func IsInvalidCircleProfileErr(err error) bool  { return errors.Is(err, errInvalidCircleProfile) }
func IsUserSearchUnavailableErr(err error) bool { return errors.Is(err, errUserSearchUnavailable) }

// mapJoinLeaveError 把 memberRepo 返回的错误（字符串匹配）映射为可识别错误。
//
// 旧 model.JoinCircle/LeaveCircle 用 fmt.Errorf 返回字符串错误，
// 这里通过字符串匹配转换。未来 repository 改用 sentinel error 后可简化。
func mapJoinLeaveError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "user is already a member"):
		return errAlreadyMember
	case strings.Contains(msg, "user is banned"):
		return errBannedFromCircle
	case strings.Contains(msg, "private and requires invitation"):
		return errPrivateCircle
	case strings.Contains(msg, "circle owner cannot leave"):
		return errOwnerCannotLeave
	case strings.Contains(msg, "not a member"):
		return errNotMember
	}
	return err
}
