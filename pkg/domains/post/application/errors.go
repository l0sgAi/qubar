package application

import (
	"errors"
	"time"
)

// 领域层可识别错误。
var (
	errCircleIDRequired   = errors.New("circle_id is required")
	errTitleRequired      = errors.New("title is required")
	errNotMember          = errors.New("you are not a member of this circle")
	errCircleNotAvailable = errors.New("this circle is not available for posting")
	errMembershipPending  = errors.New("your membership is still pending approval")
	errBannedFromCircle   = errors.New("you have been banned from this circle")
)

// errMutedUntil 是带参数的错误（禁言截止时间）。
type mutedError struct{ until time.Time }

func (e *mutedError) Error() string {
	return "you are muted until " + e.until.Format("2006-01-02 15:04:05")
}

func errMutedUntil(t time.Time) error {
	return &mutedError{until: t}
}

// IsMutedErr 判断是否为"被禁言"错误，并返回截止时间。
func IsMutedErr(err error) (time.Time, bool) {
	if e, ok := err.(*mutedError); ok {
		return e.until, true
	}
	return time.Time{}, false
}

// 错误判断函数（供 handler 层使用）。
func IsCircleIDRequiredErr(err error) bool   { return errors.Is(err, errCircleIDRequired) }
func IsTitleRequiredErr(err error) bool      { return errors.Is(err, errTitleRequired) }
func IsNotMemberErr(err error) bool          { return errors.Is(err, errNotMember) }
func IsCircleNotAvailableErr(err error) bool { return errors.Is(err, errCircleNotAvailable) }
func IsMembershipPendingErr(err error) bool  { return errors.Is(err, errMembershipPending) }
func IsBannedFromCircleErr(err error) bool   { return errors.Is(err, errBannedFromCircle) }
