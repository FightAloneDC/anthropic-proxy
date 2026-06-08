package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	format string
	level  string
}

func NewLogger(format, level string) *Logger {
	if format == "" {
		format = "text"
	}
	if level == "" {
		level = "info"
	}
	return &Logger{format: format, level: level}
}

func (l *Logger) Debug(msg string, fields map[string]interface{}) { l.log("debug", msg, fields) }
func (l *Logger) Info(msg string, fields map[string]interface{})  { l.log("info", msg, fields) }
func (l *Logger) Warn(msg string, fields map[string]interface{})  { l.log("warn", msg, fields) }
func (l *Logger) Error(msg string, fields map[string]interface{}) { l.log("error", msg, fields) }

func (l *Logger) log(level, msg string, fields map[string]interface{}) {
	if !logLevelEnabled(l.level, level) {
		return
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["level"] = level
	fields["msg"] = msg

	if l.format == "json" {
		b, _ := json.Marshal(fields)
		log.Print(string(b))
		return
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, quoteLogValue(fields[key])))
	}
	log.Print(strings.Join(parts, " "))
}

func logLevelEnabled(configured, level string) bool {
	levels := map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}
	configuredLevel, ok := levels[configured]
	if !ok {
		configuredLevel = 1
	}
	messageLevel, ok := levels[level]
	if !ok {
		messageLevel = 1
	}
	return messageLevel >= configuredLevel
}

func quoteLogValue(v interface{}) string {
	s := fmt.Sprint(v)
	if strings.ContainsAny(s, " \t\n\"") {
		return strconv.Quote(s)
	}
	return s
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("x-request-id"); id != "" {
		return id
	}
	if id := r.Header.Get("request-id"); id != "" {
		return id
	}
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	return sr.ResponseWriter.Write(b)
}

func (sr *statusRecorder) Flush() {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	if flusher, ok := sr.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

type Metrics struct {
	mu             sync.Mutex
	requests       map[string]int
	errors         map[string]int
	durationCount  map[string]int
	durationSum    map[string]float64
	activeRequests int
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:      map[string]int{},
		errors:        map[string]int{},
		durationCount: map[string]int{},
		durationSum:   map[string]float64{},
	}
}

func (m *Metrics) Observe(endpoint, method string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(endpoint, method, strconv.Itoa(status))
	m.requests[key]++
	m.durationCount[endpoint]++
	m.durationSum[endpoint] += duration.Seconds()
	if status >= 400 {
		m.errors[metricKey(endpoint, method, strconv.Itoa(status))]++
	}
}

func (m *Metrics) IncActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.activeRequests++
	m.mu.Unlock()
}

func (m *Metrics) DecActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.activeRequests > 0 {
		m.activeRequests--
	}
	m.mu.Unlock()
}

func (m *Metrics) Prometheus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	b.WriteString("# HELP anthropic_proxy_requests_total Total HTTP requests handled by the proxy.\n")
	b.WriteString("# TYPE anthropic_proxy_requests_total counter\n")
	for key, value := range m.requests {
		parts := strings.Split(key, "\xff")
		b.WriteString(fmt.Sprintf("anthropic_proxy_requests_total{endpoint=%q,method=%q,status=%q} %d\n", parts[0], parts[1], parts[2], value))
	}
	b.WriteString("# HELP anthropic_proxy_request_duration_seconds Total request duration.\n")
	b.WriteString("# TYPE anthropic_proxy_request_duration_seconds summary\n")
	for endpoint, value := range m.durationCount {
		b.WriteString(fmt.Sprintf("anthropic_proxy_request_duration_seconds_count{endpoint=%q} %d\n", endpoint, value))
		b.WriteString(fmt.Sprintf("anthropic_proxy_request_duration_seconds_sum{endpoint=%q} %.6f\n", endpoint, m.durationSum[endpoint]))
	}
	b.WriteString("# HELP anthropic_proxy_errors_total Total HTTP errors handled by the proxy.\n")
	b.WriteString("# TYPE anthropic_proxy_errors_total counter\n")
	for key, value := range m.errors {
		parts := strings.Split(key, "\xff")
		b.WriteString(fmt.Sprintf("anthropic_proxy_errors_total{endpoint=%q,method=%q,status=%q} %d\n", parts[0], parts[1], parts[2], value))
	}
	b.WriteString("# HELP anthropic_proxy_active_requests Active requests.\n")
	b.WriteString("# TYPE anthropic_proxy_active_requests gauge\n")
	b.WriteString(fmt.Sprintf("anthropic_proxy_active_requests %d\n", m.activeRequests))
	return b.String()
}

func metricKey(parts ...string) string { return strings.Join(parts, "\xff") }

func (h *Handler) Observe(endpoint string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := requestID(r)
		r.Header.Set("x-request-id", id)
		w.Header().Set("x-request-id", id)
		w.Header().Set("request-id", id)
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		h.metrics.IncActive()
		defer h.metrics.DecActive()
		next(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)
		h.metrics.Observe(endpoint, r.Method, status, duration)
		h.logger.Info("request completed", map[string]interface{}{
			"request_id": id,
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     status,
			"latency_ms": duration.Milliseconds(),
		})
	}
}

func normalizedEndpoint(path string) string {
	if strings.HasPrefix(path, "/gemini/v1beta/models/") {
		if strings.HasSuffix(path, ":generateContent") {
			return "/gemini/v1beta/models/{model}:generateContent"
		}
		if strings.HasSuffix(path, ":streamGenerateContent") {
			return "/gemini/v1beta/models/{model}:streamGenerateContent"
		}
		if strings.HasSuffix(path, ":embedContent") {
			return "/gemini/v1beta/models/{model}:embedContent"
		}
	}
	return path
}
