package domain

import "errors"

// ErrSearchOverloaded 搜索后端（ES）线程池 / 队列已满，属于可重试的暂时错误。
// 由 infrastructure 层包装返回，接口层映射为 503 而不是 500。
var ErrSearchOverloaded = errors.New("search backend overloaded")
