package redpanda

import (
	"context"
	"fmt"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
)

// AgentAutoReplyScanner 自动回复扫描执行端口（aiagent.ReplyService 实现，composition 注入）。
// 本包只依赖这一个方法，不 import aiagent 领域包。
type AgentAutoReplyScanner interface {
	RunAutoReplyScan(ctx context.Context)
}

// AgentAutoReplySyncer 机器人自动回复定时扫描器（#99，设计见 docs/agent-auto-reply-design.md）。
//
// 每 aiagent.autonomy.scan_interval_sec 秒执行一轮 RunAutoReplyScan：按热度门槛挑出高热度
// 帖子 / 评论，复用回复执行链路自动回复。与 TrendingRankSyncer 同形：{mu, ticker, stopChan, stopped}。
// 区别：一轮可能包含多次 LLM 调用（秒级~分钟级），关停时**不排干**——取消进行中的一轮并等待退出，
// 而不是再跑一轮。上一轮未结束时 ticker 的到期信号被丢弃（time.Ticker 语义），不会堆积。
//
// 无分布式锁：多实例部署时各实例都会扫描，"每机器人每目标最多一次"由日志表部分唯一索引兜底，
// 每目标机器人数上限为软限（与现有定时任务一致，单机部署无影响）。
type AgentAutoReplySyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
	done     chan struct{}
	cancel   context.CancelFunc
	ctx      context.Context
	scanner  AgentAutoReplyScanner
}

var agentAutoReplySyncer *AgentAutoReplySyncer

// agentAutoReplyStopWait 关停时等待进行中一轮退出的上限（LLM 调用随 ctx 取消立即中断）。
const agentAutoReplyStopWait = 10 * time.Second

// StartAgentAutoReplySyncer 启动自动回复扫描器。平台总开关关闭时不启动（返回 nil）。
func StartAgentAutoReplySyncer(scanner AgentAutoReplyScanner) error {
	ac := conf.Config.AiAgent.Autonomy.WithDefaults()
	if !ac.Enabled {
		logger.Log.Info("Agent auto reply syncer disabled by config (aiagent.autonomy.enabled=false)")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &AgentAutoReplySyncer{
		ticker:   time.NewTicker(time.Duration(ac.ScanIntervalSec) * time.Second),
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
		scanner:  scanner,
	}
	agentAutoReplySyncer = s
	go s.run()
	logger.Log.Info(fmt.Sprintf("Agent auto reply syncer started (interval=%d s)", ac.ScanIntervalSec))
	return nil
}

func (s *AgentAutoReplySyncer) run() {
	defer close(s.done)
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			return
		}
	}
}

// flush 执行一轮扫描（panic 兜底，单轮失败不影响后续轮次）。
// 总开关在每轮重新读取：运行中关闭（Nacos 热更新）后不再扫描。
func (s *AgentAutoReplySyncer) flush() {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error(fmt.Sprintf("agent auto reply scan panic: %v", r))
		}
	}()
	if !conf.Config.AiAgent.Autonomy.Enabled {
		return
	}
	s.scanner.RunAutoReplyScan(s.ctx)
}

// Stop 停止扫描器（幂等）：取消进行中的一轮并等待退出（最多 agentAutoReplyStopWait）。
func (s *AgentAutoReplySyncer) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.cancel()
	close(s.stopChan)
	s.mu.Unlock()

	select {
	case <-s.done:
	case <-time.After(agentAutoReplyStopWait):
		logger.Log.Warn("Agent auto reply syncer did not stop in time, continue shutdown")
	}
}

// StartAgentAutoReplySyncerWithRetry 启动自动回复扫描器（保持与其它 syncer 一致签名）。
func StartAgentAutoReplySyncerWithRetry(scanner AgentAutoReplyScanner) {
	if err := StartAgentAutoReplySyncer(scanner); err != nil {
		logger.Log.Error("Failed to start agent auto reply syncer: " + err.Error())
	}
}

// StopAgentAutoReplySyncer 停止自动回复扫描器（关停时调用，须早于消费者 / Redis / 生产者关闭：
// 进行中的回复会写评论并发事件）。
func StopAgentAutoReplySyncer() {
	if agentAutoReplySyncer != nil {
		agentAutoReplySyncer.Stop()
	}
}
