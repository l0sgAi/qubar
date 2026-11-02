// auto_post.go 全局机器人在 hot 圈自动发帖（#101）。
//
// 定时器每轮调用 RunAutoPostScan：逐个加载开启 auto_post 的启用全局机器人 → 活跃时段 / interval_min /
// daily_max 判定到期 → 按 topics 轮换取一个关键词调 HotTopicSource.Search → 按话题键去掉已发过的结果 →
// 先插占位行（撞唯一约束 (agent_id, topic_key) 即跳过）→ LLM 只用给定材料生成 JSON 草稿 →
// 校验 used_sources 是输入子集 → 系统追加来源链接与 AI 生成标识 → 经 post 内部发帖接口落库 → 终态写日志。
// 搜索结果为空 / 去重后为空时不调 LLM、不写日志。机器人账号（role=2）发的帖不进自动回复候选（post 侧过滤）。
package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/util/crypto"

	"github.com/google/uuid"
)

// AutoPostInput 机器人自动发帖入参（同构 post.CreatePostInput 的子集）。
type AutoPostInput struct {
	CircleID uuid.UUID
	Title    string
	Content  string
}

// AutoPostCreator 内部发帖端口（桥接 post 域 CreateAgentPost：跳过成员检查、不解析 @提及，
// 其余清洗 / 计数 / 审核副作用与普通发帖一致）。
type AutoPostCreator interface {
	CreateAgentPost(ctx context.Context, userID uuid.UUID, input AutoPostInput) (uuid.UUID, error)
}

// AutoPostService 机器人自动发帖服务（定时器调用）。
type AutoPostService interface {
	// RunAutoPostScan 执行一轮自动发帖（同步，逐个机器人顺序执行，每个机器人每轮最多发一帖）。
	// 平台总开关关闭或未配置 hot_circle_id 直接返回；任何失败静默（日志表 + zap）。
	RunAutoPostScan(ctx context.Context)
	// SetPostCreator 注入内部发帖端口（未注入则不执行）。
	SetPostCreator(c AutoPostCreator)
}

const (
	autoPostMaxSources    = 5     // 每帖最多提供给 LLM 的材料条数
	autoPostDedupeDays    = 7     // 来源去重回看天数（搜索时效默认 24h，更早的结果基本不会再出现；首条来源另有唯一约束兜底）
	autoPostMaxTitleChars = 100   // 标题截断（post.title varchar(200)）
	autoPostMaxBodyChars  = 10000 // LLM 正文截断（不含系统追加的来源段）
	autoPostAIDisclosure  = "本帖由 AI 根据网络公开信息生成"
	autoPostInstruction   = "现在你要在社区里发一篇热点资讯帖。只能使用用户给出的编号材料中的信息，" +
		"禁止编造材料中没有的事实、数字、引语或链接；围绕第 1 条材料的话题写作，其他材料只在与该话题直接相关时引用。" +
		"正文用 Markdown，不要附来源链接（系统会追加）。只输出 JSON：" +
		`{"title":"不超过 50 字的标题","content":"正文","used_sources":[实际引用的材料编号]}，禁止输出任何其他内容。`
)

// autoPostZone 活跃时段与 daily_max 自然日的时区（同搜索配额，固定 UTC+8）。
var autoPostZone = time.FixedZone("CST", 8*3600)

type autoPostServiceImpl struct {
	agentRepo domain.AgentRepository
	logRepo   domain.PostLogRepository
	llm       LLMCaller
	search    domain.HotTopicSource
	creator   AutoPostCreator
	now       func() time.Time

	mu          sync.Mutex
	topicCursor map[uuid.UUID]int // 每个机器人下一轮使用的关键词下标（进程内轮换）
}

// NewAutoPostService 构造 AutoPostService（creator 为跨域依赖，setter 注入）。
func NewAutoPostService(
	agentRepo domain.AgentRepository,
	logRepo domain.PostLogRepository,
	llm LLMCaller,
	search domain.HotTopicSource,
) AutoPostService {
	return &autoPostServiceImpl{
		agentRepo: agentRepo, logRepo: logRepo, llm: llm, search: search,
		now: time.Now, topicCursor: make(map[uuid.UUID]int),
	}
}

func (s *autoPostServiceImpl) SetPostCreator(c AutoPostCreator) { s.creator = c }

