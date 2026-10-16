// Package redis 的键名常量按领域拆在同包的 keys_<domain>.go（名字与值不变）：
// keys_auth / keys_user / keys_circle / keys_post / keys_hot / keys_comment / keys_like /
// keys_collect / keys_history / keys_recommend / keys_notice / keys_trending / keys_discover。
//
// 新增 key：在对应领域的 keys_<domain>.go 加前缀常量 + GetXxxKey 构造函数，注释写明
// 完整 key 格式 / 类型 / TTL / 语义；新领域新建 keys_<domain>.go，不要再往本文件追加。
package redis
