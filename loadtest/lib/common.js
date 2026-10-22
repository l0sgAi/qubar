// 压测公共工具：环境、压测数据、带 token 的请求与信封校验。
import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';

export const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:8888';
const DATA_DIR = __ENV.DATA_DIR || '../data';

// seed-users.sh 产物：{ run_id, circle_id, users: [{id, token}] }
const seed = JSON.parse(open(`${DATA_DIR}/users.json`));
export const RUN_ID = seed.run_id;
export const CIRCLE_ID = seed.circle_id;
export const users = new SharedArray('users', () => seed.users);

// fixture.sh 产物：{ post_ids, root_comment_ids, comment_ids }；seed-content 阶段还不存在
function loadContent() {
  try {
    return JSON.parse(open(`${DATA_DIR}/content.json`));
  } catch (e) {
    return { post_ids: [], root_comment_ids: [], comment_ids: [] };
  }
}
const content = loadContent();
export const postIds = new SharedArray('posts', () => content.post_ids);
export const rootCommentIds = new SharedArray('roots', () => content.root_comment_ids);
export const commentIds = new SharedArray('comments', () => content.comment_ids);

// 搜索关键词：与 seed-content 生成的标题词表一致，保证 ES 有命中
export const KEYWORDS = ['性能', '缓存', '数据库', '分布式', '推荐', '搜索', '消息队列', '并发', '监控', '架构'];

export function pick(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

// 每个 VU 固定绑定一个账号，避免同一 token 被大量 VU 共享导致热点不真实
export function vuUser() {
  return users[(exec.vu.idInTest - 1) % users.length];
}

function params(user, name, kind) {
  const headers = { 'Content-Type': 'application/json' };
  if (user) headers.satoken = user.token;
  return { headers, tags: { name, kind } };
}

// name 作为 k6 的 name 标签，按接口聚合指标（避免带 ID 的 URL 各成一行）；kind 区分读写 SLO
export function get(path, user, name) {
  const res = http.get(`${BASE_URL}${path}`, params(user, name, 'read'));
  ok(res, name);
  return res;
}

export function post(path, body, user, name) {
  const res = http.post(`${BASE_URL}${path}`, JSON.stringify(body), params(user, name, 'write'));
  ok(res, name);
  return res;
}

// 统一信封 {code, message, data}：HTTP 200 且 code=200 才算成功
export function ok(res, name) {
  return check(res, {
    [`${name} 200`]: (r) => r.status === 200,
    [`${name} code=200`]: (r) => {
      try {
        return r.json('code') === 200;
      } catch (e) {
        return false;
      }
    },
  });
}

// 压测节奏：PROFILE=smoke|stress|soak|spike，RATE 为目标 RPS（stress 为峰值，soak 为恒定值）
export function scenarioFor(exec_, extra = {}) {
  const profile = __ENV.PROFILE || 'smoke';
  const rate = Number(__ENV.RATE || 50);
  const maxVUs = Number(__ENV.MAX_VUS || 500);
  const base = { exec: exec_, preAllocatedVUs: Math.min(50, maxVUs), maxVUs, timeUnit: '1s', ...extra };
  switch (profile) {
    case 'smoke':
      return { executor: 'constant-vus', exec: exec_, vus: 2, duration: __ENV.DURATION || '30s' };
    case 'stress': {
      // 阶梯加压：每档 RATE/STEPS，持续 STEP_DURATION，直到 RATE
      const steps = Number(__ENV.STEPS || 8);
      const stepDur = __ENV.STEP_DURATION || '1m';
      const stages = [];
      for (let i = 1; i <= steps; i++) {
        const target = Math.round((rate * i) / steps);
        stages.push({ target, duration: '10s' }, { target, duration: stepDur });
      }
      return { ...base, executor: 'ramping-arrival-rate', startRate: 1, stages };
    }
    case 'soak':
      return { ...base, executor: 'constant-arrival-rate', rate, duration: __ENV.DURATION || '1h' };
    case 'spike':
      return {
        ...base,
        executor: 'ramping-arrival-rate',
        startRate: Math.round(rate / 5),
        stages: [
          { target: Math.round(rate / 5), duration: '1m' },
          { target: rate, duration: '10s' },
          { target: rate, duration: '1m' },
          { target: Math.round(rate / 5), duration: '10s' },
          { target: Math.round(rate / 5), duration: '2m' },
        ],
      };
    default:
      throw new Error(`未知 PROFILE: ${profile}`);
  }
}

// SLO（#63）：读 p95 < 300ms，写 p95 < 500ms，错误率 < 1%；只做标记，不中止压测（找拐点需要压过线）
export const thresholds = {
  'http_req_duration{kind:read}': ['p(95)<300'],
  'http_req_duration{kind:write}': ['p(95)<500'],
  'http_req_failed{kind:read}': ['rate<0.01'],
  'http_req_failed{kind:write}': ['rate<0.01'],
};
