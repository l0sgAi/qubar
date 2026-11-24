#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
npm run build
cmp dist/index.html dist/404.html
test -s dist/landing/index.html
test -s dist/.well-known/microsoft-identity-association.json
ls dist/assets/
