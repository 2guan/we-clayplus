// 文件名: plugins/ai/scheduler.go
// 主要功能: 周期性定时批量执行器 (朋友圈巡回互动、好友批量审核、30s-60s 拟人化任务排队)
// 作者: 2guan

package main

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/we-clay/we-clay/plugins/ai/provider"
	"github.com/we-clay/we-clay/sdk/moments"
)

// Scheduler 周期性任务调度器
type Scheduler struct {
	mu           sync.RWMutex
	cfg          *Config
	moments      moments.Ability
	router       *Router
	matrix       *MatrixExecutor
	lastRunDay   map[string]int // 任务名 -> 上次执行日期 (避免同一分钟内重复运行)
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewScheduler 实例化调度器并启动定时轮询协程
func NewScheduler(cfg *Config, mom moments.Ability, router *Router, matrix *MatrixExecutor) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		cfg:        cfg,
		moments:    mom,
		router:     router,
		matrix:     matrix,
		lastRunDay: make(map[string]int),
		ctx:        ctx,
		cancel:     cancel,
	}

	go s.cronLoop()
	return s
}

// Close 停止调度器
func (s *Scheduler) Close() {
	s.cancel()
}

// cronLoop 每 30 秒检查一次是否到达每日固定任务触发点
func (s *Scheduler) cronLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkScheduledTasks()
		}
	}
}

func (s *Scheduler) checkScheduledTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cfg.Matrix.Moments.Enabled {
		return
	}

	hour, min, ok := s.cfg.Matrix.Moments.ParseCronHourMinute()
	if !ok {
		return
	}

	now := time.Now()
	today := now.YearDay()

	// 判断当前时间是否命中设定时刻
	if now.Hour() == hour && now.Minute() == min {
		if s.lastRunDay["moments_patrol"] != today {
			s.lastRunDay["moments_patrol"] = today
			slog.Info("[scheduler] 命中每日朋友圈定时巡回时刻，开始执行批量巡回...", "time", s.cfg.Matrix.Moments.CronSchedule)
			go s.ExecuteMomentsPatrol()
		}
	}
}

// ExecuteMomentsPatrol 执行朋友圈批量互动（获取动态、情感评估、点赞与评论排入 30s~60s 等待队列）
func (s *Scheduler) ExecuteMomentsPatrol() {
	if s.moments == nil {
		slog.Warn("[scheduler] 朋友圈能力未就绪，跳过巡回")
		return
	}

	slog.Info("[scheduler] 正在拉取朋友圈时间线动态...")
	resp, err := s.moments.Timeline("", 0)
	if err != nil {
		slog.Error("[scheduler] 获取朋友圈动态失败", "err", err)
		return
	}
	if resp == nil || len(resp.ObjectList) == 0 {
		slog.Info("[scheduler] 朋友圈时间线暂无新动态")
		return
	}

	scoreRegex := regexp.MustCompile(`\[SCORE:(\d+)\]`)
	opsCount := 0
	maxOps := s.cfg.Matrix.Moments.MaxOpsPerRun
	if maxOps <= 0 {
		maxOps = 15
	}

	minWait := s.cfg.Matrix.Moments.MinWaitSec
	if minWait <= 0 {
		minWait = 30
	}
	maxWait := s.cfg.Matrix.Moments.MaxWaitSec
	if maxWait < minWait {
		maxWait = 60
	}

	for _, post := range resp.ObjectList {
		if opsCount >= maxOps {
			slog.Info("[scheduler] 达到本次朋友圈最大操作上限，巡回结束", "count", opsCount)
			break
		}

		postID := post.Id
		author := post.Username
		content := post.ObjectDescription

		// 白名单过滤
		if len(s.cfg.Matrix.Moments.Whitelist) > 0 {
			matched := false
			for _, w := range s.cfg.Matrix.Moments.Whitelist {
				if w == author {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		if strings.TrimSpace(content) == "" {
			continue
		}

		// 使用轻量大模型对朋友圈内容进行情绪评估与暖心评论起草
		evalPrompt := fmt.Sprintf(`请评估以下好友发布的朋友圈动态内容。如果整体情绪正向、生活化或温和，请输出情绪分数 (0-100) 并生成一条在微信朋友圈中自然、贴心、真实的幽默简短互动评论（15字以内，不要机器人腔调）。
输出格式必须严格为：
[SCORE:分数] 评论内容

朋友圈内容：
"%s"`, content)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		aiResp, err := s.router.Generate(ctx, "", []provider.ChatMessage{
			{Role: provider.RoleUser, Content: evalPrompt},
		}, 0.7, 100)
		cancel()

		if err != nil || aiResp == nil {
			continue
		}

		// 解析分数与评论文本
		score := 70.0
		commentText := aiResp.Content
		if m := scoreRegex.FindStringSubmatch(aiResp.Content); len(m) > 1 {
			if sVal, err := strconv.ParseFloat(m[1], 64); err == nil {
				score = sVal
			}
			commentText = strings.TrimSpace(scoreRegex.ReplaceAllString(aiResp.Content, ""))
		}

		if score < s.cfg.Matrix.Moments.SentimentThreshold {
			slog.Debug("[scheduler] 动态情绪评分低于阈值，跳过互动", "author", author, "score", score)
			continue
		}

		// 1. 排队点赞任务 (强制 30s~60s 拟人等待)
		err = s.matrix.EnqueueHumanTask(fmt.Sprintf("朋友圈点赞-%d", postID), minWait, maxWait, func() error {
			_, lErr := s.moments.Like(postID)
			return lErr
		})
		if err == nil {
			opsCount++
		}

		// 2. 若生成了有效评论，排队评论任务 (同样强制 30s~60s 拟人等待)
		if commentText != "" && opsCount < maxOps {
			targetPostID := postID
			cText := commentText
			err = s.matrix.EnqueueHumanTask(fmt.Sprintf("朋友圈评论-%d", targetPostID), minWait, maxWait, func() error {
				_, cErr := s.moments.Comment(targetPostID, cText, 2, 0)
				return cErr
			})
			if err == nil {
				opsCount++
			}
		}
	}

	slog.Info("[scheduler] 朋友圈批量巡回任务编排完成", "enqueued_ops", opsCount)
}

// TriggerMomentsPatrol 提供手动或 RPC 触发朋友圈巡回
func (s *Scheduler) TriggerMomentsPatrol() {
	go s.ExecuteMomentsPatrol()
}
