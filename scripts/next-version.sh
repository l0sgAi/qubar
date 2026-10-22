#!/usr/bin/env bash
# 计算下一个发版版本号（CI：release.yml github-release job；本机可直接运行）。#143
#
#   scripts/next-version.sh [REV]      # REV 默认 HEAD
#
# 输出（stdout，key=value，可直接追加到 $GITHUB_OUTPUT）：
#   prev=<上一个版本 tag，没有时为空>
#   next=<下一个版本 tag；REV 已有版本 tag 或没有新提交时为空，表示跳过>
#
# 规则：取 REV 可达的最近一个 v* tag，按其后的非合并提交（Conventional Commits）升级：
#   - 标题带 `!`（feat!: / fix(x)!:）或正文有 `BREAKING CHANGE:`：0.x 阶段升 minor，1.0 起升 major；
#   - 有 feat：升 minor；
#   - 其余（fix / perf / refactor / docs / chore ...）：升 patch。
# 预发布 / 构建后缀被忽略：v0.1.0-alpha 之后有 feat → v0.2.0。
set -euo pipefail

REV="${1:-HEAD}"
log() { echo "$*" >&2; }

if [ -n "$(git tag --points-at "$REV" --list 'v[0-9]*')" ]; then
  log "$REV 已有版本 tag：$(git tag --points-at "$REV" --list 'v[0-9]*' | tr '\n' ' ')，跳过"
  echo "prev="
  echo "next="
  exit 0
fi

prev=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$REV" 2>/dev/null || true)
if [ -n "$prev" ]; then
  range="$prev..$REV"
  base="${prev#v}"
  base="${base%%[-+]*}"
else
  range="$REV"
  base="0.0.0"
fi

if ! [[ "$base" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  log "无法解析上一个版本号：$prev"
  exit 1
fi
major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"

subjects=$(git log --no-merges --format='%s' "$range")
bodies=$(git log --no-merges --format='%b' "$range")
if [ -z "$subjects" ]; then
  log "${prev:-（无 tag）} 之后没有新提交，跳过"
  echo "prev=$prev"
  echo "next="
  exit 0
fi

if grep -qE '^[a-z]+(\([^)]*\))?!:' <<<"$subjects" || grep -qE '^BREAKING[ -]CHANGE:' <<<"$bodies"; then
  bump="breaking"
elif grep -qE '^feat(\([^)]*\))?:' <<<"$subjects"; then
  bump="feat"
else
  bump="patch"
fi

case "$bump" in
  breaking)
    if [ "$major" = 0 ]; then
      minor=$((minor + 1)); patch=0
    else
      major=$((major + 1)); minor=0; patch=0
    fi ;;
  feat) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac

next="v$major.$minor.$patch"
log "${prev:-（无 tag）} → $next（$bump，$(wc -l <<<"$subjects" | tr -d ' ') 个提交）"
echo "prev=$prev"
echo "next=$next"
