#!/usr/bin/env bash
# 回归重发基线及前置检测漏判草稿。只有中断上传场景允许写入 Release 存储替身。
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
export GITHUB_WORKSPACE="$temp/workspace" STORE="$temp/releases"
export GH_REPO=qubar/rehearsal APP=true WEB=true DIGEST="" REUSE=false PUBLISHED=false
mkdir -p "$GITHUB_WORKSPACE/docs/ops" "$temp/bin" "$temp/site/"{landing,assets,.well-known} "$STORE/web-baseline-v0.9.0"
ln -s "$root/scripts" "$GITHUB_WORKSPACE/scripts"
export PATH="$temp/bin:$PATH"

cat > "$temp/bin/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  'release download') cp "$STORE/$3/$5" "$7/$5" ;;
  'release view') cat "$STORE/$3/details.json" ;;
  'api -X')
    [[ "${REPAIR:-}" == true && "$3" == DELETE && "$4" == "repos/$GH_REPO/releases/assets/42" ]]
    rm "$STORE/$NEXT/candidate.json" ;;
  'release upload')
    [[ "${REPAIR:-}" == true ]]
    target="$STORE/$3/${4##*/}"
    [[ ! -e "$target" || "${5:-}" == --clobber ]]
    cp "$4" "$target" ;;
  *) echo "意外的 Release 操作 $1 $2" >&2; exit 1 ;;
esac
GH
chmod +x "$temp/bin/gh"
site="$temp/site"
echo '<script src="/assets/app.js"></script>' > "$site/index.html"
cp "$site/index.html" "$site/404.html"
echo '{}' > "$site/assets/app.js"
echo '{"associatedApplications":[{}]}' > "$site/.well-known/microsoft-identity-association.json"
echo 'previous' > "$site/landing/index.html"
baseline="$STORE/web-baseline-v0.9.0/pages.tar"
tar -C "$site" -cf "$baseline" .
checksum() { sha256sum "$1" | cut -d' ' -f1; }
manifest="$GITHUB_WORKSPACE/docs/ops/web-baseline-v0.9.0.json"
jq --arg hash "$(checksum "$baseline")" '.pages.sha256 = $hash' "$root/docs/ops/web-baseline-v0.9.0.json" > "$manifest"
export GITHUB_SHA
GITHUB_SHA=$(jq -r .sha "$manifest")
export PREV=v0.9.0 NEXT=v0.9.0

stage() {
  export RUNNER_TEMP="$temp/$1" GITHUB_OUTPUT="$temp/$1.outputs"
  mkdir "$RUNNER_TEMP"
  if [[ "${REPAIR:-}" == true ]]; then
    mkdir "$RUNNER_TEMP/built"
    cp "$bundle" "$RUNNER_TEMP/built/artifact.tar"
  fi
  "$root/scripts/stage-product.sh"
}

PUBLISHED=true stage baseline
expected_image=$(jq -r .image "$manifest")
grep -Fx "digest=${expected_image##*@}" "$GITHUB_OUTPUT"
cmp "$baseline" "$RUNNER_TEMP/candidate/artifact.tar"

NEXT=v0.9.1
mkdir "$STORE/$NEXT"
echo 'candidate' > "$site/landing/index.html"
tar -C "$site" -cf "$STORE/$NEXT/pages.tar" .
candidate_hash=$(checksum "$STORE/$NEXT/pages.tar")
candidate_digest="sha256:$candidate_hash"
jq -n --arg sha "$GITHUB_SHA" --arg prev "$PREV" --arg hash "$candidate_hash" --arg digest "$candidate_digest" \
  '{sha:$sha,previous:$prev,pages_sha256:$hash,image_digest:$digest,app:true,web:true}' > "$STORE/$NEXT/candidate.json"
jq -n --arg sha "$GITHUB_SHA" \
  '{isDraft:true,targetCommitish:$sha,assets:[{name:"pages.tar"},{name:"candidate.json",state:"uploaded"}]}' > "$STORE/$NEXT/details.json"
stage missed-draft
cmp "$STORE/$NEXT/pages.tar" "$RUNNER_TEMP/candidate/artifact.tar"
grep -Fx "pages_hash=$candidate_hash" "$GITHUB_OUTPUT"
grep -Fx "digest=$candidate_digest" "$GITHUB_OUTPUT"

# 清单上传中断时可以重新暂存；完整清单上的同名上传必须被存储替身拒绝。
bundle="$temp/rebuilt.tar"
echo 'rebuilt' > "$site/landing/index.html"
tar -C "$site" -cf "$bundle" .
jq '(.assets[] | select(.name=="candidate.json")) += {state:"starter",apiUrl:"https://api.github.com/repos/qubar/rehearsal/releases/assets/42"}' \
  "$STORE/$NEXT/details.json" > "$temp/interrupted.json"
mv "$temp/interrupted.json" "$STORE/$NEXT/details.json"
: > "$STORE/$NEXT/candidate.json"
REPAIR=true DIGEST="$candidate_digest" stage interrupted
cmp "$bundle" "$STORE/$NEXT/pages.tar"
jq -e --arg hash "$(checksum "$bundle")" '.pages_sha256 == $hash' "$STORE/$NEXT/candidate.json"
