export default {
  // 通用
  common: {
    appName: '趣吧',
    search: '搜索',
    searchPlaceholder: '搜索兴趣圈、帖子...',
    searchPosts: '搜索帖子',
    searchCircles: '搜索圈子',
    searchUsers: '搜索用户',
    loading: '加载中...',
    loadMore: '加载更多',
    noMore: '没有更多了',
    noResults: '暂无结果',
    noData: '暂无数据',
    cancel: '取消',
    confirm: '确认',
    save: '保存',
    delete: '删除',
    edit: '编辑',
    share: '分享',
    back: '返回',
    submit: '提交',
    logout: '退出登录',
    logoutConfirm: '确定要退出登录吗？',
    logoutSuccess: '已退出登录',
    operationSuccess: '操作成功',
    operationFailed: '操作失败',
    unknownError: '未知错误',
    unknown: '未知',
    featureInDevelopment: '功能开发中...',
    collapse: '收起',
    showMore: '点击查看更多',
    yes: '是',
    enabled: '已开启'
  },

  // 移动端门户（≤ 768px，见 docs/mobile-adaptation-plan.md）
  mobile: {
    tab: {
      home: '首页',
      discover: '发现',
      notice: '消息',
      me: '我的',
      login: '登录'
    },
    segment: {
      discover: '发现',
      hot: '热榜'
    },
    linkCopied: '链接已复制',
    leaveConfirm: '确定退出「{name}」吗？',
    copyFailed: '复制失败，请手动复制地址栏中的链接',
    unsupported: {
      title: '该功能暂未支持手机网页版',
      desc: '发帖、编辑和管理类功能请在电脑上使用，App 版本正在路上。',
      copyLink: '复制链接，在电脑上打开',
      desktopHint: '也可以在浏览器菜单中选择「请求桌面网站」，临时切换到电脑版布局。',
      downloadApp: '下载 App',
      backHome: '回到首页'
    },
    comment: {
      placeholder: '写下你的评论...',
      replyPlaceholder: '回复 {name}',
      // 底部输入条 / 输入抽屉（见 MobileComposeBar、MobileCommentComposer）
      barPlaceholder: '说点什么…',
      replyTitle: "回复 {'@'}{name}",
      toComments: '查看评论',
      send: '发送',
      richHint: "图片和 {'@'} 提及请在电脑端使用"
    },
    me: {
      settings: '设置',
      language: '语言',
      terms: '用户协议',
      privacy: '隐私政策'
    }
  },

  // 浏览器标签页标题（document.title，见 router/index.js 的 applyPageTitle）
  title: {
    brand: '趣吧 Qubar',
    slogan: '发现你的热爱',
    success: '登录成功',
    home: '主页',
    hot: '热点',
    discover: '发现',
    createPost: '创建帖子',
    profile: '个人中心',
    userDetail: '用户主页',
    terms: '用户协议',
    privacy: '隐私政策',
    searchResults: '搜索结果',
    searchKeyword: '搜索「{keyword}」',
    circleDetail: '圈子详情',
    circleMembers: '成员管理',
    circleEdit: '编辑圈子资料',
    postDetail: '帖子详情',
    notifications: '消息中心',
    adminAgents: '机器人管理',
    circleAgents: '圈子机器人管理',
    circleModeration: '内容审核',
    managedCircles: '我管理的圈子',
    circleInvites: '邀请成员',
    invite: '圈子邀请',
    myInvites: '圈子邀请',
    following: '关注列表',
    followers: '粉丝列表',
    mutual: '互相关注',
    messages: '私信',
    chat: '聊天',
    // 动态标题模板：详情页数据加载成功后展示具体内容（见 utils/pageTitle.js）
    postDetailName: '{name}',
    circleDetailName: '{name}',
    userDetailName: '{name}',
    circleMembersName: '成员管理：{name}',
    circleEditName: '编辑圈子资料：{name}',
    circleAgentsName: '机器人管理：{name}',
    circleModerationName: '内容审核：{name}',
    circleInvitesName: '邀请成员：{name}',
    inviteName: '邀请加入：{name}',
    profileName: '个人中心：{name}',
    notFound: '页面不存在'
  },

  // 404 页面
  notFound: {
    title: '页面走丢了',
    description: '你访问的页面不存在或已被移除，请检查地址是否正确。',
    backHome: '返回主页'
  },

  // 时间相关
  time: {
    justNow: '刚刚',
    minutesAgo: '{minutes} 分钟前',
    hoursAgo: '{hours} 小时前',
    daysAgo: '{days} 天前',
    monthsAgo: '{months} 个月前',
    yearsAgo: '{years} 年前',
    createdAt: '创建时间',
    updatedAt: '更新时间'
  },

  // 帖子相关
  post: {
    post: '帖子',
    createPost: '创建帖子',
    createPostSuccess: '创建成功',
    createPostFailed: '创建失败',
    title: '标题',
    titlePlaceholder: '请输入帖子标题',
    content: '正文',
    contentPlaceholder: '请输入帖子正文内容（支持 Markdown 格式）',
    summary: '摘要',
    summaryPlaceholder: '请输入摘要，用于检索和关键词匹配',
    belongToCircle: '所属圈子',
    selectCircle: '搜索并选择圈子',
    circleRules: '圈子规则',
    noCircleRules: '该圈子暂无规则',
    selectCircleToViewRules: '选择圈子后查看规则',
    detail: '帖子详情',
    status: {
      normal: '正常',
      draft: '草稿',
      reviewing: '审核中',
      rejected: '未通过审核',
      blocked: '已屏蔽，等待复核',
      // 状态标签的说明（悬停 / 点按显示），驳回 / 屏蔽同时用作详情页作者提示条
      hint: {
        rejected: '仅你自己可见。帖子未通过 AI 审核或被圈主 / 管理员驳回 / 下架，结果已通过消息中心通知你',
        blocked: '帖子被多次举报，已暂时对他人隐藏，等待圈主 / 管理员复核'
      }
    },
    badges: {
      pinned: '置顶',
      essence: '精华',
      locked: '锁定'
    },
    stats: {
      views: '浏览',
      comments: '评论',
      likes: '点赞',
      favorites: '收藏'
    },
    actions: {
      like: '点赞',
      liked: '已赞',
      favorite: '收藏',
      favorited: '已收藏',
      share: '分享',
      comment: '评论'
    },
    recent: {
      title: '近期发布',
      subtitle: '最新发布的帖子',
      loadMore: '加载更多',
      empty: '暂无近期帖子'
    },
    noPosts: '暂无帖子',
    noPostResults: '暂无帖子结果',
    noFavorites: '暂无收藏',
    searching: '搜索中...',
    hottest:'近期热点',
    latest:'最新',
    highlights:'精华',
    postNotFound: '帖子不存在',
    viewDetail: '查看详情',
    loadCirclesFailed: '加载圈子列表失败',
    publishSuccess: '发布成功',
    publishNoId: '未获取到帖子ID，返回首页',
    publishFailed: '发布失败，请重试',
    submittedForReview: '已提交，正在审核，通过后其他人即可看到',
    // 帖子不存在，或非作者打开未发布的帖子（不透露是否在审核）
    unavailable: '帖子不存在或暂时无法查看',
    // 作者本人看到的审核中提示条
    reviewing: {
      banner: '帖子正在审核中，仅你自己可见',
      refresh: '刷新',
      published: '审核通过，帖子已发布',
      slow: '审核时间较长，结果会通过消息中心通知你'
    },
    // 举报帖子（POST /post/report）；理由文案见顶层 report.reasons
    report: {
      action: '举报',
      reported: '已举报',
      more: '更多操作',
      title: '举报帖子',
      reasonLabel: '举报理由',
      detailPlaceholder: '补充说明（选填）',
      anonymousNote: '举报人信息不会透露给作者',
      // 选「自伤轻生」时的关怀提示（文案待产品确认）
      selfHarmHint: '如果你或身边的人正处在困境中，请尽快联系信任的人或当地心理援助热线，你并不孤单。',
      submit: '提交举报',
      success: '举报已提交，感谢反馈',
      already: '你已经举报过这个帖子',
      tooOften: '举报太频繁，请稍后再试',
      unavailable: '帖子不存在或暂不可举报',
      invalid: '不能举报自己的帖子，或参数有误',
      failed: '举报失败，请稍后重试'
    },
    validation: {
      circleRequired: '请选择所属圈子',
      titleRequired: '请输入帖子标题',
      contentRequired: '请输入帖子正文'
    }
  },

  // 圈子相关
  circle: {
    circle: '兴趣圈',
    interestCircle: '兴趣圈',
    circles: '兴趣圈',
    myCircles: '我的圈子',
    joinCircle: '加入圈子',
    leaveCircle: '退出圈子',
    joined: '已加入',
    active: '近期活跃',
    random: '随机推荐',
    viewAll: '查看全部',
    myCircles: '我加入的',
    createCircle: '创建圈子',
    createCircleSuccess: '创建成功',
    joinSuccess: '加入成功',
    // POST /circle/join 失败（按码给文案，不展示后端 message）
    joinError: {
      alreadyMember: '你已经是该圈子成员',
      inviteOnly: '私密圈需邀请才能加入',
      banned: '你已被该圈子拉黑，无法加入',
      notFound: '圈子不存在或已被删除',
      generic: '加入失败，请稍后重试'
    },
    // 私密圈非成员（后端 #170）
    private: {
      inviteOnly: '私密圈，需邀请加入',
      membersOnly: '私密圈，仅成员可见',
      membersOnlyDesc: '圈内帖子和资料只对成员开放，受邀加入后即可查看',
      restrictedNote: '简介、圈规等资料仅成员可见',
      inviteHint: '仅限邀请加入，可向圈主或管理员索取邀请链接'
    },
    // 私密圈邀请（后端 v0.7.0，#169）
    invite: {
      title: '邀请成员',
      entry: '邀请成员',
      publicCircle: '公开圈子无需邀请，用户可以直接加入',
      link: {
        title: '邀请链接',
        desc: '生成链接后通过任意聊天工具分享，对方登录并确认后即可加入本圈',
        create: '生成链接',
        createTitle: '生成邀请链接',
        expire: '有效期',
        expireDays: '{n} 天',
        hoursUnit: '小时',
        expireHint: '1 小时 ~ 30 天，默认 7 天',
        maxUses: '使用次数上限',
        maxUsesPlaceholder: '不限',
        maxUsesHint: '留空为不限次数，范围 1 ~ 10000',
        createdTitle: '邀请链接已生成',
        onlyOnce: '链接只显示这一次，关闭后无法再查看，请立即复制保存',
        copy: '复制链接',
        copied: '已复制邀请链接',
        copyFailed: '复制失败，请手动选中链接复制',
        done: '我已保存',
        usesLimit: '最多 {n} 次',
        usesUnlimited: '不限次数',
        empty: '还没有邀请链接',
        expireAt: '有效期至 {time}',
        used: '已用 {used} / {max} 次',
        usedUnlimited: '已用 {used} 次 · 不限',
        createdAt: '创建于 {time}',
        unknownCreator: '未知用户',
        status: {
          active: '有效',
          expired: '已过期',
          revoked: '已撤销',
          usedUp: '已用完'
        },
        revoke: '撤销',
        revokeTitle: '撤销邀请链接',
        revokeContent: '撤销后该链接立即失效，已通过它加入的成员不受影响。确定撤销吗？',
        revoked: '已撤销邀请链接',
        loadFailed: '邀请链接加载失败',
        error: {
          notPrivate: '只有私密圈可以生成邀请链接',
          badRequest: '有效期或次数上限超出范围',
          notFound: '该邀请链接不存在，列表已刷新',
          generic: '操作失败，请稍后重试'
        }
      },
      landing: {
        title: '你收到一个圈子邀请',
        memberCount: '{n} 位成员',
        privateTag: '私密圈',
        desc: '加入后可以查看圈内帖子并参与讨论',
        join: '加入圈子',
        loginToJoin: '登录后加入',
        loginHint: '登录后会回到这个页面继续加入',
        joined: '已加入「{name}」',
        alreadyMember: '你已经是该圈子成员',
        invalidTitle: '邀请已失效',
        invalidDesc: '请联系邀请人重新获取邀请链接',
        loadFailed: '邀请信息加载失败，请检查网络后重试',
        retry: '重试',
        goDiscover: '去发现更多圈子',
        error: {
          banned: '你已被该圈子拉黑，无法加入',
          invalid: '邀请已失效',
          generic: '加入失败，请稍后重试'
        }
      },
      tabs: {
        link: '邀请链接',
        email: '邮箱邀请'
      },
      email: {
        title: '邮箱邀请',
        desc: '按注册邮箱邀请指定用户。系统不会发送邮件，对方登录后在「圈子邀请」里确认加入；还没注册的邮箱注册后也能看到',
        placeholder: '每行一个邮箱，也可以用逗号分隔，一次最多 50 个',
        count: '{n} / {max}',
        add: '添加邀请',
        emptyInput: '请输入要邀请的邮箱',
        tooMany: '一次最多添加 {max} 个邮箱，当前 {n} 个',
        addedCount: '已添加 {n} 个邀请',
        noneAdded: '没有新增邀请',
        ignoredTitle: '以下邮箱未添加',
        ignored: {
          invalid: '格式不正确',
          duplicate: '本次重复填写',
          already_invited: '已有待接受或已接受的邀请'
        },
        filter: {
          all: '全部',
          pending: '待接受',
          accepted: '已接受',
          rejected: '已拒绝',
          revoked: '已撤销'
        },
        invitedBy: '{name} 邀请',
        createdAt: '邀请于 {time}',
        updatedAt: '更新于 {time}',
        unknownInviter: '未知用户',
        empty: '还没有邮箱邀请',
        emptyFiltered: '没有该状态的邀请',
        loadFailed: '邮箱邀请加载失败',
        revoke: '撤销',
        revokeTitle: '撤销邮箱邀请',
        revokeContent: '撤销后 {email} 将看不到这条邀请。确定撤销吗？',
        revoked: '已撤销邀请',
        error: {
          handled: '该邀请已处理',
          notPrivate: '只有私密圈可以添加邮箱邀请',
          badRequest: '邮箱数量需要在 1 ~ 50 个之间',
          notFound: '该邀请不存在，列表已刷新',
          generic: '操作失败，请稍后重试'
        }
      },
      // 我的邀请：后端不发邮件、没有站内通知，这里是被邀请人唯一的入口
      mine: {
        title: '圈子邀请',
        navEntry: '圈子邀请',
        desc: '圈主或管理员按你的注册邮箱发出的邀请会出现在这里',
        entryPending: '你有 {n} 个待接受的圈子邀请',
        entryNone: '暂无待接受的圈子邀请',
        entryAction: '去查看',
        invitedBy: '{name} 邀请你加入',
        unknownInviter: '圈子管理员',
        accept: '接受',
        reject: '拒绝',
        rejectTitle: '拒绝邀请',
        rejectContent: '拒绝后这条邀请不再显示，确定不加入「{name}」吗？',
        accepted: '已加入「{name}」',
        alreadyMember: '你已经是该圈子成员',
        rejected: '已拒绝邀请',
        empty: '暂无待接受的圈子邀请',
        loadFailed: '邀请加载失败，请稍后重试',
        retry: '重试',
        error: {
          invalid: '邀请已失效',
          banned: '你已被该圈子拉黑，无法加入',
          generic: '操作失败，请稍后重试'
        }
      }
    },
    leaveSuccess: '已退出圈子',
    leaveFailed: '退出失败',
    disturb: '免打扰',
    disturbOn: '免打扰已开启',
    disturbOff: '免打扰',
    disturbEnabled: '已开启免打扰',
    disturbDisabled: '已关闭免打扰',
    createPost: '发帖',
    members: '成员',
    posts: '帖子',
    membersShort: '成员',
    postsShort: '帖子',
    description: '简介',
    noCircles: '暂无圈子',
    noCircleResults: '暂无圈子结果',
    resultsMayBeIncomplete: '结果可能不全，请细化关键字',
    moreOptions: '更多选项',
    report: '举报',
    joinType: '加入方式',
    joinTypeDirect: '直接加入',
    joinTypePrivate: '私密圈子',
    memberInfo: '成员信息',
    roleMember: '普通成员',
    roleAdmin: '管理员',
    roleOwner: '圈主',
    statusMuted: '已禁言',
    rules: '圈子规则',
    aboutCircle: '关于圈子',
    hotness: '热度',
    noDescription: '暂无描述',
    yourMemberInfo: '你的成员信息',
    pinnedDisplay: '置顶显示',
    messageDisturb: '消息免打扰',
    shareCircle: '分享圈子',
    circleIdNotFound: '圈子ID不存在',
    statusNormal: '正常',
    statusBanned: '已拉黑',
    statusExited: '已退出',
    // 个人中心 / 他人主页「加入的圈子」列表
    joinedList: {
      roleAll: '全部',
      roleOwner: '圈主',
      roleAdmin: '管理',
      roleMember: '成员',
      muted: '禁言中',
      mutedUntil: '禁言中 · 至 {time}',
      loadFailed: '圈子列表加载失败',
      retry: '重试',
      empty: {
        mine: {
          all: '还没有加入圈子',
          owner: '你还不是任何圈子的圈主',
          admin: '你还不是任何圈子的管理员',
          member: '你还没有以普通成员身份加入的圈子'
        },
        others: {
          all: 'TA 还没有加入圈子',
          owner: 'TA 还不是任何圈子的圈主',
          admin: 'TA 还不是任何圈子的管理员',
          member: 'TA 还没有以普通成员身份加入的圈子'
        }
      }
    },
    manage: {
      title: '成员管理',
      entry: '管理',
      backToCircle: '返回圈子',
      viewCircle: '查看圈子',
      tabs: {
        normal: '正常成员',
        muted: '禁言中',
        banned: '已拉黑'
      },
      badgeFull: '20+',
      empty: '暂无成员',
      loadFailed: '成员列表加载失败',
      searchPlaceholder: '搜索用户名 / 邮箱',
      noSearchResult: '未找到匹配的成员',
      searchUnavailable: '搜索服务暂不可用，请稍后重试',
      joinAt: '加入于 {time}',
      muteRemaining: '禁言剩余 {time}',
      remainingMinutes: '{n} 分钟',
      remainingHours: '{n} 小时',
      remainingDays: '{n} 天',
      unknownUser: '未知用户',
      noPermissionVisit: '无权限访问成员管理',
      actions: {
        mute: '禁言',
        unmute: '解除禁言',
        remove: '移出',
        ban: '拉黑',
        unban: '解除拉黑',
        setAdmin: '设为管理员',
        removeAdmin: '免去管理员',
        transfer: '转让圈主'
      },
      muteTitle: '禁言成员',
      muteTarget: '将禁言 {name} 的发言',
      muteDuration: '禁言时长',
      hoursUnit: '小时',
      mutePreset1h: '1 小时',
      mutePreset24h: '24 小时',
      mutePreset7d: '7 天',
      mutePreset30d: '30 天',
      muteRangeError: '禁言时长需在 1-720 小时之间',
      confirm: {
        removeTitle: '移出成员',
        removeContent: '确定将 {name} 移出本圈吗？对方不会被拉黑：公开圈可以重新加入，私密圈需要重新邀请。',
        banTitle: '拉黑成员',
        banContent: '确定将 {name} 移出并拉黑吗？解除拉黑之前，对方无法再加入本圈。',
        unbanTitle: '解除拉黑',
        unbanContent: '确定解除对 {name} 的拉黑吗？解除后对方不会自动回圈：公开圈可以重新加入，私密圈需要重新邀请。',
        removeAdminTitle: '免去管理员',
        removeAdminContent: '确定免去 {name} 的管理员身份吗？',
        transferTitle: '转让圈主',
        transferContent: '确定将圈主转让给 {name} 吗？此操作不可逆，你将立即变为普通成员。'
      },
      success: {
        muted: '已禁言',
        unmuted: '已解除禁言',
        removed: '已移出',
        banned: '已拉黑',
        unbanned: '已解除拉黑',
        adminSet: '已设为管理员',
        adminRemoved: '已免去管理员',
        transferred: '已转让圈主'
      },
      error: {
        conflict: '该成员状态已变化，列表已刷新，请重试',
        noPermission: '无权限执行该操作',
        roleDemoted: '你的管理身份已变更',
        memberNotFound: '该成员不存在或已不在本圈',
        badRequest: '请求参数有误',
        generic: '操作失败，请稍后重试'
      }
    },
    // 我管理的圈子（举报通知兜底落点，选圈子进入内容审核）
    managed: {
      title: '我管理的圈子',
      subtitle: '选择圈子，进入内容审核处理被举报和待审的帖子',
      statusPending: '圈子审核中，暂不可管理',
      statusBanned: '圈子已被封禁，暂不可管理'
    },
    moderation: {
      title: '内容审核',
      noPermissionVisit: '无权限访问内容审核',
      tabs: {
        pending: '待审帖子',
        reported: '被举报'
      },
      empty: '暂无待审帖子',
      loadFailed: '待审帖子加载失败',
      unknownUser: '未知用户',
      untitled: '（无标题）',
      postedAt: '发布于 {time}',
      showContent: '展开全文',
      hideContent: '收起全文',
      ai: {
        title: 'AI 审核结论',
        pending: '暂无 AI 结论，可直接人工处理',
        failed: 'AI 未能给出结论',
        noHit: '未命中风险',
        hazardTag: '{label} {percent}%',
        severity: '危害程度：{level}（{score}）',
        confidence: '置信度 {percent}%',
        model: '模型：{model}',
        showHazards: '查看全部风险概率',
        hideHazards: '收起风险概率',
        care: '可能需要人工关怀'
      },
      hazards: {
        sexual: '色情低俗',
        violence_terror: '暴力恐怖',
        harassment: '辱骂骚扰',
        illegal: '违法违规',
        spam_ad: '广告引流',
        privacy: '泄露隐私',
        self_harm: '自伤轻生',
        manipulation: '干扰审核'
      },
      severity: {
        none: '无',
        minor: '轻微',
        severe: '严重',
        extreme: '极端'
      },
      decision: {
        publish: 'AI 放行',
        review_publish: 'AI 判断存疑，已自动发布',
        fail_open: 'AI 不可用，已直接发布'
      },
      actions: {
        approve: '通过',
        reject: '驳回'
      },
      confirm: {
        approveTitle: '通过帖子',
        approveContent: '确定通过「{title}」吗？通过后帖子将在圈子中公开，作者会收到通知。',
        rejectTitle: '驳回帖子',
        rejectContent: '确定驳回「{title}」吗？驳回后帖子仅作者可见，作者会收到通知。',
        reasonLabel: '备注（可选）',
        reasonPlaceholder: '填写处理理由',
        reasonHint: '备注仅记入审核日志，不会展示给作者'
      },
      success: {
        approved: '已通过，帖子将在几秒内出现在圈子中',
        rejected: '已驳回'
      },
      error: {
        handled: '该帖子已被处理',
        noPermission: '无权限执行该操作',
        badRequest: '请求参数有误',
        generic: '操作失败，请稍后重试'
      },
      reported: {
        empty: '暂无被举报的帖子',
        loadFailed: '被举报帖子加载失败',
        reportCount: '被举报 {n} 次',
        reportCountOne: '被举报 1 次',
        firstReportAt: '首次举报于 {time}',
        autoHidden: '已自动屏蔽',
        autoHiddenHint: '仅作者可见，保留后恢复公开',
        reasonsTitle: '举报理由',
        onlyRecent: '仅显示最近 {n} 条',
        reasonCount: '{label} ×{n}',
        showReports: '查看举报详情',
        hideReports: '收起举报详情',
        // 通知跳转 ?focus= 定位不到（已被其他管理员处理）
        focusHandled: '该举报已处理',
        // 复核 404：没有待处理举报（含已被其他管理员处理）
        handled: '该帖子已被其他管理员处理',
        actions: {
          keep: '保留',
          remove: '下架'
        },
        confirm: {
          keepTitle: '保留帖子',
          keepContent: '确定保留「{title}」吗？将关闭本轮全部举报，帖子继续公开。',
          removeTitle: '下架帖子',
          removeContent: '确定下架「{title}」吗？下架后仅作者可见，作者会收到通知。'
        },
        success: {
          kept: '已保留，关闭 {n} 条举报',
          keptOne: '已保留，关闭 1 条举报',
          removed: '已下架'
        }
      }
    },
    edit: {
      title: '编辑资料',
      ownerSection: '圈主专属',
      ownerSectionDesc: '仅圈主可修改以下字段',
      adminSection: '基础资料',
      adminSectionDesc: '圈主与管理员均可修改',
      changedCount: '已修改 {n} 项，保存时仅提交变更字段',
      noChanges: '暂无修改',
      removeImage: '移除图片',
      slugClearHint: '清空并保存将清除该标识',
      ruleClearHint: '清空并保存将清除圈子规则',
      saveSuccess: '圈子资料已保存',
      cacheHint: '提示：保存成功后，圈子详情页与搜索结果可能有秒级延迟',
      saveFailed: '保存失败，请稍后重试',
      nameConflict: '名称或唯一标识与其他圈子重名',
      noPermission: '无权限执行该操作，你的角色可能已变更',
      validationFailed: '字段校验未通过，请检查修改内容',
      validation: {
        nameLength: '名称长度需在 1-50 个字符之间',
        descriptionMax: '简介不能超过 2000 个字符',
        ruleMax: '规则不能超过 2000 个字符'
      }
    },
    form: {
      name: '圈子名称',
      namePlaceholder: '请输入兴趣圈名称',
      slug: '唯一标识',
      slugPlaceholder: '留空自动生成，或手动输入（仅限小写字母、数字、连字符）',
      description: '简介描述',
      descriptionPlaceholder: '简单介绍一下这个兴趣圈...',
      avatar: '圈子头像',
      avatarUpload: '点击上传',
      avatarTip: '建议尺寸 200x200px',
      cover: '背景封面',
      coverUpload: '点击上传',
      coverTip: '建议尺寸 1200x300px',
      rules: '圈内规则',
      rulesPlaceholder: '请输入圈子内的行为规范...',
      category: '圈子分类',
      categoryPlaceholder: '请选择分类',
      joinType: '加入方式',
      joinTypeDirect: '直接加入',
      joinTypeDirectDesc: '任何人都可以直接加入',
      joinTypePrivate: '私密圈子',
      joinTypePrivateDesc: '仅限邀请加入',
      createButton: '创建兴趣圈',
      step1Title: '基本信息',
      step2Title: '图片与规则',
      step3Title: '分类与权限',
      step4Title: '确认预览',
      nextStep: '下一步',
      prevStep: '上一步',
      cropAvatarTitle: '裁剪头像',
      cropCoverTitle: '裁剪背景图',
      cropConfirm: '确认裁剪',
      cropReset: '重置',
      previewButton: '效果预览',
      previewTitle: '效果预览',
      previewHint: '拖动裁剪框，实时预览圈子头部的展示效果',
      previewNamePlaceholder: '圈子名称（示例）',
      validation: {
        nameRequired: '请输入圈子名称',
        nameLength: '名称长度应在 2-50 个字符之间',
        slugPattern: '只能包含小写字母、数字和连字符',
        slugMaxLength: '最多 60 个字符',
        descriptionRequired: '请输入简介描述',
        descriptionLength: '描述长度应在 10-2000 个字符之间',
        rulesRequired: '请输入圈内规则',
        rulesLength: '规则长度应在 10-2000 个字符之间',
        categoryRequired: '请选择圈子分类',
        pleaseComplete: '请完善带 * 的必填项后再提交'
      },
      messages: {
        avatarUploadSuccess: '头像上传成功',
        avatarUploadFailed: '头像上传失败',
        coverUploadSuccess: '封面上传成功',
        coverUploadFailed: '封面上传失败',
        uploadFailedRetry: '上传失败，请重试',
        createSuccess: '兴趣圈创建成功！',
        createFailed: '创建失败，请重试',
        loadCategoriesFailed: '加载分类列表失败',
        createNetworkError: '创建失败，请检查网络连接',
        cropFailed: '裁剪失败，请重试'
      }
    }
  },

  // 用户相关
  user: {
    user: '用户',
    username: '用户名',
    avatar: '头像',
    profile: '个人资料',
    editProfile: '编辑资料',
    myPosts: '我的帖子',
    posts: '帖子',
    myFavorites: '我的收藏',
    browseHistory: '浏览历史',
    myGroups: '兴趣圈',
    userInfo: '用户信息',
    basicInfo: '基本信息',
    email: '邮箱',
    phone: '手机号',
    gender: '性别',
    birthday: '生日',
    role: '角色',
    status: '状态',
    notSet: '未设置',
    notBound: '未绑定',
    normal: '正常',
    disabled: '禁用',
    userSearchInDevelopment: '用户搜索功能开发中...',
    anonymous: '匿名用户',
    unknownUser: '未知用户',
    idNotFound: '用户ID不存在',
    fetchFailed: '获取用户信息失败',
    admin: '管理员',
    noUserResults: '暂无用户结果',
    today: '今天',
    yesterday: '昨天',
    daysAgo: '{days} 天前',
    monthsAgo: '{months} 个月前',
    yearsAgo: '{years} 年前',
    genders: {
      notSet: '未设置',
      male: '男',
      female: '女',
      other: '其他'
    },
    roles: {
      user: '普通用户',
      admin: '管理员',
      superAdmin: '超级管理员',
      agentBot: '机器人'
    },
    editModal: {
      title: '编辑个人信息',
      usernamePlaceholder: '请输入用户名（1-50字符）',
      phonePlaceholder: '请输入手机号',
      genderPlaceholder: '请选择性别',
      birthdayPlaceholder: '请选择生日',
      clickUploadAvatar: '点击上传头像',
      cropAvatarTitle: '裁剪头像',
      previewTitle: '头像效果预览',
      previewHint: '拖动裁剪框，实时预览头像在不同位置的展示效果',
      avatarTip: '头像支持 JPG、JPEG、PNG、GIF、WebP、SVG 格式,文件大小 ≤ 10MB',
      saveChanges: '保存修改',
      usernameLengthError: '用户名长度为1-50个字符',
      phoneFormatError: '请输入正确的手机号',
      avatarTypeError: '只支持 JPG、JPEG、PNG、GIF、WebP、SVG 格式的图片',
      avatarSizeError: '图片大小不能超过10MB',
      avatarUploadSuccess: '头像上传成功',
      avatarUploadFailed: '头像上传失败',
      noChangesWarning: '请至少修改一项信息',
      updateSuccess: '个人信息更新成功',
      updateFailed: '更新个人信息失败',
      fetchFailed: '获取用户信息失败',
      clearHistory: '清空浏览历史'
    },
    passwordModal: {
      button: '修改密码',
      title: '修改密码',
      newPassword: '新密码',
      confirmPassword: '确认密码',
      newPasswordPlaceholder: '请输入新密码（至少 6 位）',
      confirmPasswordPlaceholder: '请再次输入新密码',
      lengthError: '密码至少 6 位',
      mismatchError: '两次输入的密码不一致',
      updateSuccess: '密码修改成功',
      updateFailed: '密码修改失败',
      sessionTip: '修改成功后当前登录保持有效，无需重新登录'
    }
  },

  // 导航和菜单
  nav: {
    home: '主页',
    recommend: '推荐',
    following: '关注',
    hot: '热门',
    latest: '最新',
    discover: '发现',
    create: '创建',
    messages: '消息',
    notifications: '通知',
    settings: '设置',
    profile: '个人中心',
    searchResults: '搜索结果',
    keyword: '关键词'
  },

  // AI 回复机器人管理（/admin/agents；管理员见全局+圈子双 tab，普通用户仅见可管理圈子列表）
  agent: {
    title: '机器人管理',
    navEntry: '机器人管理',
    noPermission: '无权访问：仅管理员可管理机器人',
    tabs: {
      global: '全局机器人管理',
      circles: '可管理圈子'
    },
    circleList: {
      searchPlaceholder: '搜索圈子名称或简介',
      roleOwner: '圈主',
      roleAdmin: '管理员',
      statusPending: '圈子审核中，暂不可管理代理',
      statusBanned: '圈子已被封禁，暂不可管理代理',
      agentsBound: '已绑定 {count}/5',
      members: '{count} 成员',
      posts: '{count} 帖子',
      noDescription: '这个圈子还没有简介',
      empty: '你暂无可管理的圈子',
      emptySearch: '没有匹配的圈子'
    },
    circlePage: {
      titleWithName: '{name} · 机器人管理',
      backToList: '返回列表',
      create: '新建机器人',
      edit: '编辑机器人',
      quota: '已绑定 {count}/5',
      quotaFull: '每圈最多绑定 5 个机器人，删除后可释放名额',
      noPermission: '无权访问：仅圈主/管理员可管理圈内机器人',
      empty: '本圈还没有机器人，点击「新建机器人」创建第一个',
      emptySearch: '没有匹配的机器人',
      loadFailed: '加载圈内机器人失败',
      replyNotLive: '圈内机器人暂不会回复圈内帖子（关键词/手动触发均未生效），当前仅保存配置；回复能力将在后续版本开放。',
      credentialsOwnerOnly: 'API 协议、自定义地址与 API Key 仅圈主可修改；如需调整请联系圈主'
    },
    searchPlaceholder: '搜索机器人名称',
    create: '新建机器人',
    edit: '编辑机器人',
    unlimited: '不限',
    deleteSuccess: '已删除',
    deleteConfirm: '确定删除「{name}」吗？删除后不可恢复（软删，无恢复接口）',
    statusUpdateFailed: '状态更新失败',
    triggerModes: {
      all: '全部新帖',
      keyword: '关键词触发',
      manual: '手动'
    },
    table: {
      name: '机器人',
      protocol: '协议',
      trigger: '触发模式',
      rateLimit: '限频',
      status: '状态',
      createTime: '创建时间',
      actions: '操作'
    },
    form: {
      sectionBasic: '基本信息',
      sectionApi: '模型接入',
      sectionTrigger: '触发与限频',
      name: '名称',
      namePlaceholder: '1-50 字符，全局唯一',
      namePlaceholderCircle: '1-50 字符，圈内唯一',
      avatarUrl: '头像',
      avatarUpload: '上传头像',
      avatarRemove: '移除',
      cropAvatarTitle: '裁剪机器人头像',
      avatarTypeError: '仅支持 JPEG / PNG / GIF / WebP / SVG 格式',
      avatarSizeError: '图片大小不能超过 10MB',
      avatarUploadFailed: '头像上传失败，请重试',
      protocol: 'API 协议',
      baseUrl: '自定义 API 地址',
      baseUrlPlaceholder: '使用官方默认端点可留空',
      apiKey: 'API Key',
      apiKeyPlaceholder: 'sk-…',
      apiKeyCurrent: '已配置：{mask}',
      apiKeyNone: '未配置 Key',
      apiKeyNewPlaceholder: '输入新 Key 更换；不修改请留空',
      apiKeyClear: '清除已保存的 Key',
      model: '模型名',
      modelPlaceholder: '如 gpt-4o-mini / claude-sonnet-5',
      llmParams: 'LLM 参数（提交时整体替换）',
      systemPrompt: '系统提示词',
      systemPromptPlaceholder: '机器人的人设与回复要求（可空）',
      keywords: '触发关键词',      keywordsPlaceholder: '输入关键词后回车添加',
      keywordsRequired: '至少需要一个触发关键词',
      filterPrompt: '回复判定条件（可选）',
      filterPromptPlaceholder: '如：只回复编程相关问题。留空则命中关键词即回复',
      maxReplies: '每小时回复上限',
      minInterval: '最小回复间隔',
      perHour: '条/时',
      seconds: '秒',
      noteTitle: '提示',
      note: '评论内容命中任一关键词即触发机器人回复；配置判定条件后，机器人会先由 LLM 判断该不该回（判定不通过不占限频配额）。',
      atLeastOne: '至少修改一个字段',
      createSuccess: '创建成功',
      updateSuccess: '已保存'
    },
    // 自动回复高热度帖子 / 评论（auto_config.auto_reply）
    autoReply: {
      section: '自动回复',
      enabled: '自动回复高热度内容',
      enabledHint: '开启后后台定时扫描，自动回复达到门槛的帖子 / 评论',
      targets: '回复目标',
      targetPost: '帖子（发顶层评论）',
      targetComment: '评论（回复到该评论楼层）',
      postHotMin: '帖子热度门槛',
      postHotMinPlaceholder: '热度 ≥ 该值才回复',
      commentThreshold: '评论门槛（满足任一即可，0 = 不参与）',
      commentLikeMin: '点赞数 ≥',
      commentReplyMin: '回复数 ≥',
      windowHours: '时间窗口',
      windowHoursPlaceholder: '只看最近 N 小时发布的内容',
      hours: '小时',
      maxPerHour: '自动回复每小时上限',
      maxPerHourPlaceholder: '0 = 不单独限制',
      note: '每个机器人对同一目标最多自动回复一次（失败或判定不回复也算，不会重试）；自动回复同样受「每小时回复上限」「最小回复间隔」限频，配置了回复判定条件时也会先判定；平台总开关关闭时不会生效。',
      scopeGlobal: '全局机器人扫描全站内容。',
      scopeCircle: '圈内机器人只回复本圈内容。',
      errTargetsInvalid: '回复目标只能是帖子 / 评论，且不可重复',
      errTargetsRequired: '开启时至少选择一个回复目标',
      errThresholdRange: '门槛须为非负整数',
      errWindowRange: '时间窗口须为 1-168 小时',
      errMaxPerHourRange: '每小时上限须为 0-1000',
      errPostHotRequired: '回复帖子时热度门槛须 ≥ 1',
      errCommentThresholdRequired: '回复评论时点赞数或回复数门槛至少一个 ≥ 1'
    }
  },

  // 热点页（/hot）
  trending: {
    title: '热点',
    window24h: '24 小时',
    window7d: '7 天',
    posts: '热门帖子',
    circles: '热门圈子',
    users: '热门用户',
    refreshedJustNow: '刚刚更新',
    refreshedMinutesAgo: '{n} 分钟前更新',
    refreshedHoursAgo: '{n} 小时前更新',
    refreshedNever: '暂无数据',
    empty: '暂无热点数据',
    loadFailed: '加载热点失败'
  },

  // 发现页（/discover）：随机推送圈子 + 帖子，鼓励探索兴趣边界
  discover: {
    title: '发现',
    subtitle: '跳出信息气泡，探索你还没看过的新内容',
    modeStream: '探索流',
    modeSectioned: '分类',
    modeWall: '卡片墙',
    circlesSection: '发现新圈子',
    postsSection: '发现新帖子',
    exploreHint: '探索',
    joinDirect: '直接加入',
    joinPrivate: '私密',
    refreshed: '已为你换一批新内容',
    refresh: '换一批',
    refreshing: '换一批中…',
    empty: '暂无发现内容，点「换一批」试试',
    loadFailed: '加载发现内容失败',
    members: '成员',
    posts: '帖子'
  },

  // 登录页
  login: {
    tagline: '连接多元兴趣，发现无限可能',
    googleLogin: '使用 Google 账号继续',
    githubLogin: '使用 GitHub 继续',
    microsoftLogin: '使用 Microsoft 继续',
    loginAgreement: '登录即代表您同意',
    userAgreement: '《用户协议》',
    privacyPolicy: '《隐私政策》',
    and: '与',

    tabs: {
      login: '登录',
      register: '注册'
    },

    email: {
      label: '邮箱地址',
      placeholder: '请输入邮箱地址'
    },
    password: {
      label: '密码',
      placeholder: '请输入密码'
    },
    submit: '登录',
    dividerText: '或通过以下方式继续',
    browseAsGuest: '先逛逛',

    forgotPassword: '忘记密码？',

    register: {
      sendCode: '发送验证码',
      resendCode: '重新发送',
      resendIn: '{seconds}s 后重发',
      codeSent: '验证码已发送',
      codeSentTo: '验证码已发送至 {email}',
      codePlaceholder: '请输入6位验证码',
      verify: '验证',
      nextStep: '下一步',
      usernameLabel: '用户名',
      usernamePlaceholder: '请输入用户名',
      confirmPasswordLabel: '确认密码',
      confirmPasswordPlaceholder: '请再次输入密码',
      submit: '完成注册',
      success: '注册成功！',
      emailExists: '该邮箱已被注册',
      rateLimit: '请求过于频繁，请稍后再试'
    },

    forgotPasswordModal: {
      title: '找回密码',
      stepSendCode: '发送验证码',
      stepVerify: '验证',
      stepReset: '设置新密码',
      sendCode: '发送验证码',
      resetButton: '重置密码',
      emailNotFound: '该邮箱未注册，请检查或先注册',
      accountDisabled: '账号已被禁用，无法重置密码',
      verificationExpired: '验证已过期，请重新获取验证码',
      resetSuccess: '密码重置成功，请使用新密码登录',
      resetFailed: '重置密码失败'
    },

    passwordStrength: {
      weak: '弱',
      medium: '中',
      strong: '强',
      hint: '密码至少8位，需包含字母和数字'
    },

    validation: {
      emailRequired: '请输入邮箱地址',
      passwordRequired: '请输入密码',
      passwordMinLength: '密码至少需要8个字符',
      passwordPattern: '密码需包含字母和数字',
      confirmPasswordRequired: '请再次输入密码',
      passwordMismatch: '两次输入的密码不一致',
      codeRequired: '请输入验证码',
      codeFormat: '验证码为6位数字',
      usernameRequired: '请输入用户名',
      usernameLength: '用户名长度应在2-30个字符之间',
      usernamePattern: '用户名只能包含字母、数字、下划线和中文'
    },

    messages: {
      loginSuccess: '登录成功！',
      loginFailed: '登录失败：{error}',
      registerFailed: '注册失败：{error}',
      codeVerified: '邮箱验证成功',
      codeInvalid: '验证码无效或已过期',
      sendCodeFailed: '发送验证码失败'
    },

    // 访客点击写操作时的登录引导 Modal
    guestPrompt: {
      title: '登录后继续',
      desc: '登录后即可使用完整功能，还能记录你的浏览历史。',
      descWithAction: '登录后即可{action}，还能收藏与互动。',
      goLogin: '去登录',
      goRegister: '注册新账号',
      continueGuest: '暂不登录，继续浏览',
      actions: {
        like: '点赞',
        collect: '收藏',
        comment: '发表评论',
        join: '加入圈子'
      }
    }
  },

  // 表单验证
  validation: {
    required: '此项为必填项',
    invalidEmail: '请输入有效的邮箱地址',
    invalidPhone: '请输入有效的手机号',
    minLength: '最少需要 {min} 个字符',
    maxLength: '最多允许 {max} 个字符',
    tooShort: '内容太短',
    tooLong: '内容太长'
  },

  // 评论相关
  comment: {
    // 帖子已不可见（删除 / 私密圈非成员）时评论、回复返回 404
    unavailable: '内容不可用，或你已无权访问',
    editor: {
      title: '发表评论',
      placeholder: '写下你的评论...（支持图文、表情）',
      submit: '发表评论',
      success: '评论成功',
      failed: '评论失败',
      uploadImage: '上传图片',
      loginToComment: '登录后发表评论...'
    },
    list: {
      title: '评论 ({count})',
      viewReplies: '点击查看 {count} 条回复',
      collapseReplies: '收起回复',
      loadMoreReplies: '加载更多回复',
      repliesTitle: '{count} 条回复',
      closeReplies: '关闭回复',
      retryLoadReplies: '加载失败，点击重试',
      noMore: '没有更多评论了',
      empty: '暂无评论，快来发表第一条评论吧',
      pagination: {
        total: '共 {count} 条',
        page: '跳至',
        goto: '跳转'
      }
    },
    sort: {
      newest: '最新',
      hottest: '最热'
    },
    actions: {
      reply: '回复'
    },
    reply: {
      placeholder: '{name}',
      success: '回复成功',
      failed: '回复失败'
    }
  },

  // 站内通知（消息中心）
  notice: {
    title: '消息中心',
    tabs: {
      all: '全部',
      like: '点赞',
      collect: '收藏',
      comment: '评论',
      reply: '回复',
      mention: '提及',
      review: '审核'
    },
    readAll: '全部已读',
    empty: '暂无通知',
    noMore: '没有更多通知了',
    loadFailed: '加载通知失败',
    readAllSuccess: '已全部标记为已读',
    readAllFailed: '操作失败，请重试',
    targetDeleted: '内容已被删除',
    locateFailed: '评论定位失败，请稍后再试',
    // 举报通知（9）查不到所属圈子：帖子已下架 / 屏蔽 / 删除
    reportTargetGone: '该帖子已被处理或不可见',
    someone: '有人',
    // 文案模板：{actor}=触发人，{snippet}=摘要快照
    templates: {
      likePost: '{actor} 赞了你的帖子《{snippet}》',
      likeComment: '{actor} 赞了你的评论「{snippet}」',
      collectPost: '{actor} 收藏了你的帖子《{snippet}》',
      commentPost: '{actor} 评论了你的帖子：{snippet}',
      replyComment: '{actor} 回复了你的评论：{snippet}',
      mention: '{actor} 在帖子/评论中提到了你：{snippet}',
      postApproved: '你的帖子《{snippet}》已通过审核',
      postRejected: '你的帖子《{snippet}》未通过审核',
      postReported: '本圈帖子《{snippet}》被举报，请及时复核',
      postSelfHarm: '本圈 {actor} 的帖子《{snippet}》疑似有自伤 / 轻生倾向，请关注',
      postRemoved: '你的帖子《{snippet}》被举报，经圈主 / 管理员复核已下架'
    },
    mention: {
      button: '提及',
      searchPlaceholder: '搜索用户...',
      empty: '未找到相关用户',
      limitTip: '最多可提及 10 人',
      suggestHint: "键入 {'@'} 可快速提及用户"
    }
  },

  // 用户关注（后端 v0.9.0；B 线页面文案在本段追加）
  follow: {
    // 关注按钮：按 relation 取（utils/follow.js 的 relationLabelKey）
    relation: {
      none: '关注',
      following: '已关注',
      followedBy: '回关',
      mutual: '互相关注'
    },
    unfollow: '取消关注',
    followingCount: '关注',
    followerCount: '粉丝',
    listPrivate: '对方未公开关注和粉丝列表',
    empty: {
      following: '还没有关注任何人',
      followers: '还没有粉丝',
      mutual: '还没有互相关注的人'
    },
    settings: {
      listVisibility: '公开我的关注和粉丝列表',
      listVisibilityHint: '关闭后其他人只能看到数量，看不到名单'
    },
    // 错误文案：utils/follow.js 的 followErrorKey
    error: {
      limitReached: '关注人数已达上限',
      self: '不能关注自己',
      listPrivate: '对方未公开关注和粉丝列表',
      userNotFound: '用户不存在',
      loginRequired: '请先登录',
      generic: '操作失败，请稍后重试'
    }
  },

  // 私信（后端 v0.9.0；C 线页面文案在本段追加。顶层 messages 是通用提示，不要混用）
  message: {
    title: '私信',
    entry: '私信',
    send: '发送',
    inputPlaceholder: '发私信…',
    empty: '还没有私信',
    emptyChat: '还没有消息，打个招呼吧',
    deletedUser: '已注销用户',
    newMessagesDivider: '以下为新消息',
    loadOlder: '查看更早的消息',
    deleteConversation: '删除会话',
    deleteConfirm: '删除后对方不受影响；对方再发消息时会话会重新出现，只显示删除之后的消息。',
    // remaining_count 为数字时的提示（未关注 5 条限制）
    remaining: '对方关注你或回复前，还可发送 {count} 条',
    newMessageFrom: '{name} 给你发了私信',
    // 谁可以给我发私信（utils/message.js 的 DM_PERMISSION_OPTIONS）
    permission: {
      label: '谁可以给我发私信',
      everyone: '所有人',
      following: '仅我关注的人',
      mutual: '仅互相关注',
      closed: '关闭私信'
    },
    // 不能发送 / 发送失败：utils/message.js 的 dmReasonKey、dmSendErrorKey
    error: {
      recipientNotFound: '该用户不存在',
      recipientUnavailable: '该用户不接收私信',
      followingOnly: '对方只接收其关注的人的私信',
      mutualOnly: '对方只接收互相关注的人的私信',
      dmClosed: '对方已关闭私信',
      dmLimitReached: '对方关注你或回复后可继续发送',
      senderDisabled: '你的账号暂时无法发送私信',
      conversationNotFound: '会话不存在',
      rateLimited: '发送太频繁，请稍后再试',
      contentEmpty: '消息不能为空',
      contentTooLong: '消息不能超过 {max} 字',
      network: '发送失败，请检查网络后重试',
      unknown: '发送失败，请稍后重试'
    }
  },

  // 帖子分享（后端 v0.9.0；D 线页面文案在本段追加）
  share: {
    button: '分享',
    linkCopied: '链接已复制',
    copyFailed: '复制失败，请手动复制链接',
    // 错误文案：utils/share.js 的 shareErrorKey
    error: {
      notFound: '内容不可分享',
      retry: '分享计数失败，请重试'
    }
  },

  // 图片上传
  upload: {
    uploading: '上传中',
    success: '上传成功',
    failed: '上传失败，请重试',
    maxImages: '最多上传 {max} 张图片',
    exceedLimit: '已超出上限，仅上传前 {remaining} 张'
  },

  // 首页信息流
  feed: {
    refreshed: '推荐已刷新',
    empty: '暂无内容',
    emptyFollowing: '还没有加入圈子，去发现感兴趣的圈子吧',
    loadFailed: '加载失败',
    loginRequiredTab: '此内容需要登录后查看'
  },

  // 搜索结果页
  report: {
    reasons: {
      spam: '垃圾广告 / 引流',
      sexual: '色情低俗',
      harassment: '辱骂骚扰',
      illegal: '违法违规',
      violence: '暴力恐怖',
      privacy: '泄露隐私',
      selfHarm: '自伤轻生',
      other: '其他'
    }
  },
  search: {
    emptyKeyword: '请输入搜索关键词',
    unknownCircle: '未知圈子',
    untitled: '无标题',
    failed: '搜索失败',
    failedRetry: '搜索失败，请稍后重试'
  },

  // OAuth 登录成功回调页（/auth/success）
  authCallback: {
    welcome: '欢迎回来！',
    processing: '处理中...',
    enterCommunity: '进入社区',
    signingIn: '正在登录...',
    loginFailed: '登录失败',
    noLoginInfo: '未检测到登录信息，正在跳转到登录页...'
  },

  // 消息提示
  messages: {
    loginRequired: '请先登录',
    pleaseLoginFirst: '请先登录',
    operationFailed: '操作失败：{error}',
    getDetailFailed: '获取详情失败：{error}',
    deleteSuccess: '删除成功',
    deleteFailed: '删除失败：{error}',
    updateSuccess: '更新成功',
    updateFailed: '更新失败：{error}',
    copySuccess: '复制成功',
    copyFailed: '复制失败',
    likeFeaturePending: '点赞功能待实现',
    favoriteFeaturePending: '收藏功能待实现',
    interactionRetryLater: '操作未生效，请稍后重试',
    contentUnavailable: '内容已不存在',
    likeFailed: '点赞操作失败'
  },

  // 用户协议
  terms: {
    title: '用户协议',
    lastUpdated: '最后更新日期：2025年1月',
    backToLogin: '返回登录',
    sections: [
      {
        title: '1. 接受条款',
        paragraphs: [
          '欢迎使用趣吧（以下简称"本服务"）。通过访问或使用本服务，您确认您已阅读、理解并同意受本用户协议（以下简称"协议"）的约束。如果您不同意本协议的任何条款，请不要使用本服务。'
        ]
      },
      {
        title: '2. 服务说明',
        paragraphs: [
          '趣吧是一个兴趣社交平台，旨在帮助用户发现和连接各种兴趣社区。本服务包括但不限于：'
        ],
        lists: [
          [
            '用户账户创建和身份验证',
            '兴趣标签和社区发现',
            '与其他用户的互动和交流',
            '内容分享和讨论',
            '个性化推荐和匹配'
          ]
        ]
      },
      {
        title: '3. 用户账户',
        subsections: [
          {
            title: '3.1 账户注册',
            paragraphs: ['在使用本服务时，您需要创建一个账户。您声明并保证：'],
            lists: [
              [
                '您提供的注册信息真实、准确、完整',
                '您年满13周岁或达到所在地区的法定年龄',
                '您是使用您账户的唯一人员',
                '您将维护您的账户信息的安全性'
              ]
            ]
          },
          {
            title: '3.2 账户安全',
            paragraphs: [
              '您对您的账户下发生的所有活动负责，无论该活动是否经过您授权。如果您发现任何未经授权使用您账户的情况，请立即通知我们。'
            ]
          }
        ]
      },
      {
        title: '4. 用户行为准则',
        paragraphs: ['使用本服务时，您同意不会：'],
        lists: [
          [
            '上传、发布或传输任何非法、有害、威胁性、辱骂性、骚扰性、诽谤性、粗俗、淫秽或其他令人反感的内容',
            '冒充任何个人或实体，或虚假陈述或以其他方式歪曲您与任何个人或实体的从属关系',
            '上传、发布或传输任何侵犯任何方专利、商标、商业秘密、版权或其他专有权利的内容',
            '干扰或破坏本服务或连接到本服务的服务器或网络',
            '故意或无意地违反任何适用的地方法、州、国家或国际法律',
            '骚扰或以其他方式伤害另一个用户',
            '收集或存储其他用户的个人数据'
          ]
        ]
      },
      {
        title: '5. 知识产权',
        paragraphs: [
          '本服务及其原始内容、功能和设计均为趣吧及其许可方的专有财产，受版权、商标和其他知识产权法律的保护。您同意不复制、修改、分发或以其他方式使用本服务的任何部分，除非获得我们的明确书面许可。'
        ]
      },
      {
        title: '6. 内容权利',
        paragraphs: [
          '您保留您通过本服务提交、发布或显示的内容（"您的内容"）的所有权。通过提交、发布或显示您的内容，您授予我们全球性、免版税、非独占的许可，以使用、复制、修改、分发和展示您的内容，以便：'
        ],
        lists: [
          [
            '提供、维护和改进本服务',
            '开发和提供新服务',
            '防止滥用和违法行为',
            '履行我们的法律义务'
          ]
        ]
      },
      {
        title: '7. 终止',
        paragraphs: [
          '我们可能随时自行决定，在有或无通知的情况下，暂停或终止您的账户和访问本服务的权利，原因包括但不限于：'
        ],
        lists: [
          [
            '违反本协议',
            '从事欺诈或非法活动',
            '对其他用户或我们的服务造成安全风险',
            '长时间不活跃'
          ]
        ]
      },
      {
        title: '8. 免责声明',
        paragraphs: [
          '本服务按"原样"和"可用"的基础提供，不提供任何明示或暗示的保证。我们不对以下情况承担责任：'
        ],
        lists: [
          [
            '服务的持续可用性、及时性或安全性',
            '服务中包含或链接的任何内容、广告、产品或其他材料的准确性、可靠性或内容',
            '因使用本服务而产生的任何直接、间接、附带、特殊或后果性损害'
          ]
        ]
      },
      {
        title: '9. 赔偿',
        paragraphs: [
          '您同意赔偿并使趣吧及其官员、董事、员工、代理商和许可方免受因您使用本服务或违反本协议而产生的任何索赔、损害、义务、损失、责任、成本或债务。'
        ]
      },
      {
        title: '10. 协议变更',
        paragraphs: [
          '我们保留随时修改或替换本协议的权利。如果修订是实质性的，我们将在新条款生效前提供通知。继续使用本服务即表示您接受修订后的协议。'
        ]
      },
      {
        title: '11. 适用法律',
        paragraphs: [
          '本协议受中华人民共和国法律管辖并依其解释。与本协议相关的任何争议应提交至有管辖权的人民法院解决。'
        ]
      },
      {
        title: '12. 联系我们',
        paragraphs: ['如果您对本协议有任何疑问或疑虑，请通过以下方式与我们联系：'],
        lists: [['邮箱：legal@qubar.com', '地址：中国上海市浦东新区']]
      }
    ]
  },

  // 隐私政策
  privacy: {
    title: '隐私政策',
    lastUpdated: '最后更新日期：2025年1月',
    backToLogin: '返回登录',
    intro:
      '趣吧（"我们"、"我们的"或"本服务"）尊重您的隐私，并致力于保护您的个人信息。本隐私政策解释了我们如何收集、使用、披露和保护您的信息。使用本服务即表示您同意本隐私政策的做法。',
    sections: [
      {
        title: '1. 我们收集的信息',
        subsections: [
          {
            title: '1.1 您提供的信息',
            paragraphs: ['当您使用本服务时，我们可能收集以下信息：'],
            lists: [
              [
                { strong: '账户信息：', text: '姓名、电子邮件地址、个人资料图片、出生日期' },
                { strong: '认证信息：', text: '通过第三方服务（如Google）进行身份验证时提供的信息' },
                { strong: '兴趣标签：', text: '您选择或生成的兴趣偏好和标签' },
                { strong: '内容：', text: '您发布的帖子、评论、图片、视频和其他内容' },
                { strong: '通信：', text: '您与我们之间的通信内容' }
              ]
            ]
          },
          {
            title: '1.2 自动收集的信息',
            paragraphs: ['当您访问或使用本服务时，我们自动收集：'],
            lists: [
              [
                { strong: '设备信息：', text: '设备类型、操作系统、浏览器类型、唯一设备标识符' },
                { strong: '使用信息：', text: '访问时间、浏览的页面、使用的功能、点击的链接' },
                { strong: '位置信息：', text: '基于IP地址的大致地理位置' },
                { strong: '日志信息：', text: '服务器日志、错误报告、性能数据' }
              ]
            ]
          },
          {
            title: '1.3 Cookie和类似技术',
            paragraphs: [
              '我们使用Cookie、Web Beacons和类似技术来收集和存储信息。Cookie是存储在您设备上的小型文本文件，用于记住您的偏好、改善用户体验和分析使用趋势。'
            ]
          }
        ]
      },
      {
        title: '2. 我们如何使用您的信息',
        paragraphs: ['我们使用收集的信息用于：'],
        lists: [
          [
            { strong: '提供、维护和改进服务：', text: '处理您的请求、个性化您的体验、开发新功能' },
            { strong: '身份验证和安全：', text: '验证您的身份、防止欺诈和滥用、保护账户安全' },
            { strong: '个性化推荐：', text: '根据您的兴趣推荐内容、用户和社区' },
            { strong: '通信：', text: '发送服务通知、更新、安全警报和营销信息（可选）' },
            { strong: '分析：', text: '分析使用趋势、改进服务性能、了解用户行为' },
            { strong: '法律合规：', text: '遵守法律义务、响应法律请求、保护我们的权利' }
          ]
        ]
      },
      {
        title: '3. 信息共享',
        subsections: [
          {
            title: '3.1 我们不会出售您的个人信息',
            paragraphs: ['我们不会将您的个人信息出售给第三方。']
          },
          {
            title: '3.2 我们可能共享信息的情况',
            lists: [
              [
                { strong: '服务提供商：', text: '与帮助我们运营服务的第三方服务提供商（如云存储、数据分析、支付处理）共享必要的信息' },
                { strong: '业务转让：', text: '在合并、收购或资产出售的情况下，您的信息可能被转让给相关方' },
                { strong: '法律要求：', text: '当我们真诚地认为法律要求、为了保护我们的权利或防止违法行为时披露信息' },
                { strong: '经您同意：', text: '在您明确同意的情况下，以其他方式共享信息' }
              ]
            ]
          },
          {
            title: '3.3 公开信息',
            paragraphs: [
              '您的个人资料信息（姓名、照片、兴趣标签）对其他用户可见。您可以通过账户设置控制哪些信息公开。'
            ]
          }
        ]
      },
      {
        title: '4. 数据安全',
        paragraphs: ['我们采取合理的技术和组织措施保护您的信息免受未经授权的访问、使用或披露：'],
        lists: [
          [
            { strong: '加密：', text: '在传输过程中使用SSL/TLS加密' },
            { strong: '访问控制：', text: '限制对个人信息的访问权限' },
            { strong: '安全审计：', text: '定期进行安全审查和评估' },
            { strong: '数据保留：', text: '在必要期限内保留信息，之后安全删除' }
          ]
        ],
        warning:
          '尽管我们努力保护您的信息，但没有互联网传输或电子存储方法是100%安全的。虽然我们力求使用合理的手段保护您的个人信息，但我们无法保证其绝对安全。'
      },
      {
        title: '5. 您的隐私权利',
        paragraphs: ['根据适用法律，您可能拥有以下权利：'],
        lists: [
          [
            { strong: '访问权：', text: '请求访问我们持有的您的个人信息副本' },
            { strong: '更正权：', text: '要求更正不准确或不完整的信息' },
            { strong: '删除权：', text: '要求删除您的个人信息' },
            { strong: '限制处理权：', text: '要求限制我们如何使用您的信息' },
            { strong: '数据可携带权：', text: '要求以结构化格式接收您的信息' },
            { strong: '反对权：', text: '反对我们处理您的信息' },
            { strong: '撤回同意：', text: '撤回之前给予的同意（不影响撤回前的合法性）' }
          ]
        ],
        paragraphs2: ['要行使这些权利，请通过本政策底部提供的联系方式与我们联系。我们将及时回应您的请求。']
      },
      {
        title: '6. 第三方链接',
        paragraphs: [
          '本服务可能包含指向第三方网站或服务的链接。我们对第三方网站的做法或内容不承担任何责任。我们鼓励您在离开本服务时阅读这些第三方的隐私政策。'
        ]
      },
      {
        title: '7. 儿童隐私',
        paragraphs: [
          '本服务不针对13岁以下的儿童。我们不会故意收集13岁以下儿童的个人信息。如果我们发现已收集此类信息，我们将采取措施删除它。'
        ]
      },
      {
        title: '8. 数据跨境传输',
        paragraphs: [
          '您的信息可能被传输到您所在国家/地区以外的国家/地区并在那里处理。这些国家/地区的数据保护法律可能与您所在国家/地区的法律不同。',
          '当我们跨境传输您的信息时，我们将采取适当措施确保您的信息得到与本隐私政策一致的保护。'
        ]
      },
      {
        title: '9. 隐私政策的变更',
        paragraphs: [
          '我们可能会不时更新本隐私政策。变更后，我们将在本页面上发布新的隐私政策，并更新"最后更新日期"。我们建议您定期查看本隐私政策。',
          '重大变更将通过电子邮件或服务内的显著通知告知您。继续使用本服务即表示您接受更新后的隐私政策。'
        ]
      },
      {
        title: '10. 联系我们',
        paragraphs: ['如果您对本隐私政策或我们的信息实践有任何疑问、疑虑或请求，请通过以下方式联系我们：'],
        lists: [
          [
            { strong: '邮箱：', text: 'privacy@qubar.com' },
            { strong: '地址：', text: '中国上海市浦东新区张江高科技园区' },
            { strong: '联系电话：', text: '+86 21-1234-5678' }
          ]
        ],
        paragraphs2: ['数据保护官联系方式：', '邮箱：dpo@qubar.com']
      },
      {
        title: '11. 适用法律',
        paragraphs: [
          '本隐私政策受中华人民共和国法律管辖并依其解释。我们遵守适用的数据保护法律，包括《中华人民共和国个人信息保护法》（PIPL）和《中华人民共和国网络安全法》。'
        ]
      }
    ],
    summaryTitle: '隐私原则总结',
    summary: [
      '✓ 透明度：我们清楚地解释我们如何使用您的数据',
      '✓ 控制：您拥有控制个人信息的权利',
      '✓ 安全：我们实施强安全措施保护您的信息',
      '✓ 合规：我们遵守全球隐私法规',
      '✓ 最小化：我们只收集必要的信息'
    ]
  }
}