func (s *autoPostServiceImpl) RunAutoPostScan(ctx context.Context) {
	ac := autonomyConfig()
	if !ac.Enabled || s.creator == nil || strings.TrimSpace(ac.HotCircleID) == "" {
		return
	}
	circleID, err := uuid.Parse(strings.TrimSpace(ac.HotCircleID))
	if err != nil || circleID == uuid.Nil {
		logger.Log.Warn("agent auto post: invalid aiagent.autonomy.hot_circle_id, skipped")
		return
	}
	agents, err := s.agentRepo.ListAutoPostEnabled(ctx)
	if err != nil {
		logger.Log.Error("agent auto post: list agents failed: " + err.Error())
		return
	}
	for i := range agents {
		if ctx.Err() != nil {
			return
		}
		s.runAgent(ctx, &agents[i], circleID, ac.DefaultHotTopics)
	}
}

// runAgent 单个机器人的一轮：到期判定 → 搜索 → 去重 → 发帖。
func (s *autoPostServiceImpl) runAgent(ctx context.Context, agent *domain.AiAgent, circleID uuid.UUID, defaults []string) {
	cfg, err := agent.AutoConfig.AutoPost()
	if err != nil {
		logger.Log.Warn(fmt.Sprintf("agent auto post: agent=%s invalid auto_config ignored: %s", agent.ID, err.Error()))
		return
	}
	if !cfg.Enabled || cfg.RequireReview || agent.CircleID != nil {
		return
	}
	now := s.now().In(autoPostZone)
	if !cfg.ActiveAt(now.Hour()) {
		return
	}
	if due, err := s.isDue(ctx, agent.ID, cfg, now); err != nil || !due {
		if err != nil {
			logger.Log.Error(fmt.Sprintf("agent auto post: agent=%s rate check failed: %s", agent.ID, err.Error()))
		}
		return
	}
	topics := cfg.Topics
	if len(topics) == 0 {
		topics = defaults
	}
	if len(topics) == 0 {
		return
	}
	query := s.nextTopic(agent.ID, topics)
	items, err := s.search.Search(ctx, query, domain.SearchOptions{AgentID: agent.ID})
	if errors.Is(err, domain.ErrSearchDisabled) || errors.Is(err, domain.ErrSearchQuotaExhausted) {
		logger.Log.Info(fmt.Sprintf("agent auto post skip: agent=%s reason=%s", agent.ID, err.Error()))
		return
	}
	if err != nil {
		logger.Log.Warn(fmt.Sprintf("agent auto post: agent=%s search %q failed: %s", agent.ID, query, err.Error()))
		return
	}
	items, keys, err := s.freshItems(ctx, agent.ID, items, now)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("agent auto post: agent=%s dedupe failed: %s", agent.ID, err.Error()))
		return
	}
	if len(items) == 0 {
		logger.Log.Info(fmt.Sprintf("agent auto post skip: agent=%s query=%q reason=no_fresh_topic", agent.ID, query))
		return
	}
	if err := s.publish(ctx, agent, circleID, query, items, keys[0]); err != nil {
		logger.Log.Error(fmt.Sprintf("agent auto post: agent=%s query=%q: %s", agent.ID, query, err.Error()))
	}
}

// isDue daily_max（当日尝试次数）与 interval_min（距上次尝试）判定。
func (s *autoPostServiceImpl) isDue(ctx context.Context, agentID uuid.UUID, cfg domain.AutoPostConfig, now time.Time) (bool, error) {
	y, m, d := now.Date()
	n, err := s.logRepo.CountSinceByAgent(ctx, agentID, time.Date(y, m, d, 0, 0, 0, 0, autoPostZone))
	if err != nil || n >= int64(cfg.DailyMax) {
		return false, err
	}
	last, err := s.logRepo.GetLastTimeByAgent(ctx, agentID)
	if err != nil {
		return false, err
	}
	return last == nil || !last.Add(time.Duration(cfg.IntervalMin)*time.Minute).After(now), nil
}

// nextTopic 按机器人轮换关键词（一轮一个，避免一次扣多次搜索配额）。
func (s *autoPostServiceImpl) nextTopic(agentID uuid.UUID, topics []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.topicCursor[agentID] % len(topics)
	s.topicCursor[agentID] = i + 1
	return topics[i]
}

