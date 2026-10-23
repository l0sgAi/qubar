// Package composition 的 wire_agent.go：aiagent 领域装配（全局 / 圈内机器人 Service + 回复执行服务构造 + 路由注册）。
package composition

import (
	agentapp "interestBar/pkg/domains/aiagent/application"
	agentdomain "interestBar/pkg/domains/aiagent/domain"
	agentinfra "interestBar/pkg/domains/aiagent/infrastructure"
	agenthttp "interestBar/pkg/domains/aiagent/interfaces/http"
	commentapp "interestBar/pkg/domains/comment/application"
	postapp "interestBar/pkg/domains/post/application"
	"interestBar/pkg/shared/routing"
)

// newAgentService 构造全局机器人 AgentService，并返回其仓储（圈内 Service 与
// circle 的 agent_count 桥接共享同一实例，无状态薄封装）。
// user 跨领域依赖（role 读取 / 机器人账号）通过 setter 注入（见 RegisterDomainRoutes）。
func newAgentService(deps *Deps) (agentdomain.AgentRepository, agentapp.AgentService) {
	agentRepo := agentinfra.NewAgentRepository(deps.DB.Get())
	return agentRepo, agentapp.NewAgentService(agentRepo)
}

// newAgentReplyService 构造机器人回复执行服务（eino LLM + post/comment 跨域桥接 + 自动回复候选，
// 桥接器见 agent_bridges.go；role 读取 setter 注入见 RegisterDomainRoutes）。
func newAgentReplyService(deps *Deps, postSvc postapp.PostService, commentSvc commentapp.CommentService) agentapp.ReplyService {
	agentRepo := agentinfra.NewAgentRepository(deps.DB.Get())
	replyLogRepo := agentinfra.NewReplyLogRepository(deps.DB.Get())
	llm := agentinfra.NewLLMCaller()
	svc := agentapp.NewReplyService(agentRepo, replyLogRepo, llm)
	svc.SetPostReader(&agentPostReader{delegate: postSvc})
	svc.SetCommentCreator(&agentCommentCreator{delegate: commentSvc})
	svc.SetCandidateReader(&agentCandidateReader{post: postSvc, comment: commentSvc})
	return svc
}

// registerAgent 装配 aiagent 领域（全局机器人 CRUD + 圈子级机器人 CRUD + 手动触发回复；
// role=1 / 圈内 admin+ 校验都在 service 层）。
func registerAgent(
	root routing.RouterGroup,
	svc agentapp.AgentService,
	circleAgentSvc agentapp.CircleAgentService,
	replySvc agentapp.ReplyService,
	authCheck routing.HandlerFunc,
) {
	agenthttp.RegisterRoutes(root, svc, circleAgentSvc, replySvc, authCheck)
}
