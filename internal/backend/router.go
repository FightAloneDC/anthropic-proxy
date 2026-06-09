package backend

import (
	"sort"
	"strings"
	"sync/atomic"

	"anthropic-proxy/internal/config"
)

type Backend struct {
	Name       string
	URL        string
	APIKey     string
	Models     []string
	Weight     int
	Priority   int
	Enabled    bool
	DropFields []string
}

type Runtime struct {
	Backend Backend
	active  int64
}

type Target struct {
	Runtime *Runtime
	URL     string
}

type Strategy string

const (
	RoundRobin       Strategy = "round_robin"
	Weighted         Strategy = "weighted"
	LeastConnections Strategy = "least_connections"
)

type Router struct {
	runtimes []*Runtime
	strategy Strategy
	next     uint64
}

func NewRouter(backends []config.BackendConfig, strategy string) *Router {
	runtimes := make([]*Runtime, 0, len(backends))
	for _, cfg := range backends {
		enabled := true
		if cfg.Enabled != nil {
			enabled = *cfg.Enabled
		}
		if !enabled {
			continue
		}
		models := cfg.Models
		if len(models) == 0 {
			models = []string{"*"}
		}
		weight := cfg.Weight
		if weight <= 0 {
			weight = 1
		}
		runtimes = append(runtimes, &Runtime{Backend: Backend{
			Name:       cfg.Name,
			URL:        cfg.URL,
			APIKey:     cfg.APIKey,
			Models:     models,
			Weight:     weight,
			Priority:   cfg.Priority,
			Enabled:    enabled,
			DropFields: cfg.Overrides.DropFields,
		}})
	}
	sort.SliceStable(runtimes, func(i, j int) bool {
		return runtimes[i].Backend.Priority < runtimes[j].Backend.Priority
	})
	selected := Strategy(strategy)
	if selected == "" {
		selected = RoundRobin
	}
	return &Router{runtimes: runtimes, strategy: selected}
}

func (r *Router) Backends() []*Runtime {
	return r.runtimes
}

func (r *Router) Multi() bool {
	return len(r.runtimes) > 1
}

func (r *Router) Candidates(model, targetPath string, limit int) []*Target {
	if r == nil {
		return nil
	}
	matches := make([]*Runtime, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		if runtime.Backend.Enabled && runtime.Matches(model) {
			matches = append(matches, runtime)
		}
	}
	ordered := r.order(matches)
	if limit > 0 && len(ordered) > limit {
		ordered = ordered[:limit]
	}
	targets := make([]*Target, 0, len(ordered))
	for _, runtime := range ordered {
		targets = append(targets, &Target{Runtime: runtime, URL: JoinURL(runtime.Backend.URL, targetPath)})
	}
	return targets
}

func (r *Router) DefaultTarget(targetPath string) *Target {
	if r == nil || len(r.runtimes) == 0 {
		return nil
	}
	runtime := r.runtimes[0]
	return &Target{Runtime: runtime, URL: JoinURL(runtime.Backend.URL, targetPath)}
}

func (r *Router) order(matches []*Runtime) []*Runtime {
	if len(matches) <= 1 {
		return matches
	}
	switch r.strategy {
	case Weighted:
		return r.weighted(matches)
	case LeastConnections:
		ordered := append([]*Runtime(nil), matches...)
		sort.SliceStable(ordered, func(i, j int) bool {
			left := atomic.LoadInt64(&ordered[i].active)
			right := atomic.LoadInt64(&ordered[j].active)
			if left == right {
				return ordered[i].Backend.Priority < ordered[j].Backend.Priority
			}
			return left < right
		})
		return ordered
	default:
		return rotate(matches, int(atomic.AddUint64(&r.next, 1)-1))
	}
}

func (r *Router) weighted(matches []*Runtime) []*Runtime {
	expanded := make([]*Runtime, 0, len(matches))
	for _, runtime := range matches {
		weight := runtime.Backend.Weight
		if weight <= 0 {
			weight = 1
		}
		for i := 0; i < weight; i++ {
			expanded = append(expanded, runtime)
		}
	}
	rotated := rotate(expanded, int(atomic.AddUint64(&r.next, 1)-1))
	seen := map[*Runtime]bool{}
	ordered := make([]*Runtime, 0, len(matches))
	for _, runtime := range rotated {
		if seen[runtime] {
			continue
		}
		seen[runtime] = true
		ordered = append(ordered, runtime)
	}
	return ordered
}

func rotate(in []*Runtime, offset int) []*Runtime {
	if len(in) == 0 {
		return nil
	}
	ordered := make([]*Runtime, 0, len(in))
	start := offset % len(in)
	ordered = append(ordered, in[start:]...)
	ordered = append(ordered, in[:start]...)
	return ordered
}

func (b *Runtime) Matches(model string) bool {
	for _, pattern := range b.Backend.Models {
		if MatchModel(pattern, model) {
			return true
		}
	}
	return false
}

func (b *Runtime) IncActive() {
	atomic.AddInt64(&b.active, 1)
}

func (b *Runtime) DecActive() {
	atomic.AddInt64(&b.active, -1)
}

func (b *Runtime) Active() int64 {
	return atomic.LoadInt64(&b.active)
}

func MatchModel(pattern, model string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if pattern == model {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]
		return strings.HasPrefix(model, prefix) && strings.HasSuffix(model, suffix)
	}
	pos := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(model[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	return true
}

func JoinURL(base, targetPath string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	return base + targetPath
}
