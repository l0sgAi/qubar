// 压测场景。SCENARIO 选链路，PROFILE 选节奏（见 lib/common.js scenarioFor）。
//   read   读链路：推荐 / 热门 / 最新流、热榜、发现页、ES 搜索、帖子详情（浏览记录 → Redpanda）、评论列表
//   write  写链路：点赞 / 收藏（Lua + Redpanda 异步落库）、评论、发帖（→ CDC → ES）
//   mixed  读 90% + 写 10%，贴近真实流量
//   sse    长连：每 VU 持有一条 /notice/stream（每用户上限 5 条）
// 用法：k6 run -e SCENARIO=read -e PROFILE=stress -e RATE=400 scenarios.js
// 每次迭代只发一个请求，arrival-rate 执行器下 RATE 即目标 RPS。
import http from 'k6/http';
import { check, sleep } from 'k6';
import { BASE_URL, CIRCLE_ID, KEYWORDS, RUN_ID, commentIds, get, pick, post, postIds, rootCommentIds, scenarioFor, thresholds, vuUser } from './lib/common.js';

const SCENARIO = __ENV.SCENARIO || 'read';

function buildOptions() {
  if (SCENARIO === 'sse') {
    return {
      scenarios: {
        sse: {
          executor: 'ramping-vus',
          exec: 'sse',
          startVUs: 0,
          stages: [
            { target: Number(__ENV.SSE_VUS || 500), duration: __ENV.RAMP || '2m' },
            { target: Number(__ENV.SSE_VUS || 500), duration: __ENV.DURATION || '5m' },
          ],
          gracefulStop: '70s',
        },
      },
    };
  }
  return { scenarios: { [SCENARIO]: scenarioFor(SCENARIO) }, thresholds, discardResponseBodies: false };
}
export const options = buildOptions();

const q = encodeURIComponent;

// 读接口表：[权重, name, 请求路径生成]；ENDPOINT=<name> 时只压该接口（单接口成本测量）
const READS = [
  [0.2, 'home recommend', () => '/post/home?tab=recommend&size=20'],
  [0.1, 'home hot', () => '/post/home?tab=hot&size=20'],
  [0.05, 'home latest', () => '/post/home?tab=latest&size=20'],
  [0.1, 'trending', () => `/trending/?window=${pick(['24h', '7d'])}&section=all`],
  [0.1, 'discover', () => '/discover/?section=all&size=20'],
  [0.15, 'search post', () => `/post/list?keyword=${q(pick(KEYWORDS))}&size=20`],
  [0.05, 'search user', () => `/user/search?keyword=${q(`lt_${RUN_ID}`)}&size=20`],
  [0.13, 'post detail', () => `/post/detail/${pick(postIds)}`],
  [0.08, 'comment list', () => `/comment/list?post_id=${pick(postIds)}&sort=${pick([0, 1])}`],
  [0.04, 'circle posts', () => `/circle/posts?circle_id=${CIRCLE_ID}&type=${pick([1, 2])}&size=20`],
];
const ONLY = __ENV.ENDPOINT ? READS.filter((r) => r[1] === __ENV.ENDPOINT) : null;
if (__ENV.ENDPOINT && ONLY.length === 0) throw new Error(`未知 ENDPOINT: ${__ENV.ENDPOINT}`);
// ANON=1：不带 token（匿名），用于对比登录态（sa-token 校验 + 续期）的额外开销；推荐流需登录，匿名时跳过
const ANON = __ENV.ANON === '1';

export function read() {
  const u = ANON ? null : vuUser();
  if (ONLY) {
    get(ONLY[0][2](), u, ONLY[0][1]);
    return;
  }
  let r = Math.random();
  for (const [w, name, path] of READS) {
    r -= w;
    if (r < 0 || name === 'circle posts') {
      if (ANON && name === 'home recommend') get('/post/home?tab=hot&size=20', u, 'home hot');
      else get(path(), u, name);
      return;
    }
  }
}

export function write() {
  const u = vuUser();
  const r = Math.random();
  if (r < 0.4) {
    post('/like/toggle', { type: 'post', target_id: pick(postIds), action: '' }, u, 'like post');
  } else if (r < 0.55) {
    post('/like/toggle', { type: 'comment', target_id: pick(commentIds), action: '' }, u, 'like comment');
  } else if (r < 0.75) {
    post('/collect/toggle', { post_id: pick(postIds), action: '' }, u, 'collect');
  } else if (r < 0.85) {
    post('/comment/create', { post_id: pick(postIds), content: `压测评论 ${pick(KEYWORDS)}` }, u, 'comment create');
  } else if (r < 0.95) {
    const root = pick(rootCommentIds);
    // 回复需与根评论同帖：先查根评论所属帖子会多一次读，这里用 locate 拿 post_id
    const loc = get(`/comment/locate?comment_id=${root}`, u, 'comment locate');
    const postId = loc.json('data.post_id');
    if (postId) post('/comment/create', { post_id: postId, root_id: root, reply_to_id: root, content: '压测回复' }, u, 'reply create');
  } else {
    post('/post/create', { circle_id: CIRCLE_ID, title: `[lt_${RUN_ID}] ${pick(KEYWORDS)}压测帖`, content: '压测内容', type: 1 }, u, 'post create');
  }
}

export function mixed() {
  if (Math.random() < 0.9) read();
  else write();
}

export function sse() {
  const u = vuUser();
  // SSE 不会自行结束：持有到 timeout 后断开重连，timeout 记为预期结果，不计入读写错误率
  const res = http.get(`${BASE_URL}/notice/stream`, {
    headers: { satoken: u.token, Accept: 'text/event-stream' },
    timeout: __ENV.HOLD || '60s',
    tags: { name: 'notice stream', kind: 'sse' },
  });
  // 1050 = 请求超时，即连接被持有到 HOLD；429 为超过每用户 5 连上限
  const held = check(res, { 'sse 持有到超时': (r) => r.error_code === 1050 });
  // 被拒后按 EventSource 的 retry（5s）退避，避免空转刷 429
  if (!held) sleep(5);
}

// 兜底：直接 k6 run 不带 exec 时走 read
export default read;
