package app

import (
	"fmt"
	"sync"
	"time"
)

// agentQuotaTracker 每用户每日 Agent 调用配额（内存 + 按日滚动）。
//
// 与限流同一个单实例假设：多实例部署需换共享存储。配额只限制"调用次数"，
// token 成本由用户自带 API Key 承担。
type agentQuotaTracker struct {
	mu     sync.Mutex
	daily  int
	counts map[string]*quotaRec // userID -> 当日计数
}

type quotaRec struct {
	day   string // 2026-11-25
	count int
}

func newAgentQuotaTracker(daily int) *agentQuotaTracker {
	return &agentQuotaTracker{daily: daily, counts: map[string]*quotaRec{}}
}

// take 尝试消耗一次配额。返回 false 时附剩余等待提示。
func (q *agentQuotaTracker) take(userID string) error {
	if q == nil || q.daily <= 0 {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	q.mu.Lock()
	defer q.mu.Unlock()
	rec := q.counts[userID]
	if rec == nil || rec.day != today {
		rec = &quotaRec{day: today}
		q.counts[userID] = rec
	}
	if rec.count >= q.daily {
		return fmt.Errorf("今日 AI 配额已用完（%d 次/天），请明日再试或联系管理员调整 -agent-daily-quota", q.daily)
	}
	rec.count++
	// 顺带清理隔日记录，避免内存增长
	if len(q.counts) > 1000 {
		for uid, r := range q.counts {
			if r.day != today {
				delete(q.counts, uid)
			}
		}
	}
	return nil
}

// usage 返回该用户今日已用次数。
func (q *agentQuotaTracker) usage(userID string) int {
	if q == nil || q.daily <= 0 {
		return 0
	}
	today := time.Now().Format("2006-01-02")
	q.mu.Lock()
	defer q.mu.Unlock()
	rec := q.counts[userID]
	if rec == nil || rec.day != today {
		return 0
	}
	return rec.count
}
