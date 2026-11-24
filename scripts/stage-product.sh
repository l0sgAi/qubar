#!/usr/bin/env bash
# 先持久保存两份静态包，再允许迁移和部署。candidate.json 是候选产物已齐备的标志。
set -euo pipefail
: "${DIGEST?需要传入构建 digest，静态发布时为空}"
cd "$RUNNER_TEMP"
mkdir -p previous candidate
if [[ "$PREV" == v0.9.0 ]]; then
  previous_tag=web-baseline-v0.9.0
  previous_hash=$(jq -er .pages.sha256 "$GITHUB_WORKSPACE/docs/ops/web-baseline-v0.9.0.json")
else
  previous_tag=$PREV
  gh release download "$PREV" --pattern manifest.json --dir previous
  previous_hash=$(jq -er '.pages.sha256' previous/manifest.json)
fi
gh release download "$previous_tag" --pattern pages.tar --dir previous
test "$(sha256sum previous/pages.tar | cut -d' ' -f1)" = "$previous_hash"
python3 "$GITHUB_WORKSPACE/scripts/verify-pages.py" previous/pages.tar
mv previous/pages.tar previous/artifact.tar

if [[ "${PUBLISHED:-}" == true ]]; then
  cp previous/artifact.tar candidate/pages.tar
  hash=$previous_hash
  digest=""
  if [[ "$APP" == true ]]; then
    image=$(jq -er --arg sha "$GITHUB_SHA" 'select(.sha == $sha) | .image' previous/manifest.json)
    digest=${image##*@}
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]
  fi
elif [[ "$REUSE" == true ]]; then
  gh release download "$NEXT" --pattern candidate.json --dir candidate
  gh release download "$NEXT" --pattern pages.tar --dir candidate
  jq -e --arg sha "$GITHUB_SHA" --arg prev "$PREV" --argjson app "$APP" --argjson web "$WEB" '.sha == $sha and .previous == $prev and .app == $app and .web == $web' candidate/candidate.json >/dev/null
  digest=$(jq -r .image_digest candidate/candidate.json)
  hash=$(jq -er .pages_sha256 candidate/candidate.json)
  test "$(sha256sum candidate/pages.tar | cut -d' ' -f1)" = "$hash"
else
  if [[ "$WEB" == true ]]; then
    cp built/artifact.tar candidate/pages.tar
  else
    cp previous/artifact.tar candidate/pages.tar
  fi
  hash=$(sha256sum candidate/pages.tar | cut -d' ' -f1)
  digest=$DIGEST
  if [[ "$APP" == true ]]; then [[ "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; fi
  jq -n --arg sha "$GITHUB_SHA" --arg prev "$PREV" --arg hash "$hash" --arg digest "$DIGEST" \
    --argjson app "$APP" --argjson web "$WEB" \
    '{sha:$sha, previous:$prev, pages_sha256:$hash, image_digest:$digest, app:$app, web:$web}' > candidate/candidate.json
  if gh release view "$NEXT" --json isDraft,targetCommitish > draft.json; then
    jq -e --arg sha "$GITHUB_SHA" '.isDraft and .targetCommitish == $sha' draft.json >/dev/null
  else
    gh release create "$NEXT" --draft --target "$GITHUB_SHA" --title "$NEXT" --notes "候选产物，等待部署和冒烟。"
  fi
  gh release upload "$NEXT" candidate/pages.tar --clobber
  gh release upload "$NEXT" candidate/candidate.json
fi
python3 "$GITHUB_WORKSPACE/scripts/verify-pages.py" candidate/pages.tar
echo "digest=$digest" >> "$GITHUB_OUTPUT"
echo "pages_hash=$hash" >> "$GITHUB_OUTPUT"
mv candidate/pages.tar candidate/artifact.tar
