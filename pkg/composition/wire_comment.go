// Package composition 的 wire_comment.go：comment 领域装配（Service 构造 + 路由注册）。
package composition

import (
	commentapp "interestBar/pkg/domains/comment/application"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	commenthttp "interestBar/pkg/domains/comment/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newCommentService 构造 CommentService。
//
// user/post 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newCommentService(deps *Deps) commentapp.CommentService {
	repo := commentinfra.NewCommentRepository(deps.DB.Get())
	statsCache := commentinfra.NewCommentStatsCache()
	likeCache := commentinfra.NewCommentLikeCache()
	publisher := commentinfra.NewCommentEventPublisher()
	return commentapp.NewCommentService(repo, statsCache, likeCache, publisher)
}

// registerComment 装配 comment 领域。
func registerComment(root routing.RouterGroup, svc commentapp.CommentService, authCheck, optionalCheck routing.HandlerFunc) {
	commenthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
