package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"
)

var (
	// Client Redis客户端实例
	Client *redis.Client
	ctx    = context.Background()
)

// InitRedis 初始化Redis连接
func InitRedis(addr, password string, db int) error {
	Client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 测试连接
	_, err := Client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}

	return nil
}

// CloseRedis 关闭Redis连接
func CloseRedis() error {
	if Client != nil {
		return Client.Close()
	}
	return nil
}

// Set 设置键值对
func Set(key string, value interface{}, expiration time.Duration) error {
	return Client.Set(ctx, key, value, expiration).Err()
}

// Get 获取键值
func Get(key string) (string, error) {
	return Client.Get(ctx, key).Result()
}

// Del 删除键
func Del(keys ...string) error {
	return Client.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在
func Exists(keys ...string) (int64, error) {
	return Client.Exists(ctx, keys...).Result()
}

// Expire 设置键的过期时间
func Expire(key string, expiration time.Duration) error {
	return Client.Expire(ctx, key, expiration).Err()
}

// SetJSON 设置JSON对象（自动序列化）
func SetJSON(key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Client.Set(ctx, key, data, expiration).Err()
}

// GetJSON 获取JSON对象（自动反序列化）
func GetJSON(key string, dest interface{}) error {
	data, err := Client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// BatchSet 批量设置键值对（使用 Pipeline）
// keyValues 是一个 map，key 是 Redis key，value 是要设置的值
func BatchSet(keyValues map[string]interface{}, expiration time.Duration) error {
	if len(keyValues) == 0 {
		return nil
	}

	pipe := Client.Pipeline()
	for key, value := range keyValues {
		pipe.Set(ctx, key, value, expiration)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Log.Warn(
			fmt.Sprintf("Redis BatchSet failed, keyCount=%d, err=%v",
				len(keyValues), err,
			),
		)
	}
	return err
}

// Incr 原子递增键值，并设置过期时间
// 如果键不存在则从0开始递增
// 返回递增后的值
func Incr(key string, expiration time.Duration) (int64, error) {
	pipe := Client.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, expiration)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to increment key %s: %w", key, err)
	}

	return incrCmd.Result()
}

// Decr 原子递减键值，并设置过期时间
// 如果键不存在则从0开始递减（结果为-1，调用方需要处理）
// 返回递减后的值（可能为负数）
func Decr(key string, expiration time.Duration) (int64, error) {
	pipe := Client.Pipeline()
	decrCmd := pipe.Decr(ctx, key)
	pipe.Expire(ctx, key, expiration)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to decrement key %s: %w", key, err)
	}

	result, err := decrCmd.Result()
	if err != nil {
		return 0, err
	}

	// 确保不会小于0
	if result < 0 {
		// 如果小于0，重置为0
		err = Client.Set(ctx, key, 0, expiration).Err()
		if err != nil {
			return 0, fmt.Errorf("failed to reset negative count: %w", err)
		}
		return 0, nil
	}

	return result, nil
}

var (
	// zstdEncoder Zstd 编码器复用（性能优化）
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	// zstdDecoder Zstd 解码器复用
	zstdDecoder, _ = zstd.NewReader(nil)
)

// compressZstd 使用 Zstd 压缩数据
func compressZstd(data []byte) []byte {
	return zstdEncoder.EncodeAll(data, make([]byte, 0, len(data)))
}

// decompressZstd 使用 Zstd 解压数据
func decompressZstd(data []byte) ([]byte, error) {
	return zstdDecoder.DecodeAll(data, nil)
}

// SetJSONCompressed 将对象序列化为JSON并压缩后存储到Redis
func SetJSONCompressed(key string, value interface{}, expiration time.Duration) error {
	// 1. 序列化为 JSON
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// 2. Zstd 压缩
	compressed := compressZstd(jsonData)

	// 3. 存储到 Redis
	err = Client.Set(ctx, key, compressed, expiration).Err()
	if err != nil {
		return fmt.Errorf("failed to set compressed data: %w", err)
	}

	// 记录压缩效果
	if len(jsonData) > 1024 { // 只对大于 1KB 的数据记录日志
		logger.Log.Debug(
			fmt.Sprintf("Zstd compressed: key=%s, original=%d, compressed=%d, ratio=%.2f%%",
				key, len(jsonData), len(compressed),
				float64(len(compressed))/float64(len(jsonData))*100,
			),
		)
	}

	return nil
}

