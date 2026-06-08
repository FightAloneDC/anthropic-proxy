package reliability

import (
	"bytes"
	"context"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Sleeper func(time.Duration)

type RetryConfig struct {
	Enabled        bool
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Sleep          Sleeper
}

type BackendExecutor struct {
	Client  *http.Client
	Breaker *CircuitBreaker
	Config  RetryConfig
}

func (be *BackendExecutor) Do(req *http.Request, body []byte, retryable bool) (*http.Response, error) {
	if be == nil || be.Client == nil {
		return nil, context.Canceled
	}
	if be.Breaker != nil && !be.Breaker.Allow() {
		return nil, ErrCircuitOpen
	}
	maxAttempts := 1
	if retryable && be.Config.Enabled {
		maxAttempts = be.Config.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
	}
	backoff := be.Config.InitialBackoff
	if backoff <= 0 {
		backoff = 200 * time.Millisecond
	}
	maxBackoff := be.Config.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 2 * time.Second
	}
	sleep := be.Config.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptReq, err := cloneRequest(req, body)
		if err != nil {
			return nil, err
		}
		resp, err := be.Client.Do(attemptReq)
		if err == nil && !retryableStatus(resp.StatusCode) {
			if be.Breaker != nil {
				be.Breaker.RecordSuccess()
			}
			return resp, nil
		}
		if err == nil && attempt == maxAttempts {
			if be.Breaker != nil {
				be.Breaker.RecordFailure(resp.Status)
			}
			return resp, nil
		}
		if err == nil {
			lastErr = nil
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		} else {
			lastErr = err
			if !retryableError(err) || attempt == maxAttempts {
				if be.Breaker != nil {
					be.Breaker.RecordFailure(err.Error())
				}
				return nil, err
			}
		}
		if attempt < maxAttempts {
			sleep(retryDelay(backoff, maxBackoff, attempt))
		}
	}
	return nil, lastErr
}

var ErrCircuitOpen = &circuitOpenError{}

type circuitOpenError struct{}

func (e *circuitOpenError) Error() string { return "backend circuit is open" }

func cloneRequest(req *http.Request, body []byte) (*http.Request, error) {
	cloned := req.Clone(req.Context())
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	cloned.ContentLength = int64(len(body))
	return cloned, nil
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryableError(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return true
}

func retryDelay(initial, max time.Duration, attempt int) time.Duration {
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(initial) * multiplier)
	if delay > max {
		return max
	}
	return delay
}

func ParseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay < 0 {
		return 0
	}
	return delay
}
