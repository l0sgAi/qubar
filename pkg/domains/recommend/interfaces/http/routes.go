package http

import (
	"interestBar/pkg/domains/recommend/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 recommend 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /post/home?tab=...  首页信息流（访客可读 hot/latest；recommend/following 需登录）
//	  tab=recommend  推荐流（候选池 offset + pool_token；5 路召回 + CF）—— 需登录，匿名→401
//	  tab=hot         全局热门（rank_score 时间衰减，search_after 翻页）—— 访客可读
//	  tab=latest      全局最新（create_time desc，search_after 翻页）—— 访客可读
//	  tab=following   关注流（已加入圈子的帖子 + 关注的用户发的帖子，时间倒序，search_after 翻页）—— 需登录，匿名→401
//
// 复用 /post 前缀（home 是 post 列表的 tab 化入口）。与 post 域共用 /post 前缀但不冲突
// （本域只注册 /post/home 单条路径，post 域注册 /post/list 等）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.RecommendService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)
	// /post/home 组级可选登录：handler/service 按 tab 分支决定是否需要 userID。
	// authCheck 参数保留以统一注册签名（本域仅此一条路径，无需拆登录子组）。
	_ = authCheck
	p := rg.Group("/post", optionalCheck)
	{
		p.GET("/home", h.GetHomeFeed)
	}
}
