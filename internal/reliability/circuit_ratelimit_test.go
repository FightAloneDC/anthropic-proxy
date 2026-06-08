package reliability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	cb := NewCircuitBreaker(true, 2, time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("closed circuit should allow")
	}
	cb.RecordFailure("first")
	if !cb.Allow() {
		t.Fatalf("below threshold should allow")
	}
	cb.RecordFailure("second")
	if cb.Allow() {
		t.Fatalf("open circuit should reject")
	}
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("after cooldown should allow half-open probe")
	}
	cb.RecordSuccess()
	if snapshot := cb.Snapshot(); snapshot.State != CircuitClosed || snapshot.Failures != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(true, 1, time.Millisecond)
	cb.RecordFailure("fail")
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("half-open probe should be allowed")
	}
	cb.RecordFailure("probe failed")
	if cb.Allow() {
		t.Fatalf("failed probe should reopen circuit")
	}
}

func TestDisabledCircuitBreakerAlwaysAllows(t *testing.T) {
	cb := NewCircuitBreaker(false, 1, time.Hour)
	cb.RecordFailure("fail")
	if !cb.Allow() {
		t.Fatalf("disabled circuit breaker should allow")
	}
}

func TestRateLimiterDisabledAllows(t *testing.T) {
	rl := NewRateLimiter(false, 1, 1)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for i := 0; i < 3; i++ {
		if !rl.Allow(req) {
			t.Fatalf("disabled limiter rejected request %d", i)
		}
	}
}

func TestRateLimiterAllowsBurstThenRejects(t *testing.T) {
	rl := NewRateLimiter(true, 60, 2)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if !rl.Allow(req) || !rl.Allow(req) {
		t.Fatalf("burst requests should be allowed")
	}
	if rl.Allow(req) {
		t.Fatalf("third request should be rejected")
	}
}

func TestClientIPUsesForwardedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if got := ClientIP(req); got != "10.0.0.1" {
		t.Fatalf("client ip = %q", got)
	}
}
