#!/usr/bin/env bash
# 版本计算和发布说明共用同一提交集合。导入后的正常提交（含合并分支中的提交）仍计入。
set -euo pipefail
prev=$1
rev=$2
format=$3
frozen=b8f551dc60f23a26835412fbd6a6008bab23d54a
range=("$rev")
if [[ -n "$prev" ]]; then range+=("^$prev"); fi
if git cat-file -e "$frozen^{commit}" 2>/dev/null; then range+=("^$frozen"); fi
git log --no-merges --format="$format" "${range[@]}"
