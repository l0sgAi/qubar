-- circle_empty_slug_to_null
-- 兼容性：仅数据修正，旧版本应用不受影响（旧版本写入空串仍会碰撞，新版本发布后恢复正常）。
SET lock_timeout = '5s';

-- 存量空串 slug 改 NULL：唯一索引 idx_circle_slug 视 NULL 互不相等，空串会互相碰撞。
UPDATE domains.circle SET slug = NULL WHERE slug = '';