// GetJSONCompressed 从Redis获取压缩数据，解压并反序列化为对象
func GetJSONCompressed(key string, value interface{}) error {
	// 1. 获取压缩数据
	compressed, err := Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return err // 键不存在
		}
		return fmt.Errorf("failed to get compressed data: %w", err)
	}

	// 2. Zstd 解压
	jsonData, err := decompressZstd(compressed)
	if err != nil {
		return fmt.Errorf("failed to decompress data: %w", err)
	}

	// 3. 反序列化 JSON
	if err := json.Unmarshal(jsonData, value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// UpdateCircleStatistics 更新圈子统计信息缓存到Hash
func UpdateCircleStatistics(circleID uuid.UUID, statistics *CircleStatistics) error {
	key := GetCircleStatsKey(circleID)
	pipe := Client.Pipeline()
	pipe.HSet(ctx, key, "member_count", statistics.MemberCount)
	pipe.HSet(ctx, key, "post_count", statistics.PostCount)
	pipe.HSet(ctx, key, "hot", statistics.Hot)
	pipe.Expire(ctx, key, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update circle statistics: %w", err)
	}

	return nil
}

// GetCircleStatistics 获取圈子统计信息（从Hash读取）
func GetCircleStatistics(circleID uuid.UUID) (*CircleStatistics, error) {
	key := GetCircleStatsKey(circleID)
	values, err := Client.HMGet(ctx, key, "member_count", "post_count", "hot").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get circle statistics: %w", err)
	}

	// 如果任一字段不存在，返回nil表示需要从数据库恢复
	if values[0] == nil || values[1] == nil || values[2] == nil {
		return nil, nil
	}

	stats := &CircleStatistics{}
	if memberCount, ok := values[0].(string); ok {
		if mc, err := strconv.Atoi(memberCount); err == nil {
			stats.MemberCount = mc
		}
	}
	if postCount, ok := values[1].(string); ok {
		if pc, err := strconv.Atoi(postCount); err == nil {
			stats.PostCount = pc
		}
	}
	if hot, ok := values[2].(string); ok {
		if h, err := strconv.Atoi(hot); err == nil {
			stats.Hot = h
		}
	}

	return stats, nil
}

// CircleStatisticsExists 检查圈子统计 Hash 是否已回种（以 member_count 字段为准，见 circleStatsSeededField）。
func CircleStatisticsExists(circleID uuid.UUID) (bool, error) {
	return Client.HExists(ctx, GetCircleStatsKey(circleID), circleStatsSeededField).Result()
}

// IncrementCircleMemberCount 增加圈子成员数量（原子操作；Hash 未回种时不写，见 incrCircleStatIfSeededScript）
func IncrementCircleMemberCount(circleID uuid.UUID) error {
	return incrCircleStat(circleID, "member_count", 1)
}

// DecrementCircleMemberCount 减少圈子成员数量（原子操作，结果不小于 0；Hash 未回种时不写）
func DecrementCircleMemberCount(circleID uuid.UUID) error {
	return incrCircleStat(circleID, "member_count", -1)
}

// IncrementCirclePostCount 增加圈子帖子数量（原子操作；Hash 未回种时不写）
func IncrementCirclePostCount(circleID uuid.UUID) error {
	return incrCircleStat(circleID, "post_count", 1)
}

// DecrementCirclePostCount 减少圈子帖子数量（原子操作）
func DecrementCirclePostCount(circleID uuid.UUID) error {
	key := GetCircleStatsKey(circleID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "post_count", -1)
	pipe.Expire(ctx, key, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement post count: %w", err)
	}

	// 检查结果，确保不会小于0
	newCount := decrCmd.Val()
	if newCount < 0 {
		// 如果小于0，重置为0
		err = Client.HSet(ctx, key, "post_count", 0).Err()
		if err != nil {
			return fmt.Errorf("failed to reset negative post count: %w", err)
		}
	}

	return nil
}

