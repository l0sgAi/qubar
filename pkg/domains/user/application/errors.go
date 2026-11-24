package application

import "errors"

// 领域层定义的"可识别错误"。HTTP handler 层据此映射到对应的响应码/消息。
var (
	errAtLeastOneField  = errors.New("at least one field must be provided")
	errUsernameEmpty    = errors.New("username cannot be empty")
	errGenderRange      = errors.New("gender must be 0 (unknown), 1 (male), 2 (female) or 3 (others)")
	errBirthdateFuture  = errors.New("birthdate cannot be in the future")
	errPasswordTooShort = errors.New("password must be at least 6 characters")
	errPasswordMismatch = errors.New("password and confirm password do not match")
	// errPasswordIncomplete 表示 password 与 confirm_password 只传了其中一个。
	errPasswordIncomplete = errors.New("password and confirm_password must be provided together")
)

// IsAtLeastOneFieldErr 判断是否为"至少修改一个字段"错误（供 handler 层判断）。
func IsAtLeastOneFieldErr(err error) bool { return errors.Is(err, errAtLeastOneField) }

// IsUsernameEmptyErr 判断是否为"用户名不能为空"错误。
func IsUsernameEmptyErr(err error) bool { return errors.Is(err, errUsernameEmpty) }

// IsGenderRangeErr 判断是否为"性别值非法"错误。
func IsGenderRangeErr(err error) bool { return errors.Is(err, errGenderRange) }

// IsBirthdateFutureErr 判断是否为"生日不能在未来"错误。
func IsBirthdateFutureErr(err error) bool { return errors.Is(err, errBirthdateFuture) }

// IsPasswordTooShortErr 判断是否为"密码过短"错误。
func IsPasswordTooShortErr(err error) bool { return errors.Is(err, errPasswordTooShort) }

// IsPasswordMismatchErr 判断是否为"两次密码不一致"错误。
func IsPasswordMismatchErr(err error) bool { return errors.Is(err, errPasswordMismatch) }

// IsPasswordIncompleteErr 判断是否为"password 与 confirm_password 未成对提供"错误。
func IsPasswordIncompleteErr(err error) bool { return errors.Is(err, errPasswordIncomplete) }

// 用户设置（setting.go）的可识别错误。
var (
	errNoSettingField       = errors.New("at least one setting must be provided")
	errFollowListVisibility = errors.New("follow_list_visibility must be 0 (public) or 1 (private)")
	errDMPermission         = errors.New("dm_permission must be 0 (everyone), 1 (people I follow), 2 (mutual follows) or 3 (no one)")
)

// IsNoSettingFieldErr 判断是否为"至少修改一个设置项"错误。
func IsNoSettingFieldErr(err error) bool { return errors.Is(err, errNoSettingField) }

// IsFollowListVisibilityErr 判断是否为"列表可见性取值非法"错误。
func IsFollowListVisibilityErr(err error) bool { return errors.Is(err, errFollowListVisibility) }

// IsDMPermissionErr 判断是否为"私信权限取值非法"错误。
func IsDMPermissionErr(err error) bool { return errors.Is(err, errDMPermission) }
