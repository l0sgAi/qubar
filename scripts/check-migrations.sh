#!/usr/bin/env bash
# 迁移文件规范检查（CI：ci.yml db job；本机：make migrate-check）。规则见 migrations/README.md。
#
#   scripts/check-migrations.sh [BASE_REF]
#
# 始终检查（全部文件）：命名、无 .down.sql、CONCURRENTLY 文件只能有一条语句。
# 给了 BASE_REF 时额外检查（相对 BASE_REF 的变更）：
#   - 已合并的迁移不许改 / 删 / 改名（ALLOW_MIGRATION_EDIT=true 时只告警：修复生产失败的迁移，PR 标签 migration-fix）；
#   - 新增迁移的版本号必须大于 BASE_REF 上的最大版本号（golang-migrate 只记当前版本，更小的会被静默跳过）；
#   - 新增迁移里 CREATE TABLE domains.<t>：<t> 须在 deploy/ansible/group_vars/all.yml 的 cdc_tables 中，
#     或文件里写明 `-- cdc: none`。
# 不给 BASE_REF（本机）时，CDC 检查作用于全部非基线文件。
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
DIR=migrations
BASE="${1:-}"
fail=0
err() { echo "::error::$*"; fail=1; }

version_of() { basename "$1" | cut -d_ -f1; }
is_baseline() { [[ $(basename "$1") == *_baseline.up.sql ]]; }
# 去掉注释与空行后的 SQL
sql_body() { sed -e 's/--.*$//' "$1" | grep -v '^[[:space:]]*$' || true; }

# ---------- 全量：命名 / down / CONCURRENTLY ----------
shopt -s nullglob
for f in "$DIR"/*; do
  name=$(basename "$f")
  [ "$name" = README.md ] && continue
  if [[ $name == *.down.sql ]]; then
    err "$f: forward-only，不维护 .down.sql"
  elif ! [[ $name =~ ^[0-9]{14}_[a-z0-9_]+\.up\.sql$ ]]; then
    err "$f: 文件名须为 <yyyymmddhhmmss>_<snake_case>.up.sql（用 make migrate-new name=xxx 生成）"
  fi
done

dups=$(for f in "$DIR"/*.up.sql; do version_of "$f"; done | sort | uniq -d)
[ -z "$dups" ] || err "版本号重复: $dups"

for f in "$DIR"/*.up.sql; do
  if sql_body "$f" | grep -qi 'concurrently'; then
    n=$(sql_body "$f" | tr -cd ';' | wc -c | tr -d ' ')
    [ "$n" -le 1 ] || err "$f: CONCURRENTLY 不能在事务内执行，文件里只能有这一条语句（当前 $n 条；lock_timeout 由 qubar_owner 角色默认值兜底）"
  fi
done

# ---------- 相对 BASE：只许新增 + 版本单调 ----------
new_files=()
if [ -n "$BASE" ]; then
  while IFS=$'\t' read -r status path rest; do
    [ "$(basename "$path")" = README.md ] && continue
    case "$status" in
      A) new_files+=("$path") ;;
      *)
        if [ "${ALLOW_MIGRATION_EDIT:-}" = true ]; then
          echo "::warning::$path: 修改已合并的迁移（$status），已由 migration-fix 标签放行"
        else
          err "${rest:-$path}: 已合并的迁移不允许修改 / 删除 / 改名（$status），请新增迁移文件；修复生产失败的迁移见 migrations/README.md"
        fi ;;
    esac
  done < <(git diff --name-status --no-renames "$BASE" HEAD -- "$DIR")

  base_max=$(git ls-tree --name-only "$BASE" "$DIR/" 2>/dev/null \
    | sed -n 's#^.*/\([0-9]\{14\}\)_.*\.up\.sql$#\1#p' | sort | tail -n1)
  if [ -n "$base_max" ]; then
    for f in ${new_files[@]+"${new_files[@]}"}; do
      [ "$(version_of "$f")" \> "$base_max" ] \
        || err "$f: 版本号须大于 $BASE 上的最大版本 $base_max（否则生产会静默跳过），请用 make migrate-new 重新生成文件名"
    done
  fi
else
  for f in "$DIR"/*.up.sql; do new_files+=("$f"); done
fi

# ---------- CDC 登记 ----------
cdc=$(sed -n 's/^cdc_tables:[[:space:]]*\[\(.*\)\].*/\1/p' deploy/ansible/group_vars/all.yml | tr -d ' ' | tr ',' '\n')
[ -n "$cdc" ] || err "未能从 deploy/ansible/group_vars/all.yml 解析 cdc_tables"
for f in ${new_files[@]+"${new_files[@]}"}; do
  [ -f "$f" ] || continue
  is_baseline "$f" && continue
  grep -qiE '^[[:space:]]*--[[:space:]]*cdc:[[:space:]]*none' "$f" && continue
  for t in $(sql_body "$f" | grep -oiE 'create[[:space:]]+table[[:space:]]+(if[[:space:]]+not[[:space:]]+exists[[:space:]]+)?domains\.[a-z0-9_]+' \
    | sed -E 's/.*\.//' | tr '[:upper:]' '[:lower:]'); do
    echo "$cdc" | grep -qx "$t" \
      || err "$f: 新表 domains.$t 未登记 cdc_tables（deploy/ansible/group_vars/all.yml）；不进 CDC 请在文件中写 '-- cdc: none'"
  done
done

[ "$fail" = 0 ] && echo "migrations ok (${#new_files[@]} file(s) checked against ${BASE:-<all>})"
exit "$fail"