// IncrementCircleHot 增加圈子热度（原子操作）
func IncrementCircleHot(circleID uuid.UUID, increment int64) error {
	key := GetCircleStatsKey(circleID)
	pipe := Client.Pipeline()
	incrCmd := pipe.HIncrBy(ctx, key, "hot", increment)
	pipe.Expire(ctx, key, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to increment hot: %w", err)
	}

	// 记录结果
	newHot := incrCmd.Val()
	logger.Log.Debug(fmt.Sprintf("Circle hot incremented: circleID=%s, increment=%d, newHot=%d", circleID.String(), increment, newHot))

	return nil
}

// DecrementCircleHot 减少圈子热度（原子操作）
func DecrementCircleHot(circleID uuid.UUID, decrement int64) error {
	key := GetCircleStatsKey(circleID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "hot", -decrement)
	pipe.Expire(ctx, key, 24*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement hot: %w", err)
	}

	// 检查结果，确保不会小于0
	newHot := decrCmd.Val()
	if newHot < 0 {
		// 如果小于0，重置为0
		err = Client.HSet(ctx, key, "hot", 0).Err()
		if err != nil {
			return fmt.Errorf("failed to reset negative hot: %w", err)
		}
		newHot = 0
	}

	logger.Log.Debug(fmt.Sprintf("Circle hot decremented: circleID=%s, decrement=%d, newHot=%d", circleID.String(), decrement, newHot))

	return nil
}

// BatchUpdateCircleStatistics 批量更新圈子统计信息（用于MQ消费等场景）
func BatchUpdateCircleStatistics(updates map[uuid.UUID]*CircleStatistics) error {
	if len(updates) == 0 {
		return nil
	}

	// 使用Pipeline批量设置多个圈子的统计信息到Hash
	pipe := Client.Pipeline()
	for circleID, stats := range updates {
		key := GetCircleStatsKey(circleID)
		pipe.HSet(ctx, key, "member_count", stats.MemberCount)
		pipe.HSet(ctx, key, "post_count", stats.PostCount)
		pipe.HSet(ctx, key, "hot", stats.Hot)
		pipe.Expire(ctx, key, 24*time.Hour)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("Batch update circle statistics failed: count=%d, err=%v", len(updates), err))
		return err
	}

	logger.Log.Debug(fmt.Sprintf("Batch update circle statistics success: count=%d", len(updates)))
	return nil
}

// ==================== 帖子统计信息操作 ====================

const postStatsTTL = 43 * time.Minute

// PostStatisticsExists 检查帖子统计信息Hash是否存在
func PostStatisticsExists(postID uuid.UUID) (bool, error) {
	key := GetPostStatsKey(postID)
	exists, err := Client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// UpdatePostStatistics 更新帖子统计信息缓存到Hash
func UpdatePostStatistics(postID uuid.UUID, statistics *PostStatistics) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	pipe.HSet(ctx, key, "view_count", statistics.ViewCount)
	pipe.HSet(ctx, key, "comment_count", statistics.CommentCount)
	pipe.HSet(ctx, key, "like_count", statistics.LikeCount)
	pipe.HSet(ctx, key, "collect_count", statistics.CollectCount)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update post statistics: %w", err)
	}
	return nil
}

// GetPostStatistics 获取帖子统计信息（从Hash读取）
func GetPostStatistics(postID uuid.UUID) (*PostStatistics, error) {
	key := GetPostStatsKey(postID)
	values, err := Client.HMGet(ctx, key, "view_count", "comment_count", "like_count", "collect_count").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get post statistics: %w", err)
	}

	// 如果任一字段不存在，返回nil表示需要从数据库恢复
	if values[0] == nil || values[1] == nil || values[2] == nil || values[3] == nil {
		return nil, nil
	}

	stats := &PostStatistics{}
	if vc, ok := values[0].(string); ok {
		if v, err := strconv.Atoi(vc); err == nil {
			stats.ViewCount = v
		}
	}
	if cc, ok := values[1].(string); ok {
		if v, err := strconv.Atoi(cc); err == nil {
			stats.CommentCount = v
		}
	}
	if lc, ok := values[2].(string); ok {
		if v, err := strconv.Atoi(lc); err == nil {
			stats.LikeCount = v
		}
	}
	if colc, ok := values[3].(string); ok {
		if v, err := strconv.Atoi(colc); err == nil {
			stats.CollectCount = v
		}
	}

	return stats, nil
}

