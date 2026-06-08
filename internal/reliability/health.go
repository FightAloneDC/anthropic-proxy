package reliability

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type HealthMonitor struct {
	mu          sync.RWMutex
	enabled     bool
	url         string
	apiKey      string
	client      *http.Client
	breaker     *CircuitBreaker
	interval    time.Duration
	lastSuccess time.Time
	lastFailure time.Time
	lastError   string
	healthy     bool
	stop        chan struct{}
}

type HealthSnapshot struct {
	Enabled     bool            `json:"enabled"`
	Healthy     bool            `json:"healthy"`
	LastSuccess string          `json:"last_success,omitempty"`
	LastFailure string          `json:"last_failure,omitempty"`
	LastError   string          `json:"last_error,omitempty"`
	Circuit     CircuitSnapshot `json:"circuit"`
}

func NewHealthMonitor(enabled bool, backendURL, apiKey string, interval, timeout time.Duration, breaker *CircuitBreaker) *HealthMonitor {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	base := strings.TrimRight(backendURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = base[:len(base)-3]
	}
	return &HealthMonitor{
		enabled:  enabled,
		url:      base + "/v1/models",
		apiKey:   apiKey,
		client:   &http.Client{Timeout: timeout},
		breaker:  breaker,
		interval: interval,
		healthy:  true,
		stop:     make(chan struct{}),
	}
}

func (hm *HealthMonitor) Start() {
	if hm == nil || !hm.enabled {
		return
	}
	go func() {
		hm.Check()
		ticker := time.NewTicker(hm.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				hm.Check()
			case <-hm.stop:
				return
			}
		}
	}()
}

func (hm *HealthMonitor) Stop() {
	if hm == nil || !hm.enabled {
		return
	}
	select {
	case <-hm.stop:
	default:
		close(hm.stop)
	}
}

func (hm *HealthMonitor) Check() {
	if hm == nil || !hm.enabled {
		return
	}
	req, err := http.NewRequest(http.MethodGet, hm.url, nil)
	if err == nil && hm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+hm.apiKey)
	}
	if err != nil {
		hm.recordFailure(err.Error())
		return
	}
	resp, err := hm.client.Do(req)
	if err != nil {
		hm.recordFailure(err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		hm.recordFailure(resp.Status)
		return
	}
	hm.recordSuccess()
}

func (hm *HealthMonitor) Snapshot() HealthSnapshot {
	if hm == nil {
		return HealthSnapshot{Circuit: CircuitSnapshot{State: CircuitClosed}}
	}
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	circuit := CircuitSnapshot{State: CircuitClosed}
	if hm.breaker != nil {
		circuit = hm.breaker.Snapshot()
	}
	snapshot := HealthSnapshot{Enabled: hm.enabled, Healthy: hm.healthy, LastError: hm.lastError, Circuit: circuit}
	if !hm.lastSuccess.IsZero() {
		snapshot.LastSuccess = hm.lastSuccess.Format(time.RFC3339)
	}
	if !hm.lastFailure.IsZero() {
		snapshot.LastFailure = hm.lastFailure.Format(time.RFC3339)
	}
	return snapshot
}

func (hm *HealthMonitor) recordSuccess() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.healthy = true
	hm.lastSuccess = time.Now()
	hm.lastError = ""
	if hm.breaker != nil {
		hm.breaker.RecordSuccess()
	}
}

func (hm *HealthMonitor) recordFailure(message string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.healthy = false
	hm.lastFailure = time.Now()
	hm.lastError = message
	if hm.breaker != nil {
		hm.breaker.RecordFailure(message)
	}
}
