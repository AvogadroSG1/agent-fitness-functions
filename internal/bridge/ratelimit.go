package bridge

import (
	"sync"
	"time"
)

// RateLimiter decides whether a request from a named caller is allowed.
type RateLimiter interface {
	Allow(caller string) bool
}

// fixedWindowRateLimiter is a per-caller fixed-window counter.
type fixedWindowRateLimiter struct {
	mu         sync.Mutex
	limit      int
	windowSize time.Duration
	windows    map[string]*callerWindow
	nowFunc    func() time.Time
}

type callerWindow struct {
	count int
	start time.Time
}

// NewFixedWindowRateLimiter returns a RateLimiter that allows at most limit
// requests per windowSize per caller.
func NewFixedWindowRateLimiter(limit int, windowSize time.Duration) RateLimiter {
	return &fixedWindowRateLimiter{
		limit:      limit,
		windowSize: windowSize,
		windows:    make(map[string]*callerWindow),
		nowFunc:    time.Now,
	}
}

func (l *fixedWindowRateLimiter) Allow(caller string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFunc()
	w, ok := l.windows[caller]
	if !ok || now.Sub(w.start) >= l.windowSize {
		l.windows[caller] = &callerWindow{count: 1, start: now}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
