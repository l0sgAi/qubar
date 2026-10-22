#!/usr/bin/env bash
# 准备压测账号与压测圈子：注册 N 个 lt_* 账号（跳过邮件验证码）→ 账号 0 建圈 → 全员加圈。
# 产物 data/users.json（含 token，已 git-ignore，勿外传）。
#
# 用法：N=200 ./seed-users.sh
# 环境变量：
#   BASE_URL   应用地址，默认 http://127.0.0.1:8888
#   REDIS_CLI  redis-cli 命令前缀，默认开发栈；生产见 README
#   PSQL       psql 命令前缀（超级用户、库 qubar），默认开发栈；生产见 README
#   RUN_ID     账号批次标识，默认当前时间，账号为 lt_<RUN_ID>_<序号>
set -euo pipefail
cd "$(dirname "$0")"

N="${N:-200}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8888}"
REDIS_CLI="${REDIS_CLI:-docker exec -i qubar-dev-redis-1 redis-cli}"
PSQL="${PSQL:-docker exec -i qubar-dev-postgres-1 psql -U postgres -d qubar}"
RUN_ID="${RUN_ID:-$(date +%m%d%H%M)}"
# 每批次一个随机密码，仅用于注册，不落盘（后续全部复用注册返回的 token）
PASSWORD="$(openssl rand -hex 16)"

mkdir -p data
OUT=data/users.json
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

api() { # api <method> <path> [token] [json]
  curl -sS -m 30 -X "$1" "$BASE_URL$2" -H 'Content-Type: application/json' \
    ${3:+-H "satoken: $3"} ${4:+-d "$4"}
}

echo "注册 $N 个账号（RUN_ID=$RUN_ID）..."
for i in $(seq -f '%04g' 0 $((N - 1))); do
  email="lt_${RUN_ID}_${i}@loadtest.invalid"
  # 直接写入"验证码已校验"标记，跳过 send-code / verify（不触达 Mailtrap）
  $REDIS_CLI SET "register:verified:$email" 1 EX 600 >/dev/null
  resp="$(api POST /auth/register/complete "" \
    "{\"email\":\"$email\",\"username\":\"lt_${RUN_ID}_${i}\",\"password\":\"$PASSWORD\",\"device\":\"web\"}")"
  if [ "$(jq -r .code <<<"$resp")" != 200 ]; then
    echo "注册失败 $email: $resp" >&2
    exit 1
  fi
  jq -c '{id: .data.user.id, token: .data.token}' <<<"$resp" >>"$tmp"
done

owner_token="$(head -1 "$tmp" | jq -r .token)"
category_id="$(api GET /category/get | jq -r '.data[0].id // empty')"
if [ -z "$category_id" ]; then
  echo "没有可用分类（category 表为空），先插入一条分类再重试" >&2
  exit 1
fi

circle_name="压测圈 lt_${RUN_ID}"
resp="$(api POST /circle/create "$owner_token" \
  "{\"name\":\"$circle_name\",\"slug\":\"lt-${RUN_ID}\",\"description\":\"k6 压测专用，压测结束后清理\",\"category_id\":\"$category_id\",\"join_type\":0}")"
if [ "$(jq -r .code <<<"$resp")" != 200 ]; then
  echo "建圈失败: $resp" >&2
  exit 1
fi
# 建圈接口不回传 ID，按名称回查
circle_id="$($PSQL -Atc "select id from domains.circle where name = '$circle_name' and deleted = 0")"

echo "加入圈子 $circle_id ..."
tail -n +2 "$tmp" | jq -r .token | while read -r tok; do
  code="$(api POST /circle/join "$tok" "{\"circle_id\":\"$circle_id\"}" | jq -r .code)"
  [ "$code" = 200 ] || { echo "加圈失败 code=$code" >&2; exit 1; }
done

jq -s --arg run "$RUN_ID" --arg circle "$circle_id" \
  '{run_id: $run, circle_id: $circle, users: .}' "$tmp" >"$OUT"
echo "完成：$OUT（$N 个账号，圈子 $circle_id）"