// freshItems 去掉本批内重复与该机器人近期用过的来源，最多保留 autoPostMaxSources 条（保持搜索排序），
// 同时返回各条的话题键（首条即本帖 topic_key）。
func (s *autoPostServiceImpl) freshItems(ctx context.Context, agentID uuid.UUID, items []domain.TopicItem, now time.Time) ([]domain.TopicItem, []string, error) {
	used, err := s.logRepo.ListSourceURLsSince(ctx, agentID, now.AddDate(0, 0, -autoPostDedupeDays))
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool, len(used)+len(items))
	for _, u := range used {
		seen[topicKey(u)] = true
	}
	var outItems []domain.TopicItem
	var outKeys []string
	for _, it := range items {
		if it.URL == "" || len(outItems) >= autoPostMaxSources {
			continue
		}
		if k := topicKey(it.URL); !seen[k] {
			seen[k] = true
			outItems, outKeys = append(outItems, it), append(outKeys, k)
		}
	}
	return outItems, outKeys, nil
}

// publish 占位 → LLM → 校验 → 发帖 → 终态日志。占位被抢（并发 / 多实例）返回 nil。
func (s *autoPostServiceImpl) publish(ctx context.Context, agent *domain.AiAgent, circleID uuid.UUID, query string, items []domain.TopicItem, key string) error {
	if r := []rune(query); len(r) > 128 { // search_query varchar(128)
		query = string(r[:128])
	}
	log := &domain.PostLog{
		ID: sharedomain.NewID(), AgentID: agent.ID, CircleID: circleID, TopicKey: key,
		Status: domain.PostLogStatusPending, SearchQuery: query, Sources: toPostSources(items),
	}
	ok, err := s.logRepo.CreatePlaceholder(ctx, log)
	if err != nil || !ok {
		return err
	}
	start := time.Now()
	finish := func(status int16, errMsg string) error {
		log.Status, log.ErrorMsg = status, truncateErr(errMsg)
		log.LatencyMs = int(time.Since(start).Milliseconds())
		if err := s.logRepo.Finish(context.WithoutCancel(ctx), log); err != nil {
			logger.Log.Error(fmt.Sprintf("agent auto post: agent=%s finish log failed: %s", agent.ID, err.Error()))
		}
		if status == domain.PostLogStatusFailed {
			return errors.New(errMsg)
		}
		return nil
	}

	apiKey := ""
	if agent.APIKeyEnc != "" {
		if apiKey, err = crypto.Decrypt(conf.Config.Security.DataKey, agent.APIKeyEnc); err != nil {
			return finish(domain.PostLogStatusFailed, "decrypt api_key failed: "+err.Error())
		}
	}
	systemPrompt := agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	genCtx, cancel := context.WithTimeout(ctx, replyTimeout())
	res, err := s.llm.Generate(genCtx, LLMRequest{
		Protocol: agent.APIProtocol, BaseURL: agent.BaseURL, APIKey: apiKey, Model: agent.Model,
		Params: agent.LLMParams, SystemPrompt: systemPrompt + "\n\n" + autoPostInstruction,
		UserPrompt: buildAutoPostUserPrompt(items),
	})
	cancel()
	if err != nil {
		return finish(domain.PostLogStatusFailed, "llm generate failed: "+err.Error())
	}
	log.PromptTokens, log.CompletionTokens = res.PromptTokens, res.CompletionTokens
	draft, used, err := parseAutoPostDraft(res.Content, items)
	if err != nil {
		return finish(domain.PostLogStatusFailed, "invalid llm output: "+err.Error())
	}
	postID, err := s.creator.CreateAgentPost(ctx, agent.LinkedUserID, AutoPostInput{
		CircleID: circleID,
		Title:    truncateRunes(draft.Title, autoPostMaxTitleChars),
		Content:  appendAttribution(truncateRunes(draft.Content, autoPostMaxBodyChars), used),
	})
	if err != nil {
		return finish(domain.PostLogStatusFailed, "create post failed: "+err.Error())
	}
	log.PostID, log.Sources = &postID, toPostSources(used)
	logger.Log.Info(fmt.Sprintf("agent auto post ok: agent=%s post=%s query=%q", agent.ID, postID, query))
	return finish(domain.PostLogStatusOK, "")
}

