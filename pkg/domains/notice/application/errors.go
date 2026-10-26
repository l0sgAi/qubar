package application

import "errors"

// 应用层哨兵错误 + 谓词（handler 用 IsXxxErr 映射 HTTP 状态码）。
var (
	// errInvalidNoticeType 非法通知类型（type 列表含 1-11 之外的值）。
	errInvalidNoticeType = errors.New("invalid notice type, must be within 1-11")
	// errEmptyNoticeIDs 已读操作未提供通知 ID。
	errEmptyNoticeIDs = errors.New("notice ids must not be empty")
)

// IsInvalidNoticeTypeErr 判断是否非法通知类型错误。
func IsInvalidNoticeTypeErr(err error) bool { return errors.Is(err, errInvalidNoticeType) }

// IsEmptyNoticeIDsErr 判断是否空通知 ID 列表错误。
func IsEmptyNoticeIDsErr(err error) bool { return errors.Is(err, errEmptyNoticeIDs) }
