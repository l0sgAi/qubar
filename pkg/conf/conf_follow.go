// conf_follow.go：配置段 follow，聚合进 AppConfig（conf.go）。

package conf

// DefaultFollowMaxFollowing follow.max_following 未配置（<=0）时的默认值。
const DefaultFollowMaxFollowing = 2000

// Follow 用户关注配置（follow 领域）。设计见 docs/follow-design.md。
type Follow struct {
	MaxFollowing int `mapstructure:"max_following" json:"max_following" yaml:"max_following"` // 每个用户最多关注人数(<=0 兜底 2000)；粉丝数不设上限
}

// MaxFollowingOrDefault 返回关注上限；<=0 时回落 DefaultFollowMaxFollowing。
func (f Follow) MaxFollowingOrDefault() int {
	if f.MaxFollowing <= 0 {
		return DefaultFollowMaxFollowing
	}
	return f.MaxFollowing
}
