package domain

// 评论可见性谓词（#208，#184 阶段 1）：评论对读者可见 ⇔ 未删除且状态正常。
// 审核中（CommentStatusReview）、折叠隐藏（CommentStatusHidden）与已删除的评论一律不可见。
//
// 这是唯一定义，读路径全部复用，不再各自手写条件：
//   - SQL：顶层列表、楼内回复、定位 rank 计数与取页起点（infrastructure.visibleScope），
//     帖子 comment_count 重算（pkg/server/storage/redpanda 的 postStatsApplySQL）；
//   - 内存：详情、定位、回复列表的目标 / 根评论判定（Comment.Visible）。
//
// 楼层规则：根评论不可见时整楼不可见（回复列表、定位、回复详情都按「评论不存在」处理），
// 与删除根评论的既有行为一致；帖子 comment_count 仍按行计数，不随根评论连带剔除回复。

// VisibleCondition 可见性谓词的 SQL 片段（不带表别名，列名为 domains.comment 的 deleted / status）。
//
// 字面量必须与 CommentStatusNormal 和 Comment.Visible 同步，visibility_test.go 防漂移。
const VisibleCondition = "deleted = 0 AND status = 1"

// Visible 报告评论是否对读者可见（与 VisibleCondition 同一口径）。
func (c *Comment) Visible() bool {
	return c != nil && c.Deleted == 0 && c.Status == CommentStatusNormal
}
