package reliability

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	enabled  bool
	rate     float64
	burst    float64
	buckets  map[string]*bucket
	now      func() time.Time
	interval time.Duration
}

type bucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

func NewRateLimiter(enabled bool, requestsPerMinute, burst int) *RateLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 60
	}
	if burst <= 0 {
		burst = requestsPerMinute
	}
	return &RateLimiter{
		enabled:  enabled,
		rate:     float64(requestsPerMinute) / 60.0,
		burst:    float64(burst),
		buckets:  map[string]*bucket{},
		now:      time.Now,
		interval: time.Minute,
	}
}

func (rl *RateLimiter) Allow(r *http.Request) bool {
	if rl == nil || !rl.enabled {
		return true
	}
	key := ClientIP(r)
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b := rl.buckets[key]
	if b == nil {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, updated: now, lastSeen: now}
		return true
	}
	elapsed := now.Sub(b.updated).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.updated = now
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *RateLimiter) Cleanup() {
	if rl == nil {
		return
	}
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for key, b := range rl.buckets {
		if now.Sub(b.lastSeen) > 5*rl.interval {
			delete(rl.buckets, key)
		}
	}
}

func (rl *RateLimiter) Enabled() bool {
	return rl != nil && rl.enabled
}

func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
