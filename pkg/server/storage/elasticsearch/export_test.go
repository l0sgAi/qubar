// export_test.go 向外部测试包（elasticsearch_test）暴露内部函数。
// 集成测试必须放在外部测试包：testkit 经 redpanda → discover 间接依赖本包，内部测试包 import testkit 会成环。
package elasticsearch

// SearchUsersInIndex 暴露 searchUsersInIndex（集成测试对临时索引执行真实用户搜索 DSL）。
var SearchUsersInIndex = searchUsersInIndex
