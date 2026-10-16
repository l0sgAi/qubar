package infrastructure

import (
	"context"
	"encoding/json"

	"interestBar/pkg/domains/user/application"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"

	"github.com/google/uuid"
)

// userSearcherES 基于 pkg/server/storage/elasticsearch.SearchUsers 的实现。
type userSearcherES struct{}

// NewUserSearcher 构造一个基于全局 ES client 的 UserSearcher。
func NewUserSearcher() application.UserSearcher {
	return &userSearcherES{}
}

// Search 调用 ES 搜索用户，转换为 application 层的 UserSearchResult。
// circleID 透传给 ES 作 @提及 圈子作用域过滤（Nil=全站）。
func (s *userSearcherES) Search(ctx context.Context, keyword string, size int, searchAfter []interface{}, circleID uuid.UUID) (*application.UserSearchResult, error) {
	result, err := elasticsearch.SearchUsers(ctx, keyword, size, searchAfter, circleID)
	if err != nil {
		return nil, err
	}

	users := make([]application.UserListItemVO, 0, len(result.Users))
	for _, doc := range result.Users {
		users = append(users, application.UserListItemVO{
			ID:         doc.ID,
			Username:   doc.Username,
			Email:      doc.Email,
			AvatarURL:  doc.AvatarURL,
			Gender:     doc.Gender,
			Role:       doc.Role,
			CreateTime: doc.CreateTime,
		})
	}

	var searchAfterJSON string
	if result.SearchAfter != nil {
		if bytes, err := json.Marshal(result.SearchAfter); err == nil {
			searchAfterJSON = string(bytes)
		}
	}

	return &application.UserSearchResult{
		Total:       result.Total,
		Size:        result.Size,
		SearchAfter: searchAfterJSON,
		Users:       users,
	}, nil
}
