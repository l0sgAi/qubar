-- circle_drop_join_approval
-- 兼容性：仅数据修正 + 注释，旧版本应用照常工作（旧版本对 join_type=2 走私密拒绝、对 status=4 视为已退出）。
-- 入圈审核下线（#168）：加入方式只剩 0=公开 / 2=私密（邀请制），成员不再有待审核状态。
SET lock_timeout = '5s';

-- 存量「需审核」圈子转为私密：圈主原本就要限制谁能进，转公开会放开门槛（转换规则待维护者审核确认）。
UPDATE domains.circle SET join_type = 2, update_time = CURRENT_TIMESTAMP WHERE join_type = 1;

-- 存量待审核申请转为已退出：待审成员本就不计入 member_count，计数不变；私密圈需重新邀请。
UPDATE domains.circle_member SET status = 4, update_time = CURRENT_TIMESTAMP WHERE status = 0;

COMMENT ON COLUMN domains.circle.join_type IS '加入限制：0=公开，2=私密(邀请制)；1=审核已下线';
COMMENT ON COLUMN domains.circle_member.status IS '状态: 1=正常, 2=禁言, 3=拉黑, 4=已退出/被移出；0=申请中已下线';
