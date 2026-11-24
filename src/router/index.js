import { createRouter, createWebHistory } from 'vue-router'
import Login from '@/views/auth/Login.vue'
import { auth } from '@/utils/auth'
import { applyPageTitle } from '@/utils/pageTitle'

// meta.mobile：移动端门户配置（supported / topBar / tabBar），缺省取默认值，见 ./mobileMeta.js
export const routes = [
  {
    // 登录页：标签页标题展示品牌标语（见 utils/pageTitle.js）
    path: '/',
    name: 'login',
    component: Login
  },
  {
    path: '/success',
    name: 'success',
    component: () => import('@/views/auth/Success.vue'),
    meta: { titleKey: 'title.success' }
  },
  {
    // 首页信息流：访客可读（hot tab），recommend/following tab 需登录
    path: '/home',
    name: 'home',
    component: () => import('@/views/feed/Home.vue'),
    meta: { titleKey: 'title.home' }
  },
  {
    // 热点页：访客可读（/trending 接口已开放 anonymous）
    path: '/hot',
    name: 'hot',
    component: () => import('@/views/feed/Hot.vue'),
    meta: { titleKey: 'title.hot' }
  },
  {
    // 发现页：可选登录，允许匿名访问（新用户落地页）
    path: '/discover',
    name: 'discover',
    component: () => import('@/views/feed/Discover.vue'),
    meta: { titleKey: 'title.discover' }
  },
  {
    // 发帖页：需登录。pageFade：进出该页时启用页面级淡入淡出（见 App.vue）
    path: '/create-post',
    name: 'create-post',
    component: () => import('@/views/post/CreatePost.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.createPost', mobile: { supported: false } }
  },
  {
    path: '/profile',
    name: 'profile',
    component: () => import('@/views/user/UserProfile.vue'),
    meta: { requiresAuth: true, titleKey: 'title.profile' }
  },
  {
    // 用户公开主页：访客可读（/user/detail/:id 已开放 anonymous）
    path: '/user/:id',
    name: 'user-detail',
    component: () => import('@/views/user/UserDetail.vue'),
    meta: { titleKey: 'title.userDetail', mobile: { topBar: 'back' } }
  },
  {
    path: '/terms',
    name: 'terms',
    component: () => import('@/views/auth/Terms.vue'),
    meta: { titleKey: 'title.terms' }
  },
  {
    path: '/privacy',
    name: 'privacy',
    component: () => import('@/views/auth/Privacy.vue'),
    meta: { titleKey: 'title.privacy' }
  },
  {
    // 搜索页：访客可读（/post/list /circle/list /user/search 已开放 anonymous）
    path: '/search',
    name: 'search',
    component: () => import('@/views/feed/SearchResults.vue'),
    meta: { titleKey: 'title.searchResults', mobile: { topBar: 'back' } }
  },
  {
    // 圈子详情：访客可读（/circle/detail/:id 已开放 anonymous）
    path: '/circle/:id',
    name: 'circle-detail',
    component: () => import('@/views/circle/CircleDetail.vue'),
    meta: { titleKey: 'title.circleDetail', mobile: { topBar: 'back' } }
  },
  {
    // 圈子成员管理：需登录；圈主/管理员（member_role>=20）校验在页面内自查（非管理角色 /circle/members 返回 403）
    // pageFade：与编辑资料互切时启用页面级淡入过渡（见 App.vue）
    path: '/circle/:id/members',
    name: 'circle-members',
    component: () => import('@/views/circle/CircleMembers.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.circleMembers', mobile: { supported: false } }
  },
  {
    // 圈子资料编辑：需登录；圈主可改全部字段、管理员仅部分字段（页面内按 member_role 自查并按角色渲染）
    path: '/circle/:id/edit',
    name: 'circle-edit',
    component: () => import('@/views/circle/CircleEdit.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.circleEdit', mobile: { supported: false } }
  },
  {
    // 圈子机器人管理：需登录；圈主/管理员（member_role>=20）管理本圈机器人（每圈上限 5 个），
    // 删除与凭据字段（协议/地址/Key）修改仅圈主——校验以服务端为准（403 时页面内 toast）
    path: '/circle/:id/agents',
    name: 'circle-agents',
    component: () => import('@/views/circle/CircleAgents.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.circleAgents', mobile: { supported: false } }
  },
  {
    // 圈子内容审核：需登录；圈主/管理员（member_role>=20）处理本圈待审帖子（子 Tab，被举报队列后续加入），
    // 角色校验在页面内自查（非管理角色 /circle/moderation/* 返回 403）
    path: '/circle/:id/moderation',
    name: 'circle-moderation',
    component: () => import('@/views/circle/CircleModeration.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.circleModeration', mobile: { supported: false } }
  },
  {
    // 圈子邀请管理：需登录；圈主/管理员管理私密圈邀请链接（后端 v0.7.0 #169），角色校验在页面内自查。
    // 邀请链接要随手分享，移动端也支持（其他管理页仍是桌面专属）
    path: '/circle/:id/invites',
    name: 'circle-invites',
    component: () => import('@/views/circle/CircleInvites.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.circleInvites', mobile: { topBar: 'back' } }
  },
  {
    // 对接文档里的入口写法（/circle/:id/manage?tab=reports&focus=）：重定向到内容审核页，tab 别名由页面解析
    path: '/circle/:id/manage',
    redirect: to => ({ name: 'circle-moderation', params: to.params, query: to.query })
  },
  {
    // 我管理的圈子：选圈子进入内容审核（举报通知找不到帖子所属圈子时的兜底落点）；静态段优先于 /circle/:id
    path: '/circle/managed',
    name: 'managed-circles',
    component: () => import('@/views/circle/ManagedCircles.vue'),
    meta: { requiresAuth: true, pageFade: true, titleKey: 'title.managedCircles', mobile: { supported: false } }
  },
  {
    // 邀请落地页：访客可读（/circle/invite/preview 开放 anonymous），点「加入」时未登录先跳登录再回到本页
    path: '/invite/:token',
    name: 'invite-landing',
    component: () => import('@/views/circle/InviteLanding.vue'),
    meta: { titleKey: 'title.invite' }
  },
  {
    // 我的圈子邀请：需登录。被邀请人确认 / 拒绝按邮箱收到的私密圈邀请（后端不发邮件，这里是唯一入口）
    path: '/invites',
    name: 'my-invites',
    component: () => import('@/views/circle/MyInvites.vue'),
    meta: { requiresAuth: true, titleKey: 'title.myInvites', mobile: { topBar: 'back' } }
  },
  {
    // 关注列表（后端 v0.9.0 #218）：访客可读（GET /follow/following/:user_id 开放 anonymous）；
    // 对方设为私密时接口 403，页面内按 stats.list_visible / 403 展示。:id 为列表主人的用户 ID
    path: '/user/:id/following',
    name: 'user-following',
    component: () => import('@/views/follow/FollowList.vue'),
    meta: { titleKey: 'title.following', mobile: { topBar: 'back' } }
  },
  {
    // 粉丝列表：访客可读，规则同关注列表
    path: '/user/:id/followers',
    name: 'user-followers',
    component: () => import('@/views/follow/FollowList.vue'),
    meta: { titleKey: 'title.followers', mobile: { topBar: 'back' } }
  },
  {
    // 我的互关：需登录（GET /follow/mutual 只能看自己的）
    path: '/follow/mutual',
    name: 'follow-mutual',
    component: () => import('@/views/follow/MutualList.vue'),
    meta: { requiresAuth: true, titleKey: 'title.mutual', mobile: { topBar: 'back' } }
  },
  {
    // 私信会话列表（后端 v0.9.0 #238）：需登录
    path: '/messages',
    name: 'messages',
    component: () => import('@/views/message/ConversationList.vue'),
    meta: { requiresAuth: true, titleKey: 'title.messages' }
  },
  {
    // 私信聊天页：需登录。:userId 为对方用户 ID——用户主页和会话列表都能直接拿到，
    // 页面内调 GET /message/conversations/with/:userId 取会话（可能为 null = 还没聊过）与 can_send
    path: '/messages/:userId',
    name: 'message-chat',
    component: () => import('@/views/message/ChatView.vue'),
    meta: { requiresAuth: true, titleKey: 'title.chat', mobile: { topBar: 'back', tabBar: false } }
  },
  {
    // 帖子详情：访客可读（/post/detail/:id 已开放 anonymous）
    // pageFade：进出该页时启用页面级淡入淡出（见 App.vue）
    path: '/post/:id',
    name: 'post-detail',
    component: () => import('@/views/post/PostDetail.vue'),
    meta: { pageFade: true, titleKey: 'title.postDetail', mobile: { topBar: 'back', tabBar: false } }
  },
  {
    // 消息中心（站内通知）：需登录
    path: '/notifications',
    name: 'notifications',
    component: () => import('@/views/Notifications.vue'),
    meta: { requiresAuth: true, titleKey: 'title.notifications' }
  },
  {
    // 机器人管理：需登录。管理员（role=1）页面内自查后见「全局机器人管理 + 可管理圈子」双 tab；
    // 普通用户仅见可管理圈子列表（圈子选择器），全局 /agent/* 接口非管理员返回 403
    path: '/admin/agents',
    name: 'admin-agents',
    component: () => import('@/views/admin/AdminAgents.vue'),
    meta: { requiresAuth: true, titleKey: 'title.adminAgents', mobile: { supported: false } }
  },
  {
    // 兜底路由：未匹配路径统一展示 404 页（访客可读，避免空白页）
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/views/NotFound.vue'),
    meta: { titleKey: 'title.notFound' }
  }
]

// 全局路由守卫（具名导出，便于单测）
export function authGuard(to, _, next) {
  // 检查路由是否需要认证
  if (to.meta.requiresAuth) {
    if (auth.isAuthenticated()) {
      // 已登录，允许访问
      next()
    } else {
      // 未登录，重定向到登录页
      next('/')
    }
  } else {
    // 不需要认证的路由，直接放行
    next()
  }
}

const router = createRouter({
  history: createWebHistory('/'),
  routes
})

router.beforeEach(authGuard)

// 进入页面后同步浏览器标签页标题（含 i18n）
router.afterEach((to) => {
  applyPageTitle(to)
})

export default router