// GetPostStatisticsBatch 批量获取帖子统计（pipeline 打包 N 个 HMGet，1 RTT）。
// 仅返回字段齐全的 postID -> stats；缺失/部分缺失的不计入（调用方走 DB/ES baseline）。
func GetPostStatisticsBatch(postIDs []uuid.UUID) (map[uuid.UUID]*PostStatistics, error) {
	result := make(map[uuid.UUID]*PostStatistics, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}

	pipe := Client.Pipeline()
	cmds := make([]*redis.SliceCmd, len(postIDs))
	for i, pid := range postIDs {
		cmds[i] = pipe.HMGet(ctx, GetPostStatsKey(pid), "view_count", "comment_count", "like_count", "collect_count")
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to batch get post statistics: %w", err)
	}

	for i, cmd := range cmds {
		values, err := cmd.Result()
		if err != nil || len(values) < 4 {
			continue
		}
		// 任一字段缺失视为该帖缓存未建立，不计入
		if values[0] == nil || values[1] == nil || values[2] == nil || values[3] == nil {
			continue
		}
		result[postIDs[i]] = &PostStatistics{
			ViewCount:    parseStatCount(values[0]),
			CommentCount: parseStatCount(values[1]),
			LikeCount:    parseStatCount(values[2]),
			CollectCount: parseStatCount(values[3]),
		}
	}
	return result, nil
}

// SeedPostStatisticsIfAbsent 仅当字段不存在时回种（HSetNX）。
// 用于详情读路径 miss 时回种：async 浏览量 goroutine 会 HINCRBY view_count，
// 普通 HSet 会 clobber 掉它的 +1；HSetNX 保证只写"尚未建立"的字段。
func SeedPostStatisticsIfAbsent(postID uuid.UUID, statistics *PostStatistics) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	pipe.HSetNX(ctx, key, "view_count", statistics.ViewCount)
	pipe.HSetNX(ctx, key, "comment_count", statistics.CommentCount)
	pipe.HSetNX(ctx, key, "like_count", statistics.LikeCount)
	pipe.HSetNX(ctx, key, "collect_count", statistics.CollectCount)
	pipe.Expire(ctx, key, postStatsTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to seed post statistics: %w", err)
	}
	return nil
}

// parseStatCount 把 HMGet 返回的 interface{} 解析为 int（非法/空→0）。
func parseStatCount(v interface{}) int {
	if s, ok := v.(string); ok {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 0
}

// IncrementPostViewCount 增加帖子浏览量（原子操作，带去重和上限检查）
// 返回值：>0=新浏览量, 0=去重跳过, -1=已达上限
// 注意：此函数需要先调用 restorePostStatsIfNeed 确保 Redis 缓存存在
func IncrementPostViewCount(postID, userID uuid.UUID) (int64, error) {
	return IncrementPostViewCountWithDedup(postID, userID)
}

// IncrementPostCommentCount 增加帖子评论数（原子操作）
func IncrementPostCommentCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	pipe.HIncrBy(ctx, key, "comment_count", 1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to increment post comment count: %w", err)
	}
	return nil
}

// DecrementPostCommentCount 减少帖子评论数（原子操作）
func DecrementPostCommentCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "comment_count", -1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement post comment count: %w", err)
	}

	newCount := decrCmd.Val()
	if newCount < 0 {
		err = Client.HSet(ctx, key, "comment_count", 0).Err()
		if err != nil {
			return fmt.Errorf("failed to reset negative comment count: %w", err)
		}
	}
	return nil
}

// IncrementPostLikeCount 增加帖子点赞数（原子操作）
func IncrementPostLikeCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	pipe.HIncrBy(ctx, key, "like_count", 1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to increment post like count: %w", err)
	}
	return nil
}

// DecrementPostLikeCount 减少帖子点赞数（原子操作）
func DecrementPostLikeCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "like_count", -1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement post like count: %w", err)
	}

	newCount := decrCmd.Val()
	if newCount < 0 {
		err = Client.HSet(ctx, key, "like_count", 0).Err()
		if err != nil {
			return fmt.Errorf("failed to reset negative like count: %w", err)
		}
	}
	return nil
}

