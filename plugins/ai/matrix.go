// 文件名: plugins/ai/matrix.go
// 主要功能: AI 权限矩阵安全网关、30s-60s 拟人态排队防风控执行器与频控熔断
// 作者: 2guan

package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// HumanTask 拟人化执行任务结构
type HumanTask struct {
	Name       string
	Execute    func() error
	MinWaitSec int
	MaxWaitSec int
}

// MatrixExecutor 拟人态执行引擎与安全防护网
type MatrixExecutor struct {
	mu           sync.RWMutex
	cfg          MatrixConfig
	patCooldowns map[string]time.Time // 用户ID -> 上次拍一拍时间
	addFriendCnt int                  // 今日加好友计数
	lastResetDay int                  // 上次重置加好友计数的日期
	taskQueue    chan HumanTask
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewMatrixExecutor 实例化执行器并启动排队后台协程
func NewMatrixExecutor(cfg MatrixConfig) *MatrixExecutor {
	ctx, cancel := context.WithCancel(context.Background())
	exec := &MatrixExecutor{
		cfg:          cfg,
		patCooldowns: make(map[string]time.Time),
		addFriendCnt: 0,
		lastResetDay: time.Now().Day(),
		taskQueue:    make(chan HumanTask, 200),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 启动后台拟人化串行工作协程（每个操作强制等待 30s~60s）
	go exec.workerLoop()

	return exec
}

// Close 停止执行器
func (e *MatrixExecutor) Close() {
	e.cancel()
}

// UpdateConfig 动态更新矩阵配置
func (e *MatrixExecutor) UpdateConfig(cfg MatrixConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

// CheckPatAllowed 检查是否允许对目标执行拍一拍（频控与开关）
func (e *MatrixExecutor) CheckPatAllowed(targetID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.cfg.Pat.Enabled {
		return false
	}

	cooldownSec := e.cfg.Pat.CooldownSec
	if cooldownSec <= 0 {
		cooldownSec = 300
	}

	lastTime, exists := e.patCooldowns[targetID]
	if exists && time.Since(lastTime) < time.Duration(cooldownSec)*time.Second {
		slog.Debug("[matrix] 拍一拍操作被冷却拦截", "target", targetID, "remaining_sec", int((time.Duration(cooldownSec)*time.Second - time.Since(lastTime)).Seconds()))
		return false
	}

	e.patCooldowns[targetID] = time.Now()
	return true
}

// CheckAddFriendAllowed 检查是否允许发起加好友（日限额检查）
func (e *MatrixExecutor) CheckAddFriendAllowed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.cfg.AddFriend.Enabled {
		return false
	}

	// 检查日期重置
	curDay := time.Now().Day()
	if curDay != e.lastResetDay {
		e.addFriendCnt = 0
		e.lastResetDay = curDay
	}

	limit := e.cfg.AddFriend.DailyLimit
	if limit <= 0 {
		limit = 10
	}

	if e.addFriendCnt >= limit {
		slog.Warn("[matrix] 今日加好友次数已达上限，熔断保护", "count", e.addFriendCnt, "limit", limit)
		return false
	}

	e.addFriendCnt++
	return true
}

// EnqueueHumanTask 将非实时强交互操作排入拟人等待队列（严格执行 30s~60s 等待）
func (e *MatrixExecutor) EnqueueHumanTask(name string, minWait, maxWait int, fn func() error) error {
	if minWait <= 0 {
		minWait = 30
	}
	if maxWait < minWait {
		maxWait = minWait + 30
	}

	task := HumanTask{
		Name:       name,
		Execute:    fn,
		MinWaitSec: minWait,
		MaxWaitSec: maxWait,
	}

	select {
	case e.taskQueue <- task:
		slog.Info("[matrix] 任务已排入拟人防风控队列", "task", name, "min_wait", minWait, "max_wait", maxWait)
		return nil
	default:
		return fmt.Errorf("拟人任务队列已满，放弃提交: %s", name)
	}
}

// SimulateTyping 模拟真人打字生理延时（1.5s ~ 4.5s）
func (e *MatrixExecutor) SimulateTyping(textLen int) {
	e.mu.RLock()
	sim := e.cfg.TypingSimulate
	e.mu.RUnlock()

	if !sim.Enabled {
		return
	}

	minMs := sim.MinMs
	if minMs <= 0 {
		minMs = 1500
	}
	maxMs := sim.MaxMs
	if maxMs < minMs {
		maxMs = minMs + 3000
	}

	// 根据字数按比例增加轻微打字时长，并叠加随机抖动
	charBonus := textLen * 15 // 每字加 15ms
	if charBonus > 1500 {
		charBonus = 1500
	}

	jitter := rand.Intn(maxMs - minMs + 1)
	sleepDuration := time.Duration(minMs+jitter+charBonus) * time.Millisecond
	if sleepDuration > 5*time.Second {
		sleepDuration = 5 * time.Second
	}

	time.Sleep(sleepDuration)
}

// workerLoop 拟人态排队串行工作循环
func (e *MatrixExecutor) workerLoop() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		select {
		case <-e.ctx.Done():
			return
		case task := <-e.taskQueue:
			// 严格执行 30s ~ 60s 随机拟人等待，高仿真真人操作间隔
			waitRange := task.MaxWaitSec - task.MinWaitSec + 1
			waitSec := task.MinWaitSec + r.Intn(waitRange)
			slog.Info("[matrix] 拟人排队中，休眠等待中...",
				"task", task.Name,
				"wait_sec", waitSec)

			select {
			case <-e.ctx.Done():
				return
			case <-time.After(time.Duration(waitSec) * time.Second):
			}

			// 执行底层操作
			if err := task.Execute(); err != nil {
				slog.Error("[matrix] 执行拟人任务失败", "task", task.Name, "err", err)
			} else {
				slog.Info("[matrix] 拟人任务执行成功", "task", task.Name)
			}
		}
	}
}
