// 灌压测内容：lt_* 账号在压测圈里发帖，每帖 3 条根评论 + 2 条回复。
// 帖子经 CDC 进 ES 后，搜索 / 推荐 / 圈内帖子等读接口才有数据（开发栈无 CDC，见 README）。
// 用法：k6 run -e POSTS=3000 -e VUS=20 seed-content.js
import { fail } from 'k6';
import { CIRCLE_ID, KEYWORDS, RUN_ID, pick, post, users } from './lib/common.js';

export const options = {
  scenarios: {
    seed: {
      executor: 'shared-iterations',
      vus: Number(__ENV.VUS || 20),
      iterations: Number(__ENV.POSTS || 3000),
      maxDuration: '1h',
    },
  },
};

function paragraph() {
  const words = [];
  for (let i = 0; i < 20; i++) words.push(pick(KEYWORDS));
  return words.join('，');
}

export default function () {
  const author = pick(users);
  const kw = pick(KEYWORDS);
  const res = post(
    '/post/create',
    {
      circle_id: CIRCLE_ID,
      title: `[lt_${RUN_ID}] ${kw}实践笔记 ${Math.floor(Math.random() * 1e6)}`,
      content: `${paragraph()}。${paragraph()}。`,
      summary: `${kw}相关的压测数据`,
      type: 1,
    },
    author,
    'seed post',
  );
  const postId = res.json('data');
  if (!postId) fail(`发帖失败: ${res.body}`);

  const roots = [];
  for (let i = 0; i < 3; i++) {
    const c = post('/comment/create', { post_id: postId, content: `${pick(KEYWORDS)}：${paragraph()}` }, pick(users), 'seed comment');
    const id = c.json('data.id') || c.json('data');
    if (id) roots.push(id);
  }
  for (let i = 0; i < 2 && roots.length > 0; i++) {
    const root = pick(roots);
    post('/comment/create', { post_id: postId, root_id: root, reply_to_id: root, content: `回复：${paragraph()}` }, pick(users), 'seed reply');
  }
}
