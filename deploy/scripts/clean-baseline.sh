#!/usr/bin/env bash
# 本机执行（一次性）：把 `pg_dump --schema-only -n domains --no-owner --no-privileges` 的输出清洗成
# golang-migrate 可执行的基线迁移（migrations/*_baseline.up.sql）。
#
#   deploy/scripts/clean-baseline.sh schema.sql > migrations/20261015000000_baseline.up.sql
#
# 去掉：psql 元命令（PG18 的 \restrict / \unrestrict）、会话 SET / set_config(search_path)、
#       CREATE SCHEMA（schema 由 grants.sql 建）、pg_dump 的注释块与多余空行。
# 保留：CREATE TABLE / INDEX、约束、COMMENT ON。
set -euo pipefail

in="${1:?用法: clean-baseline.sh <pg_dump schema.sql>}"

cat <<'EOF'
-- 基线：生产 domains schema（pg_dump --schema-only 清洗而来，见 deploy/scripts/clean-baseline.sh）。
-- 已有环境用 `migrate force <本文件版本>` 标记为已执行；只有全新空库（CI 回放 / 新环境）会真正执行。
-- 不要修改本文件；表结构变更一律新增迁移文件。

EOF

awk '
  /^\\/                         { next }   # psql 元命令
  /^SET /                       { next }
  /^SELECT pg_catalog\.set_config/ { next }
  /^CREATE SCHEMA /             { next }
  /^--/                         { next }   # pg_dump 注释块
  /^$/ { if (blank++) next; print; next }
  { blank = 0; print }
' "$in" | sed -e '/./,$!d'   # 去掉开头空行
