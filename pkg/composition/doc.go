// Package composition 是装配根：构造各领域 Service、注入跨领域 Facade、注册路由。
//
// 文件布局（按领域拆分，减少并行开发时的合并冲突）：
//   - server.go：RegisterDomainRoutes，只保留构造顺序、跨领域 setter 注入、路由注册顺序；
//   - wire_<domain>.go：该领域的 newXxxService 构造器与 registerXxx 路由注册；
//   - <domain>_bridges.go：注入该领域的跨领域桥接器（按"被注入方"归档，如 post_bridges.go
//     放 post 依赖的 user / circle / history 端口实现）；多个领域共用的放独立文件
//     （feed_interaction_bridge.go）。
//
// 跨领域 Facade 桥接器是"领域间通过 Facade 通信"模式的核心：
//   - user 领域暴露 UserFacade（GetBriefs/GetBrief）
//   - circle 领域暴露 CircleFacade（GetBriefs）+ CircleMemberChecker + CircleStatusChecker + CirclePostCountPort
//   - post 领域暴露 PostMediaFetcher（GetMediaByPostIDs）
//
// 各领域定义的 Facade 接口字段类型（如 user.application.UserBrief vs
// circle.application.UserBrief）虽然结构相同但是不同类型，
// 因此桥接器需要做字段级转换。
package composition