// IncrementPostCollectCount 增加帖子收藏数（原子操作）
func IncrementPostCollectCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	pipe.HIncrBy(ctx, key, "collect_count", 1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to increment post collect count: %w", err)
	}
	return nil
}

// DecrementPostCollectCount 减少帖子收藏数（原子操作）
func DecrementPostCollectCount(postID uuid.UUID) error {
	key := GetPostStatsKey(postID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "collect_count", -1)
	pipe.Expire(ctx, key, postStatsTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement post collect count: %w", err)
	}

	newCount := decrCmd.Val()
	if newCount < 0 {
		err = Client.HSet(ctx, key, "collect_count", 0).Err()
		if err != nil {
			return fmt.Errorf("failed to reset negative collect count: %w", err)
		}
	}
	return nil
}

// BatchUpdatePostStatistics 批量更新帖子统计信息（用于MQ消费等场景）
func BatchUpdatePostStatistics(updates map[uuid.UUID]*PostStatistics) error {
	if len(updates) == 0 {
		return nil
	}

	pipe := Client.Pipeline()
	for postID, stats := range updates {
		key := GetPostStatsKey(postID)
		pipe.HSet(ctx, key, "view_count", stats.ViewCount)
		pipe.HSet(ctx, key, "comment_count", stats.CommentCount)
		pipe.HSet(ctx, key, "like_count", stats.LikeCount)
		pipe.HSet(ctx, key, "collect_count", stats.CollectCount)
		pipe.Expire(ctx, key, postStatsTTL)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("Batch update post statistics failed: count=%d, err=%v", len(updates), err))
		return err
	}

	logger.Log.Debug(fmt.Sprintf("Batch update post statistics success: count=%d", len(updates)))
	return nil
}

// ==================== 评论统计信息操作 ====================

// CommentStatisticsExists 检查评论统计信息Hash是否存在
// ctx 由调用方传入，使超时/取消得以传播（覆盖包级 background ctx）。
func CommentStatisticsExists(ctx context.Context, commentID uuid.UUID) (bool, error) {
	key := GetCommentStatsKey(commentID)
	exists, err := Client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// UpdateCommentStatistics 更新评论统计信息缓存到Hash
// ctx 由调用方传入，使超时/取消得以传播（覆盖包级 background ctx）。
func UpdateCommentStatistics(ctx context.Context, commentID uuid.UUID, likeCount int) error {
	key := GetCommentStatsKey(commentID)
	pipe := Client.Pipeline()
	pipe.HSet(ctx, key, "like_count", likeCount)
	pipe.Expire(ctx, key, postStatsTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update comment statistics: %w", err)
	}
	return nil
}

// GetCommentLikeCount 获取评论点赞数（从Hash读取）
func GetCommentLikeCount(commentID uuid.UUID) (int, error) {
	key := GetCommentStatsKey(commentID)
	val, err := Client.HGet(ctx, key, "like_count").Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get comment like count: %w", err)
	}
	count, err := strconv.Atoi(val)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// IncrementCommentLikeCount 增加评论点赞数（原子操作）
func IncrementCommentLikeCount(commentID uuid.UUID) error {
	key := GetCommentStatsKey(commentID)
	pipe := Client.Pipeline()
	pipe.HIncrBy(ctx, key, "like_count", 1)
	pipe.Expire(ctx, key, postStatsTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to increment comment like count: %w", err)
	}
	return nil
}

// DecrementCommentLikeCount 减少评论点赞数（原子操作）
func DecrementCommentLikeCount(commentID uuid.UUID) error {
	key := GetCommentStatsKey(commentID)
	pipe := Client.Pipeline()
	decrCmd := pipe.HIncrBy(ctx, key, "like_count", -1)
	pipe.Expire(ctx, key, postStatsTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to decrement comment like count: %w", err)
	}
	newCount := decrCmd.Val()
	if newCount < 0 {
		if err := Client.HSet(ctx, key, "like_count", 0).Err(); err != nil {
			return fmt.Errorf("failed to reset negative comment like count: %w", err)
		}
	}
	return nil
}
