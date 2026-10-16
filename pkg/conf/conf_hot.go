// conf_hot.go：配置段 hot，聚合进 AppConfig（conf.go）。

package conf

// Hot 热度计算配置（权重 + 上限）。
type Hot struct {
	Weight HotWeight `mapstructure:"weight" json:"weight" yaml:"weight"`
	Cap    HotCap    `mapstructure:"cap" json:"cap" yaml:"cap"`
}

// HotWeight 各互动事件的热度权重（hot Δ = weight × 方向）。
type HotWeight struct {
	PostLike    int `mapstructure:"post_like" json:"post_like" yaml:"post_like"`          // 帖子点赞
	PostCollect int `mapstructure:"post_collect" json:"post_collect" yaml:"post_collect"` // 帖子收藏
	PostShare   int `mapstructure:"post_share" json:"post_share" yaml:"post_share"`       // 帖子分享（TODO: 分享功能未实现）
	Comment     int `mapstructure:"comment" json:"comment" yaml:"comment"`                // 评论
	CommentLike int `mapstructure:"comment_like" json:"comment_like" yaml:"comment_like"` // 评论点赞
}

// HotCap 热度贡献上限（per-post，防刷分）。
// 不变式：cap 必须为对应 weight 的整数倍，否则 undo 与 clamp 边界产生漂移。
type HotCap struct {
	Comment     int `mapstructure:"comment" json:"comment" yaml:"comment"`                // 评论 hot 贡献上限
	CommentLike int `mapstructure:"comment_like" json:"comment_like" yaml:"comment_like"` // 评论点赞 hot 贡献上限
}
