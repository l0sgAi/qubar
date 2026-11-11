package application

import (
	"fmt"

	"interestBar/pkg/domains/message/domain"
)

// MaxClientMsgIDLen 发送幂等键 client_msg_id 的长度上限（#248）。列宽 varchar(64)；UUID 带连字符 36 位，
// 也容纳 ULID（26 位）/ 自定义前缀等客户端格式。
const MaxClientMsgIDLen = 64

// normalizeClientMsgID 校验发送幂等键（#248）：空串表示不提供，返回 nil；否则必须是 1–MaxClientMsgIDLen 个
// [A-Za-z0-9_-] 字符（白名单内全是 ASCII，无需 SanitizeForPg）。不做大小写归一：键原样比较。
func normalizeClientMsgID(id string) (*string, error) {
	if id == "" {
		return nil, nil
	}
	if len(id) > MaxClientMsgIDLen {
		return nil, fmt.Errorf("%w: at most %d characters", domain.ErrInvalidClientMsgID, MaxClientMsgIDLen)
	}
	for i := 0; i < len(id); i++ {
		if !isClientMsgIDByte(id[i]) {
			return nil, fmt.Errorf("%w: only letters, digits, '-' and '_' are allowed", domain.ErrInvalidClientMsgID)
		}
	}
	return &id, nil
}

func isClientMsgIDByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
}