// autoPostDraft LLM 输出的帖子草稿。
type autoPostDraft struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	UsedSources []int  `json:"used_sources"`
}

// parseAutoPostDraft 解析 LLM 输出（容忍 ```json 代码块包裹），返回草稿与实际引用的材料。
// used_sources 为空或引用了不存在的材料编号即失败（防编造）。
func parseAutoPostDraft(raw string, items []domain.TopicItem) (*autoPostDraft, []domain.TopicItem, error) {
	i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if i < 0 || j < i {
		return nil, nil, errors.New("no json object")
	}
	var d autoPostDraft
	if err := json.Unmarshal([]byte(raw[i:j+1]), &d); err != nil {
		return nil, nil, err
	}
	d.Title = utils.SanitizeForPg(strings.TrimSpace(d.Title))
	d.Content = utils.SanitizeForPg(strings.TrimSpace(d.Content))
	if d.Title == "" || d.Content == "" {
		return nil, nil, errors.New("empty title or content")
	}
	if len(d.UsedSources) == 0 {
		return nil, nil, errors.New("used_sources is empty")
	}
	used := make([]domain.TopicItem, 0, len(d.UsedSources))
	seen := make(map[int]bool, len(d.UsedSources))
	for _, n := range d.UsedSources {
		if n < 1 || n > len(items) {
			return nil, nil, fmt.Errorf("used_sources references unknown material %d", n)
		}
		if !seen[n] {
			seen[n] = true
			used = append(used, items[n-1])
		}
	}
	return &d, used, nil
}

// buildAutoPostUserPrompt 编号材料（只给标题 / 摘要 / 来源站点 / 发布时间，不给 URL，防 LLM 自行附链接）。
func buildAutoPostUserPrompt(items []domain.TopicItem) string {
	var b strings.Builder
	b.WriteString("材料：\n")
	for i, it := range items {
		fmt.Fprintf(&b, "[%d] 标题：%s\n摘要：%s\n来源：%s", i+1, it.Title, truncateRunes(it.Snippet, 1000), it.Source)
		if !it.PublishedAt.IsZero() {
			fmt.Fprintf(&b, "（发布于 %s）", it.PublishedAt.In(autoPostZone).Format("2006-01-02 15:04"))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

var (
	mdTextEscaper = strings.NewReplacer("[", `\[`, "]", `\]`)
	mdURLEscaper  = strings.NewReplacer("(", "%28", ")", "%29", " ", "%20")
)

// appendAttribution 正文末尾由系统追加来源链接与 AI 生成显式标识（《人工智能生成合成内容标识办法》）。
func appendAttribution(content string, used []domain.TopicItem) string {
	var b strings.Builder
	b.WriteString(content)
	b.WriteString("\n\n---\n\n来源：\n")
	for _, it := range used {
		text := strings.TrimSpace(it.Title)
		if text == "" {
			text = it.URL
		}
		fmt.Fprintf(&b, "- [%s](%s)\n", mdTextEscaper.Replace(text), mdURLEscaper.Replace(it.URL))
	}
	b.WriteString("\n" + autoPostAIDisclosure)
	return b.String()
}

// topicKey 话题键 = 规范化 URL 的 SHA-256：scheme / host 小写、去 www.、去 fragment / userinfo /
// utm_* 参数、去路径尾部斜杠、参数排序。解析失败按原串哈希。
func topicKey(raw string) string {
	norm := strings.TrimSpace(raw)
	if u, err := url.Parse(norm); err == nil {
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		u.User, u.Fragment, u.RawFragment, u.RawPath = nil, "", "", ""
		u.Path = strings.TrimRight(u.Path, "/")
		q := u.Query()
		for k := range q {
			if strings.HasPrefix(strings.ToLower(k), "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
		norm = u.String()
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// toPostSources 搜索结果 → 日志来源列表。
func toPostSources(items []domain.TopicItem) domain.PostSourcesJSON {
	out := make(domain.PostSourcesJSON, 0, len(items))
	for _, it := range items {
		src := domain.PostSource{Title: it.Title, URL: it.URL, Source: it.Source}
		if !it.PublishedAt.IsZero() {
			t := it.PublishedAt
			src.PublishedAt = &t
		}
		out = append(out, src)
	}
	return out
}
