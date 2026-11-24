/* Qubar 趣吧 宣传页脚本：零依赖，原生 JS
 * - i18n：中文文案直接写在 HTML（data-i18n），启动时从 DOM 收集为 zh 字典；英文字典见 EN
 * - Hero：三层兴趣图标（鼠标 / 滚动视差 + 点击冒泡）、Q Logo 回音、关键词轮换
 * - 各示意界面进入视口时播放一次演示，离开视口时复原为完整静态状态
 * - prefers-reduced-motion：不启动视差、轮换与自动演示，页面保持完整静态内容 */
(function () {
  "use strict";

  var reduceMq = window.matchMedia ? window.matchMedia("(prefers-reduced-motion: reduce)") : null;
  var reduced = !!(reduceMq && reduceMq.matches);
  var finePointer = window.matchMedia ? window.matchMedia("(hover: hover) and (pointer: fine)").matches : false;
  var hasIO = "IntersectionObserver" in window;

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }
  function reflow(el) { return el.offsetWidth; }
  /* 重播 CSS 动画：已带该类时优先用 getAnimations() 归零重播，避免 offsetWidth 强制同步布局 */
  function restartClass(el, cls) {
    if (!el.classList.contains(cls)) { el.classList.add(cls); return; }
    var anims = el.getAnimations ? el.getAnimations() : [];
    if (anims.length) { anims.forEach(function (a) { a.currentTime = 0; a.play(); }); return; }
    el.classList.remove(cls); reflow(el); el.classList.add(cls);
  }

  /* ======================= i18n ======================= */
  var EN = {
    "meta.title": "Qubar · Where every passion finds its echo",
    "meta.desc": "Qubar is an open, open-source and non-profit interest community where every circle is run by its own owner. Browse without signing up, share posts with pictures, and let well-mannered AI bots help out.",
    "a11y.skip": "Skip to content", "a11y.home": "Qubar home", "a11y.nav": "Page sections", "a11y.top": "Back to top", "a11y.footNav": "Footer links",
    "brand": "Qubar",
    "nav.ideas": "Ideas", "nav.circles": "Circles", "nav.create": "Create", "nav.discover": "Discover", "nav.bots": "AI buddies", "nav.open": "Open source", "nav.enter": "Enter Qubar",
    "hero.badge": "Open · Open source · Self-run · Non-profit",
    "hero.titleA": "Where every", "hero.word": "passion", "hero.titleB": "finds its echo",
    "hero.sub": "Qubar is an open community for the things you love. Find your people and show off what you’re into. Every circle is run by its own hosts, with a well-mannered AI buddy to keep the conversation going.",
    "cta.enter": "Enter Qubar", "cta.browse": "Look around (no sign-up)",
    "hero.n1": "Browse as a guest", "hero.n2": "Sign in with email, Google, GitHub or Microsoft", "hero.n3": "中文 / English",
    "hero.hint": "Go on, poke a floating icon ✨",
    "strip.title": "There’s a circle waiting for you", "demo.circles": "Example circles · names are made up",
    "cc.film": "Film Walks", "cc.ride": "City Ride", "cc.radio": "Late-Night Radio", "cc.craft": "Weekend Makers", "cc.indie": "Indie Game Corner", "cc.read": "Slow Reading Club",
    "cc.coffee": "Coffee Lab", "cc.code": "Code After Dark", "cc.plant": "Balcony Plants", "cc.hike": "Weekend Hikers", "cc.draw": "A Sketch a Day", "cc.movie": "Old Movie Theater",
    "ideas.eyebrow": "What we believe", "ideas.title": "A community should belong to everyone",
    "ideas.sub": "Qubar isn’t here to cash in on your attention. We just want to get these things right.",
    "ideas.hint": "Hover over a card, or tap it, to flip it over",
    "ideas.c1t": "Open", "ideas.c1s": "Walk right in",
    "ideas.c1b": "Look around as a guest without signing up. When you want to join in, sign in with email, Google, GitHub or Microsoft, in Chinese or English.",
    "ideas.c2t": "Open source", "ideas.c2s": "Nothing hidden",
    "ideas.c2b": "Both the frontend and the backend code are public (MIT license), so anyone can take a look. Ideas and bug reports are always welcome. Let’s make Qubar better together.",
    "ideas.c3t": "Shared", "ideas.c3s": "Content belongs to the community",
    "ideas.c3b": "Posts and conversations belong to the community. Interests flow between circles, and good posts reach more people through For You, Trending and Discover.",
    "ideas.c4t": "Self-run", "ideas.c4s": "Circles make their own calls",
    "ideas.c4b": "Members, circle rules, moderation and circle bots are all managed by each circle’s owner and admins.",
    "ideas.c5t": "Non-profit & light", "ideas.c5s": "Quiet by design",
    "ideas.c5b": "We’re not here to cash in on traffic. No pushy distractions, just a clean interface that’s pleasant to use.",
    "ideas.c6t": "AI as a buddy", "ideas.c6s": "Helps out, never hogs the stage",
    "ideas.c6b": "Bots answer questions and liven things up. They post from their own accounts, so it’s always clear who’s a bot, and they have a rate limit. How a circle bot is used is up to the circle owner.",
    "circles.eyebrow": "Circles, run by their hosts", "circles.title": "Start a circle,<br>run it your way",
    "circles.lead": "Anyone can start a circle, and it’s ready to use right away. Write an intro and some circle rules, bring in a few admins to help, and let AI take a first look at new posts. Anything it isn’t sure about is your call.",
    "circles.f1t": "Start a circle in seconds", "circles.f1p": "Anyone can create a circle and use it right away. The owner can edit the circle’s profile, intro and rules at any time. Joining an open circle takes one click.",
    "circles.f2t": "Owner, admins, members", "circles.f2p": "Three roles, each with a job: the owner can appoint admins or hand over ownership, and admins can mute or block members who stir up trouble. “My circles” can be filtered by your role.",
    "circles.f3t": "AI looks first, people decide", "circles.f3p": "AI reads the text of every new post first. Anything it can’t call goes into the circle’s review queue, where admins approve or reject it, and the author is told the result.",
    "circles.f4t": "Reports get handled, alerts take you there", "circles.f4p": "Members can report posts. The owner and admins get an alert and can keep the post or take it down, and the author is notified if it’s taken down. The alert opens the circle’s Review tab directly, with the pending count right there.",
    "circles.rOwner": "Owner", "circles.rAdmin": "Admin", "circles.rMember": "Member", "circles.rMuted": "Muted",
    "mod.title": "City Ride · Review", "mod.pending": "Pending", "mod.unsure": "AI: not sure", "mod.report": "Report",
    "mod.i1a": "Xiaoman", "mod.i1t": "Sunday morning ride, 40 km along the lake. Add me on WeChat to join", "mod.i1w": "Why: contains contact details",
    "mod.i2a": "A passing dealer", "mod.i2t": "Brand-new road bikes, super cheap, DM me", "mod.i2w": "Why: looks like an ad",
    "mod.i3a": "Member report", "mod.i3t": "“Cheapest bike lights anywhere, buy via this link” was reported", "mod.i3w": "Report reason: ad",
    "mod.i4a": "Hehe", "mod.i4t": "Newbie question: how do I adjust my cleats? Tutorial at this link", "mod.i4w": "Why: contains an external link",
    "mod.reject": "Reject", "mod.approve": "Approve", "mod.keep": "Keep", "mod.takedown": "Take down",
    "mod.done": "All done. Go grab some water",
    "demo.note": "Demo UI · names, circles and posts are made up · the buttons work",
    "create.eyebrow": "Create & connect", "create.title": "Share what you love,<br>someone will catch it",
    "create.lead": "Add a heading, bold the important bits, quote a line, attach your photos (up to 9 per post), pick a circle, and post. After that, every like, reply and @mention reaches you right away.",
    "create.f1t": "Posts with pictures", "create.f1p": "The editor handles headings, bold, quotes and more. Each post can have up to 9 pictures, and goes to the circle you choose.",
    "create.f2t": "Threaded comments", "create.f2p": "Comments and replies stay neatly stacked, however lively it gets. Comments can include pictures too.",
    "create.f3t": "Likes, saves, history", "create.f3p": "Like posts and comments, and save the posts you love. Your saves and viewing history are on your profile.",
    "create.f4t": "@mentions that reach the right person", "create.f4p": "Mention someone and they get notified. Tap the notification to land right on that comment.",
    "ed.md": "Early-autumn alleys\nOut the door at 6 a.m. The light comes in sideways and the shadows go on forever.", "ed.to": "Post to: Film Walks", "ed.publish": "Publish",
    "post.author": "Acha", "post.time": "Just now", "post.title": "Early-autumn alleys", "post.like": "Like", "post.fav": "Save",
    "post.c1a": "Lulu", "post.c1": "The light in the second shot is unreal!",
    "post.c2": "Replying to <span class=\"mention\">@Lulu</span>: waited all morning for it 😄",
    "demo.note2": "Demo UI · made-up content · try the like and save buttons",
    "disc.eyebrow": "For You · Trending · Discover", "disc.title": "Three doors for three kinds of curiosity",
    "disc.sub": "For You is based on the circles you’ve joined and what you’ve viewed. Trending gathers the most popular posts on the site. Discover introduces new circles and new posts. Looking for a specific post, circle or user? Just search.",
    "disc.tabsLabel": "Feeds (demo)", "disc.t1": "For You", "disc.t2": "Trending", "disc.t3": "Discover",
    "disc.p1": "Based on your circles and what you’ve viewed", "disc.p2": "Popular across the site", "disc.p3": "New circles, new posts",
    "disc.r1": "Night ride by the river: the best-lit route", "disc.r2": "Can you still shoot expired film?", "disc.r3": "Picking your first pour-over kettle",
    "disc.h1": "The song you had on repeat all week", "disc.h2": "Day 100 of making a game solo", "disc.h3": "Show us your balcony in autumn",
    "disc.newCircle": "New circle",
    "disc.d1": "“A Sketch a Day” just opened. Come hand in your homework", "disc.d2": "Hehe made a first post in “Weekend Hikers”", "disc.d3": "“Old Movie Theater” posted this week’s lineup",
    "search.q": "coffee", "search.g1": "Circles", "search.g2": "Posts", "search.g3": "Users",
    "search.r2": "First espresso machine: semi-auto or capsules?", "search.r3": "A map of coffee stops along cycling routes", "search.r4": "CoffeeBean",
    "demo.note3": "Demo UI · made-up content · click a tab, or use the arrow keys",
    "bots.eyebrow": "AI buddies", "bots.title": "Bring in Little Q<br>to warm up the room",
    "bots.lead": "Bots can connect to different AI models, with their own name, avatar and personality, and one sentence can tell them when to reply. They only show up when it makes sense, and never crowd out the people.",
    "bots.trigTitle": "When do they speak up?",
    "bots.t1": "@mentioned in a new post", "bots.t2": "A comment hits a keyword", "bots.t3": "An admin asks it to reply", "bots.t4": "Auto-replies to popular posts and comments",
    "bots.f1t": "Global bots + circle bots", "bots.f1p": "Global bots are provided by the platform. Circle bots are managed by the circle owner and only speak in their own circle.",
    "bots.f2t": "One sentence keeps them in check", "bots.f2p": "Tell the bot in one sentence when it should reply and when it should stay quiet.",
    "bots.f3t": "Knows its manners", "bots.f3p": "When auto-replying, a bot answers each post or comment at most once. It has a rate limit, bots never chat with each other, and it posts from its own account, labeled “Bot” on its profile.",
    "chat.title": "City Ride · Post", "chat.postTitle": "First 100 km ride. What should I bring?",
    "chat.uName": "Xiaoman · new post", "chat.q": "First ride this long and I’m a bit nervous. <span class=\"mention\">@Little Q</span> could you put together a supplies checklist?",
    "chat.bName": "Little Q", "chat.badge": "AI bot",
    "chat.a": "On it! Bring two bottles of water, three or four energy bars, a spare tube and a pump, plus a bike light. Check your tire pressure before you set off and refill your water about every 20 km. Take it easy and have fun 🚴",
    "bots.persona": "Persona: ", "bots.personaText": "A friendly cycling veteran who keeps it short and loves emoji",
    "bots.r1": "Auto-replies: once per post", "bots.r2": "Rate-limited", "bots.r3": "Stays in its own circle", "bots.r4": "Never replies to other bots",
    "demo.note4": "Demo UI · made-up conversation",
    "nt.eyebrow": "Real-time notifications", "nt.title": "When someone answers,<br>you’ll know right away",
    "nt.lead": "Likes, saves, comments, replies, @mentions, review results: they all reach you in real time. Sign in on your phone and your computer at the same time, and both get the news.",
    "nt.f1t": "Real time", "nt.f1p": "No need to keep refreshing. New notifications just show up.",
    "nt.f2t": "Several devices at once", "nt.f2p": "Read on your phone, reply on your laptop. Notifications arrive on both.",
    "nt.f3t": "One tap to get there", "nt.f3p": "@mention alerts take you straight to the comment, and report alerts for admins open the circle’s Review tab.",
    "nt.center": "Notifications", "nt.now": "Just now",
    "nt.n1": "<b>Lulu</b> liked your post “Early-autumn alleys”",
    "nt.n2": "<b>Bai</b> commented on your post: “Love these colors”",
    "nt.n3": "<b>Acha</b> @mentioned you in a comment",
    "nt.n4": "Your post “Sunday morning ride” <b>was approved</b>",
    "nt.n5": "“Cheapest bike lights” <b>was reported</b> · Handle it →",
    "demo.note5": "Demo UI · made-up notifications",
    "open.eyebrow": "Open source · Non-profit", "open.title": "Open code,<br>a community for everyone",
    "open.lead": "Qubar’s frontend and backend code are both public on GitHub (MIT license), so anyone can see how it works. We’re not here to cash in on traffic, just to build a good community. Got an idea or found a problem? Tell us, and let’s make Qubar better together.", "open.backend": "Backend code · MIT", "open.frontend": "Website code · MIT",
    "end.title": "Your passion deserves to be heard",
    "end.sub": "Look around first, and sign in if you like it. You’re also welcome on GitHub to help make Qubar better.",
    "foot.slogan": "Where every passion finds its echo.", "foot.site": "qubar.site", "foot.be": "Backend on GitHub", "foot.fe": "Frontend on GitHub",
    "circles.join": "Join",
    "circles.intro": "After-work rides around the city at an easy pace. Share routes, gear and the views along the way.",
    "circles.rulesTitle": "CIRCLE RULES",
    "circles.rule1": "For group rides, list the route, time and pace",
    "circles.rule2": "No ads; mark second-hand sales clearly",
    "ed.count": "3 / 9 photos",
    "disc.newPost": "New post",
    "chat.floor": "replied under the post",
    "circles.joined": "Joined",
    "foot.copy": "© 2026 Qubar · Frontend and backend open source under MIT · Names, circles and posts in this page’s demo UI are made up"
  };
  /* 只在脚本里用到、HTML 里没有的中文文案 */
  var ZH_EXTRA = {
    "circles.joined": "已加入"
  };
  /* 头像里的单字：中文名 → 英文名首字母 */
  var AV_EN = { "白": "B", "鹿": "L", "满": "X", "茶": "A", "禾": "H", "卖": "D", "豆": "C" };

  var ZH = {};
  var attrBindings = [];
  $$("[data-i18n]").forEach(function (el) {
    var k = el.getAttribute("data-i18n");
    if (!(k in ZH)) ZH[k] = el.innerHTML.trim();
  });
  $$("[data-i18n-attr]").forEach(function (el) {
    el.getAttribute("data-i18n-attr").split(";").forEach(function (pair) {
      var p = pair.split(":");
      if (p.length !== 2) return;
      attrBindings.push({ el: el, attr: p[0].trim(), key: p[1].trim() });
      if (!(p[1].trim() in ZH)) ZH[p[1].trim()] = el.getAttribute(p[0].trim()) || "";
    });
  });
  ZH["meta.title"] = document.title;
  var metaDesc = $('meta[name="description"]');
  ZH["meta.desc"] = metaDesc ? metaDesc.getAttribute("content") : "";
  Object.keys(ZH_EXTRA).forEach(function (k) { ZH[k] = ZH_EXTRA[k]; });

  var DICT = { zh: ZH, en: EN };
  var LANG = "zh";
  function t(key) {
    var v = DICT[LANG][key];
    return typeof v === "string" ? v : (ZH[key] || "");
  }
  function plain(html) {
    var d = document.createElement("div");
    d.innerHTML = html;
    return d.textContent;
  }

  function readLangPref() {
    var q = /[?&]lang=(zh|en)\b/.exec(location.search);
    if (q) return q[1];
    try {
      var s = localStorage.getItem("qubar-landing-lang");
      if (s === "zh" || s === "en") return s;
    } catch (e) { /* 隐私模式下 localStorage 不可用，忽略 */ }
    return (navigator.language || "zh").toLowerCase().indexOf("zh") === 0 ? "zh" : "en";
  }
  function storeLangPref(v) {
    try { localStorage.setItem("qubar-landing-lang", v); } catch (e) { /* 忽略 */ }
  }

  var langListeners = [];
  function applyLang(lang) {
    LANG = lang;
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    document.title = t("meta.title");
    if (metaDesc) metaDesc.setAttribute("content", t("meta.desc"));
    $$("[data-i18n]").forEach(function (el) { el.innerHTML = t(el.getAttribute("data-i18n")); });
    attrBindings.forEach(function (b) { b.el.setAttribute(b.attr, t(b.key)); });
    $$(".av").forEach(function (el) {
      if (!el.dataset.zh) el.dataset.zh = el.textContent;
      el.textContent = lang === "en" && AV_EN[el.dataset.zh] ? AV_EN[el.dataset.zh] : el.dataset.zh;
    });
    var tog = $("#langToggle");
    if (tog) {
      tog.textContent = lang === "zh" ? "EN" : "中文";
      tog.setAttribute("lang", lang === "zh" ? "en" : "zh-CN");
      tog.setAttribute("aria-label", lang === "zh" ? "Switch to English" : "切换到中文");
    }
    langListeners.forEach(function (fn) { fn(lang); });
  }

  /* ======================= 导航：滚动进度 + 毛玻璃 ======================= */
  var nav = $("#nav");
  var progress = $("#progress");
  var hero = $(".hero");
  var heroVisible = true;
  var scrollTicking = false;
  function onScrollFrame() {
    scrollTicking = false;
    var y = window.scrollY || document.documentElement.scrollTop;
    var max = document.documentElement.scrollHeight - window.innerHeight;
    if (nav) nav.classList.toggle("scrolled", y > 16);
    if (progress) progress.style.transform = "scaleX(" + (max > 0 ? Math.min(1, y / max) : 0) + ")";
    hiKick();
  }
  window.addEventListener("scroll", function () {
    if (!scrollTicking) { scrollTicking = true; requestAnimationFrame(onScrollFrame); }
  }, { passive: true });

  /* ======================= 滚动入场 ======================= */
  function initReveal() {
    var els = $$(".reveal");
    if (reduced || !hasIO) { els.forEach(function (el) { el.classList.add("in"); }); return; }
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
      });
    }, { threshold: 0.12, rootMargin: "0px 0px -6% 0px" });
    els.forEach(function (el) { io.observe(el); });
  }

  /* ======================= Hero：兴趣图标 ======================= */
  /* 宽屏三层，位置为相对 Hero 的百分比。
   * i 图标、c 兴趣色、s 尺寸(px)、o 透明度、b 模糊(px，仅远景)、rot 入场旋转角、f 漂浮方式 */
  var HI_DEEP = [
    { i: "game", c: "sky", x: "-3%", y: "2%", s: 270, o: 0.15, b: 9, rot: -18, f: "a" },
    { i: "palette", c: "rose", x: "76%", y: "46%", s: 250, o: 0.13, b: 9, rot: 12, f: "b" },
    { i: "book", c: "sky", x: "1%", y: "64%", s: 230, o: 0.14, b: 8, rot: 8, f: "c" },
    { i: "camera", c: "mint", x: "80%", y: "2%", s: 210, o: 0.15, b: 8, rot: -10, f: "a" },
    { i: "film", c: "lilac", x: "38%", y: "76%", s: 290, o: 0.1, b: 10, rot: 6, f: "b" },
    { i: "music", c: "lilac", x: "44%", y: "-8%", s: 240, o: 0.1, b: 10, rot: -6, f: "c" }
  ];
  var HI_MID = [
    { i: "book", c: "sky", x: "25%", y: "30%", s: 104, o: 0.3, rot: -8, f: "b" },
    { i: "palette", c: "rose", x: "20%", y: "57%", s: 96, o: 0.26, rot: 10, f: "a" },
    { i: "scissors", c: "sun", x: "62%", y: "17%", s: 92, o: 0.3, rot: 20, f: "c" },
    { i: "coffee", c: "sun", x: "73%", y: "63%", s: 100, o: 0.24, rot: -12, f: "b" },
    { i: "mountain", c: "mint", x: "92%", y: "38%", s: 88, o: 0.28, rot: -18, f: "a" },
    { i: "film", c: "coral", x: "20%", y: "86%", s: 88, o: 0.24, rot: 8, f: "c" },
    { i: "sparkles", c: "sky", x: "21%", y: "5%", s: 66, o: 0.34, rot: 0, f: "b" },
    { i: "music", c: "lilac", x: "70%", y: "88%", s: 78, o: 0.26, rot: 6, f: "a" }
  ];
  var HI_FRONT = [
    { i: "camera", c: "coral", x: "8%", y: "18%", s: 72, rot: -8, f: "a" },
    { i: "music", c: "lilac", x: "84%", y: "21%", s: 62, rot: 6, f: "b" },
    { i: "game", c: "sky", x: "6%", y: "50%", s: 66, rot: 8, f: "c" },
    { i: "bike", c: "mint", x: "86%", y: "54%", s: 70, rot: -6, f: "a" },
    { i: "code", c: "sky", x: "13%", y: "79%", s: 54, rot: -10, f: "b" },
    { i: "coffee", c: "sun", x: "80%", y: "81%", s: 56, rot: 4, f: "c" },
    { i: "heart", c: "rose", x: "30%", y: "12%", s: 44, rot: 0, f: "b" },
    { i: "star", c: "sun", x: "66%", y: "9%", s: 46, rot: 0, f: "a" },
    { i: "leaf", c: "mint", x: "93%", y: "8%", s: 40, rot: 12, f: "c" }
  ];
  /* 窄屏前景：以 Logo 中心为原点的像素偏移（左侧用 right、右侧用 left），始终贴着 Logo、不压正文 */
  var HI_NEAR = [
    { i: "camera", c: "coral", side: "l", d: 66, top: -46, s: 46, rot: -8, f: "a" },
    { i: "music", c: "lilac", side: "r", d: 64, top: -40, s: 42, rot: 6, f: "b" },
    { i: "bike", c: "mint", side: "l", d: 92, top: 18, s: 40, rot: 8, f: "c" },
    { i: "code", c: "sky", side: "r", d: 90, top: 22, s: 44, rot: -10, f: "a" },
    { i: "heart", c: "rose", side: "l", d: 140, top: -20, s: 30, rot: 0, f: "b" },
    { i: "star", c: "sun", side: "r", d: 138, top: -16, s: 30, rot: 0, f: "c" }
  ];

  function makeIcon(cfg, idx, interactive, near) {
    var el = document.createElement("div");
    el.className = "hicon c-" + cfg.c;
    el.style.setProperty("--s", cfg.s);
    if (near) {
      el.style.top = (cfg.top - cfg.s / 2) + "px";
      el.style[cfg.side === "l" ? "right" : "left"] = cfg.d + "px";
    } else {
      el.style.setProperty("--x", cfg.x);
      el.style.setProperty("--y", cfg.y);
    }
    if (cfg.o) el.style.setProperty("--o", cfg.o);
    if (cfg.b) el.style.setProperty("--b", cfg.b + "px");
    el.style.setProperty("--rot", (cfg.rot || 0) + "deg");
    el.style.setProperty("--delay", (0.05 + idx * 0.08).toFixed(2) + "s");
    el.style.setProperty("--float", "hi-float-" + (cfg.f || "a"));
    el.style.setProperty("--dur", ((cfg.s > 150 ? 14 : 7) + Math.random() * 5).toFixed(2) + "s");
    el.style.setProperty("--fdelay", (-Math.random() * 8).toFixed(2) + "s");
    if (cfg.s < 80) el.style.setProperty("--sw", "1.9");
    el.innerHTML = '<div class="hicon-in"><div class="hicon-float"><svg class="ic"><use href="#i-' + cfg.i + '"/></svg></div></div>';
    if (interactive) el.firstChild.addEventListener("click", function (ev) { ev.stopPropagation(); popIcon(el, cfg.c); });
    return el;
  }

  var SPARK_COLORS = ["#66eac2", "#ff8a65", "#ffc94d", "#5cc8ff", "#b79cff", "#ff7eb3"];
  function popIcon(el, color) {
    if (el.classList.contains("popped")) return;
    el.classList.remove("respawn");
    el.classList.add("popped");
    if (!reduced) {
      var n = 12, size = el.offsetWidth;
      for (var k = 0; k < n; k++) {
        var sp = document.createElement("span");
        var ang = (Math.PI * 2 * k) / n + Math.random() * 0.3;
        var dist = size * 0.55 + 18 + Math.random() * 30;
        sp.className = "spark c-" + color;
        if (k % 3 === 0) sp.style.background = SPARK_COLORS[k % SPARK_COLORS.length];
        sp.style.setProperty("--dx", (Math.cos(ang) * dist).toFixed(1) + "px");
        sp.style.setProperty("--dy", (Math.sin(ang) * dist).toFixed(1) + "px");
        el.appendChild(sp);
      }
      setTimeout(function () { $$(".spark", el).forEach(function (x) { x.remove(); }); }, 700);
    }
    /* respawn 类保留到下次冒泡：移除它会让入场动画重新播放 */
    setTimeout(function () { el.classList.remove("popped"); el.classList.add("respawn"); }, 1600);
  }

  /* 三层视差：鼠标（平滑插值）+ 滚动（远景慢于页面、前景快于页面）。
   * 只写三个层容器的 transform，并且只在 Hero 可见且有变化时运行 rAF */
  var hiLayers = [];
  var ptr = { tx: 0, ty: 0, x: 0, y: 0 };
  var hiRaf = 0;
  function hiFrame() {
    hiRaf = 0;
    ptr.x += (ptr.tx - ptr.x) * 0.08;
    ptr.y += (ptr.ty - ptr.y) * 0.08;
    var y = window.scrollY || document.documentElement.scrollTop;
    hiLayers.forEach(function (l) {
      l.el.style.transform = "translate3d(" + (ptr.x * l.px).toFixed(2) + "px," + (ptr.y * l.px + y * l.sy).toFixed(2) + "px,0)";
    });
    if (Math.abs(ptr.tx - ptr.x) > 0.0005 || Math.abs(ptr.ty - ptr.y) > 0.0005) hiRaf = requestAnimationFrame(hiFrame);
  }
  function hiKick() {
    if (!hiRaf && !reduced && heroVisible && hiLayers.length) hiRaf = requestAnimationFrame(hiFrame);
  }

  function initHeroIcons() {
    var deep = $("#hiDeep"), mid = $("#hiMid"), front = $("#hiFront"), near = $("#hiNear");
    var n = 0;
    if (deep) HI_DEEP.forEach(function (c) { deep.appendChild(makeIcon(c, n++, false, false)); });
    if (mid) HI_MID.forEach(function (c) { mid.appendChild(makeIcon(c, n++, false, false)); });
    if (front) HI_FRONT.forEach(function (c, i) { front.appendChild(makeIcon(c, i, true, false)); });
    if (near) HI_NEAR.forEach(function (c, i) { near.appendChild(makeIcon(c, i, true, true)); });
    if (deep) hiLayers.push({ el: deep, px: 14, sy: 0.22 });
    if (mid) hiLayers.push({ el: mid, px: 32, sy: 0.08 });
    if (front) hiLayers.push({ el: front, px: 58, sy: -0.14 });

    if (!reduced && finePointer) {
      window.addEventListener("pointermove", function (e) {
        if (!heroVisible) return;
        ptr.tx = e.clientX / window.innerWidth - 0.5;
        ptr.ty = e.clientY / window.innerHeight - 0.5;
        hiKick();
      }, { passive: true });
      document.documentElement.addEventListener("pointerleave", function () { ptr.tx = 0; ptr.ty = 0; hiKick(); });
    }

    /* 点 Logo：弹一下，再发出一圈回音 */
    var logo = $("#heroLogo");
    if (logo) logo.addEventListener("click", function () {
      restartClass(logo, "boing");
      if (reduced) return;
      var e = document.createElement("span");
      e.className = "echo extra";
      logo.insertBefore(e, logo.firstChild);
      setTimeout(function () { e.remove(); }, 1500);
    });
  }

  /* ======================= Hero：关键词轮换 ======================= */
  var WORDS = {
    zh: ["热爱", "兴趣", "灵感", "好奇心", "脑洞"],
    en: ["passion", "hobby", "spark", "curiosity", "wild idea"]
  };
  var WORD_COLORS = [
    ["#66eac2", "102, 234, 194"], ["#ff8a65", "255, 138, 101"], ["#ffc94d", "255, 201, 77"],
    ["#5cc8ff", "92, 200, 255"], ["#b79cff", "183, 156, 255"]
  ];
  function initRotator() {
    var rot = $("#rotator");
    if (!rot) return;
    var idx = 0, timer = 0;
    function build(word, cls) {
      var w = document.createElement("span");
      w.className = "rot-word" + (cls ? " " + cls : "");
      Array.prototype.forEach.call(word, function (ch, i) {
        var c = document.createElement("span");
        c.className = "ch";
        c.style.setProperty("--i", i);
        c.textContent = ch;
        w.appendChild(c);
      });
      return w;
    }
    function paint(i) {
      rot.style.setProperty("--wc", WORD_COLORS[i % WORD_COLORS.length][0]);
      rot.style.setProperty("--wrgb", WORD_COLORS[i % WORD_COLORS.length][1]);
    }
    function render(animated) {
      var word = WORDS[LANG][idx];
      var old = $$(".rot-word", rot);
      var w = build(word, animated ? "in" : "");
      paint(idx);
      if (!animated) {
        rot.innerHTML = "";
        rot.appendChild(w);
        if (rot.classList.contains("live")) rot.style.width = w.offsetWidth + "px";
        return;
      }
      old.forEach(function (o) {
        o.classList.remove("in");
        o.classList.add("out");
        setTimeout(function () { o.remove(); }, 520);
      });
      rot.appendChild(w);
      rot.style.width = w.offsetWidth + "px";
      restartClass(rot, "swap");
    }
    function tick() {
      idx = (idx + 1) % WORDS[LANG].length;
      render(true);
    }
    function start() { if (!timer && !reduced) timer = setInterval(tick, 2600); }
    function stop() { clearInterval(timer); timer = 0; }

    if (!reduced) rot.classList.add("live");
    render(false);
    langListeners.push(function () { idx = 0; render(false); });
    if (reduced) return;
    start();
    document.addEventListener("visibilitychange", function () { if (document.hidden) stop(); else if (heroVisible) start(); });
    if (hasIO && hero) {
      new IntersectionObserver(function (es) {
        heroVisible = es[0].isIntersecting;
        hero.classList.toggle("is-off", !heroVisible);
        if (heroVisible) { start(); hiKick(); } else stop();
      }).observe(hero);
    }
  }

  /* ======================= 跑马灯 ======================= */
  function initMarquee() {
    $$(".marquee-track").forEach(function (track) {
      Array.prototype.slice.call(track.children).forEach(function (li) {
        var c = li.cloneNode(true);
        c.setAttribute("aria-hidden", "true");
        track.appendChild(c);
      });
      var half = track.scrollWidth / 2;
      track.style.setProperty("--dur", Math.max(28, half / 38).toFixed(1) + "s");
    });
    if (hasIO && !reduced) {
      var io = new IntersectionObserver(function (es) {
        es.forEach(function (e) { e.target.classList.toggle("is-off", !e.isIntersecting); });
      });
      $$(".marquee").forEach(function (m) { io.observe(m); });
    }
  }

  /* ======================= 理念翻转卡：触屏点按翻面 ======================= */
  function initIdeas() {
    $$(".idea").forEach(function (card) {
      card.addEventListener("click", function (e) {
        if (e.pointerType === "mouse" && finePointer) return;
        card.classList.toggle("flipped");
      });
    });
  }

  /* ======================= 演示调度 ======================= */
  /* demo: { el, start(), stop() }；stop() 负责清理计时器并复原为完整静态状态 */
  var demos = [];
  function makeDemo(el, start, stop) {
    var d = { el: el, running: false, timers: [] };
    d.later = function (fn, ms) { d.timers.push(setTimeout(fn, ms)); };
    d.clear = function () { d.timers.forEach(clearTimeout); d.timers = []; };
    d.start = function () { d.clear(); start(d); };
    d.stop = function () { d.clear(); stop(d); };
    demos.push(d);
    stop(d);
    if (!reduced && hasIO && el) {
      new IntersectionObserver(function (es) {
        es.forEach(function (e) {
          if (e.isIntersecting && !d.running) { d.running = true; d.start(); }
          else if (!e.isIntersecting && d.running) { d.running = false; d.stop(); }
        });
      }, { threshold: 0.35 }).observe(el);
    }
    return d;
  }
  /* 逐字打出文本，完成后回调 */
  /* ghost=true 时未打出的部分以隐藏文字占位，元素高度从一开始就是最终高度 */
  function typeText(d, el, text, speed, done, ghost) {
    var chars = Array.from(text), i = 0, shown = el, rest = null;
    el.textContent = "";
    if (ghost) {
      shown = document.createElement("span");
      rest = document.createElement("span");
      rest.className = "tw-rest";
      rest.textContent = text;
      el.appendChild(shown);
      el.appendChild(rest);
    }
    (function step() {
      if (i >= chars.length) { if (done) done(); return; }
      shown.textContent += chars[i++];
      if (rest) rest.textContent = chars.slice(i).join("");
      d.later(step, speed);
    })();
  }
  function charSpeed(base) { return LANG === "zh" ? base : Math.round(base * 0.45); }
  langListeners.push(function () {
    demos.forEach(function (d) { d.stop(); if (d.running) d.start(); });
  });

  /* ---------- 一键加入圈子（示意） ---------- */
  function initJoin() {
    var btn = $("#ccJoin");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var on = btn.getAttribute("aria-pressed") !== "true";
      btn.setAttribute("aria-pressed", String(on));
      var label = btn.querySelector("[data-i18n]");
      label.setAttribute("data-i18n", on ? "circles.joined" : "circles.join");
      label.innerHTML = t(on ? "circles.joined" : "circles.join");
    });
  }

  /* ---------- 审核队列 ---------- */
  function initModeration() {
    var panel = $("#modPanel"), list = $("#modList"), countEl = $("#modCount"), done = $("#modDone");
    if (!panel) return;
    var items = $$(".mod-item", list);
    var userTook = false, busy = false, demo;
    /* resolve() 的飞卡计时器单独存放：用户点击时 demo.clear() 不能清掉它们，但 stop() 必须清掉 */
    var pending = [];
    function wait(fn, ms) { pending.push(setTimeout(fn, ms)); }
    done.setAttribute("tabindex", "-1");

    function remaining() { return items.filter(function (it) { return !it.classList.contains("fly-right") && !it.classList.contains("fly-left"); }); }
    function setCount(n, bump) {
      countEl.textContent = n;
      if (bump && !reduced) restartClass(countEl, "bump");
    }
    function resolve(item, act, viaUser) {
      if (item.classList.contains("fly-right") || item.classList.contains("fly-left")) return;
      var btn = $(".mbtn." + act, item);
      var hadFocus = item.contains(document.activeElement);
      busy = true;
      btn.classList.add("press");
      wait(function () {
        btn.classList.remove("press");
        item.classList.add(act === "approve" ? "fly-right" : "fly-left");
        setCount(remaining().length, true);
        wait(function () {
          var left = remaining();
          /* FLIP：记录剩余卡片位置 → 移除飞走的卡片 → 用 transform 从旧位置滑到新位置（不做布局动画） */
          var before = left.map(function (it) { return it.offsetTop; });
          item.hidden = true;
          if (!reduced && left.length && left[0].animate) {
            left.forEach(function (it, i) {
              var dy = before[i] - it.offsetTop;
              if (dy) it.animate([{ transform: "translateY(" + dy + "px)" }, { transform: "none" }], { duration: 420, easing: "cubic-bezier(0.34, 1.56, 0.64, 1)" });
            });
          }
          busy = false;
          if (!left.length) {
            done.hidden = false;
            if (hadFocus) done.focus();
            if (demo.running || viaUser) demo.later(reset, 2800);
          } else if (hadFocus) {
            $(".mbtn.approve", left[0]).focus();
          }
        }, reduced ? 10 : 480);
      }, reduced ? 0 : 240);
    }
    function reset() {
      done.hidden = true;
      items.forEach(function (it, i) {
        it.hidden = false;
        it.classList.remove("fly-right", "fly-left");
        if (!reduced) {
          it.style.setProperty("--d", (i * 0.08) + "s");
          restartClass(it, "enter");
        }
      });
      setCount(items.length, false);
      if (demo.running && !userTook) demo.later(auto, 1400);
    }
    function auto() {
      if (userTook) return;
      var left = remaining();
      if (!left.length) return;
      if (!busy) resolve(left[0], left[0].getAttribute("data-act"), false);
      demo.later(auto, 1500);
    }
    list.addEventListener("click", function (e) {
      var btn = e.target.closest(".mbtn");
      if (!btn) return;
      userTook = true;
      demo.clear();
      resolve(btn.closest(".mod-item"), btn.classList.contains("approve") ? "approve" : "reject", true);
    });
    demo = makeDemo(panel, function (d) {
      userTook = false;
      /* 锁定面板高度：队列清空时不让左右两栏跟着跳动 */
      panel.style.minHeight = "";
      panel.style.minHeight = panel.offsetHeight + "px";
      reset();
      d.later(auto, 900);
    }, function () {
      pending.forEach(clearTimeout);
      pending = [];
      busy = false;
      panel.style.minHeight = "";
      $$(".mbtn.press", list).forEach(function (b) { b.classList.remove("press"); });
      done.hidden = true;
      items.forEach(function (it) { it.hidden = false; it.classList.remove("fly-right", "fly-left", "enter"); });
      setCount(items.length, false);
    });
  }

  /* ---------- 编辑器 → 发布 → 帖子 ---------- */
  function initCreate() {
    var editor = $("#editor"), post = $("#post");
    if (!editor || !post) return;
    var edSpan = $("#edText [data-i18n]"), publish = $("#edPublish");
    var likeBtn = $("#likeBtn"), likeNum = $("#likeNum"), favBtn = $("#favBtn");
    var floors = $$(".floor", post);
    var FINAL = 26, base = FINAL, liked = false;
    function showLikes(bump) {
      likeNum.textContent = base + (liked ? 1 : 0);
      if (bump && !reduced) restartClass(likeNum, "bump");
    }
    likeBtn.addEventListener("click", function () {
      liked = !liked;
      likeBtn.setAttribute("aria-pressed", String(liked));
      showLikes(true);
      if (!reduced) restartClass(likeBtn, "burst");
    });
    favBtn.addEventListener("click", function () {
      var on = favBtn.getAttribute("aria-pressed") !== "true";
      favBtn.setAttribute("aria-pressed", String(on));
      if (!reduced) restartClass(favBtn, "burst");
    });
    makeDemo(editor, function (d) {
      editor.classList.add("playing");
      floors.forEach(function (f) { f.classList.add("pre-hidden"); f.classList.remove("enter"); });
      base = 18;
      showLikes(false);
      typeText(d, edSpan, plain(t("ed.md")), charSpeed(55), function () {
        d.later(function () { publish.classList.add("press"); }, 300);
        d.later(function () {
          publish.classList.remove("press");
          restartClass(post, "enter");
          (function tickLike() {
            if (base >= FINAL) {
              floors.forEach(function (f, i) {
                d.later(function () { f.classList.remove("pre-hidden"); restartClass(f, "enter"); }, 300 + i * 700);
              });
              return;
            }
            base++;
            showLikes(true);
            d.later(tickLike, 260);
          })();
        }, 650);
      });
    }, function () {
      editor.classList.remove("playing");
      publish.classList.remove("press");
      post.classList.remove("enter");
      edSpan.innerHTML = t("ed.md");
      base = FINAL;
      showLikes(false);
      floors.forEach(function (f) { f.classList.remove("pre-hidden"); });
    });
  }

  /* ---------- 推荐 / 热门 / 发现 标签页（ARIA tabs） ---------- */
  function initFeed() {
    var feed = $("#feed");
    if (!feed) return;
    var tabs = $$('[role="tab"]', feed);
    var cur = 0, userTook = false, hover = false;
    function select(i, focus) {
      cur = (i + tabs.length) % tabs.length;
      tabs.forEach(function (tab, j) {
        var on = j === cur;
        tab.setAttribute("aria-selected", String(on));
        tab.tabIndex = on ? 0 : -1;
        var panel = document.getElementById(tab.getAttribute("aria-controls"));
        panel.hidden = !on;
        if (on && !reduced) restartClass(panel, "enter");
      });
      if (focus) tabs[cur].focus();
    }
    tabs.forEach(function (tab, i) {
      tab.addEventListener("click", function () { userTook = true; select(i, false); });
    });
    $('[role="tablist"]', feed).addEventListener("keydown", function (e) {
      var map = { ArrowRight: cur + 1, ArrowLeft: cur - 1, Home: 0, End: tabs.length - 1 };
      if (!(e.key in map)) return;
      e.preventDefault();
      userTook = true;
      select(map[e.key], true);
    });
    feed.addEventListener("mouseenter", function () { hover = true; });
    feed.addEventListener("mouseleave", function () { hover = false; });
    feed.addEventListener("focusin", function () { hover = true; });
    feed.addEventListener("focusout", function () { hover = false; });
    makeDemo(feed, function (d) {
      (function next() {
        d.later(function () {
          if (!userTook && !hover) select(cur + 1, false);
          if (!userTook) next();
        }, 4200);
      })();
    }, function () { userTook = false; });
  }

  /* ---------- 搜索 ---------- */
  function initSearch() {
    var box = $("#search"), q = $("#searchQ"), res = $("#searchRes");
    if (!box) return;
    makeDemo(box, function (d) {
      res.classList.add("hide");
      q.textContent = "";
      d.later(function () {
        typeText(d, q, t("search.q"), LANG === "zh" ? 320 : 130, function () {
          d.later(function () {
            Array.prototype.forEach.call(res.children, function (c, i) { c.style.transitionDelay = (i * 0.06) + "s"; });
            res.classList.remove("hide");
          }, 250);
        });
      }, 500);
    }, function () {
      q.innerHTML = t("search.q");
      res.classList.remove("hide");
      Array.prototype.forEach.call(res.children, function (c) { c.style.transitionDelay = ""; });
    });
  }

  /* ---------- @小Q 对话 ---------- */
  function initChat() {
    var chat = $("#chat"), qEl = $("#chatQ"), aEl = $("#chatA"), typing = $("#chatTyping"), text = $("#chatText");
    if (!chat) return;
    makeDemo(chat, function (d) {
      qEl.classList.add("pre-hidden");
      aEl.classList.add("pre-hidden");
      typing.classList.remove("on");
      typing.style.top = aEl.offsetTop + "px";
      d.later(function () { qEl.classList.remove("pre-hidden"); restartClass(qEl, "show-in"); }, 300);
      d.later(function () { typing.classList.add("on"); }, 1100);
      d.later(function () {
        typing.classList.remove("on");
        aEl.classList.remove("pre-hidden");
        restartClass(aEl, "show-in");
        typeText(d, text, t("chat.a"), charSpeed(45), null, true);
      }, 2600);
    }, function () {
      qEl.classList.remove("pre-hidden", "show-in");
      aEl.classList.remove("pre-hidden", "show-in");
      typing.classList.remove("on");
      text.innerHTML = t("chat.a");
    });
  }

  /* ---------- 通知：电脑与手机同时收到 ---------- */
  function initNotify() {
    var wrap = $("#devices"), notes = $$("#notes .note"), badge = $("#bellBadge"), toasts = $("#toasts");
    if (!wrap) return;
    var bell = badge.parentNode;
    function toast(note, animate) {
      var el = document.createElement("div");
      el.className = "toast";
      if (!animate) el.style.animation = "none";
      el.appendChild(note.querySelector(".n-ic").cloneNode(true));
      var p = document.createElement("p");
      p.innerHTML = note.querySelector("p").innerHTML;
      el.appendChild(p);
      toasts.insertBefore(el, toasts.firstChild);
      while (toasts.children.length > 3) toasts.lastChild.remove();
    }
    function setBadge(n, bump) {
      badge.textContent = n;
      if (bump) { restartClass(badge, "bump"); restartClass(bell, "ring"); }
    }
    makeDemo(wrap, function (d) {
      toasts.innerHTML = "";
      notes.forEach(function (n) { n.classList.add("hide"); n.classList.remove("fresh"); });
      setBadge(0, false);
      notes.forEach(function (n, i) {
        d.later(function () {
          notes.forEach(function (m) { m.classList.remove("fresh"); });
          n.classList.remove("hide");
          n.classList.add("fresh");
          setBadge(i + 1, true);
          toast(n, true);
        }, 700 + i * 1300);
      });
    }, function () {
      toasts.innerHTML = "";
      notes.forEach(function (n) { n.classList.remove("hide", "fresh"); });
      notes.slice(-3).forEach(function (n) { toast(n, false); });
      setBadge(notes.length, false);
    });
  }

  /* ======================= 启动 ======================= */
  initMarquee();
  initHeroIcons();
  applyLang(readLangPref());
  initRotator();
  initReveal();
  initIdeas();
  initJoin();
  initModeration();
  initCreate();
  initFeed();
  initSearch();
  initChat();
  initNotify();
  onScrollFrame();

  var langToggle = $("#langToggle");
  if (langToggle) langToggle.addEventListener("click", function () {
    var next = LANG === "zh" ? "en" : "zh";
    storeLangPref(next);
    applyLang(next);
  });
})();
