package machine

import (
	"sync"
	"time"
)

const (
	gatewayRouteWindow          = time.Second
	gatewayRoutesPerSource      = 60
	gatewayRoutesGlobal         = 500
	gatewayMaxTrackedSources    = 4096
	gatewayNegativeRouteTTL     = 30 * time.Second
	gatewayMaxNegativeRoutes    = 8192
	gatewayUnknownAuditInterval = 5 * time.Minute
	gatewayUnknownAuditsGlobal  = 10
	gatewayUnknownAuditWindow   = time.Minute
	gatewayOverflowSource       = "__overflow__"
)

type gatewayRequestWindow struct {
	started time.Time
	count   int
}

type gatewayAbuseGuard struct {
	mu sync.Mutex

	routeSources  map[string]gatewayRequestWindow
	routeGlobal   gatewayRequestWindow
	negativeUntil map[string]time.Time
	auditAfter    map[string]time.Time
	auditGlobal   gatewayRequestWindow
}

func newGatewayAbuseGuard() *gatewayAbuseGuard {
	return &gatewayAbuseGuard{
		routeSources:  make(map[string]gatewayRequestWindow),
		negativeUntil: make(map[string]time.Time),
		auditAfter:    make(map[string]time.Time),
	}
}

func (g *gatewayAbuseGuard) allowRouteLookup(source string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	source = boundedGatewaySource(g.routeSources, source, gatewayMaxTrackedSources)
	global, allowed := advanceGatewayWindow(g.routeGlobal, now, gatewayRouteWindow, gatewayRoutesGlobal)
	if !allowed {
		g.routeGlobal = global
		return false
	}
	perSource, allowed := advanceGatewayWindow(g.routeSources[source], now, gatewayRouteWindow, gatewayRoutesPerSource)
	if !allowed {
		return false
	}
	g.routeGlobal = global
	g.routeSources[source] = perSource
	return true
}

func (g *gatewayAbuseGuard) negativeRoute(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.negativeUntil[key]
	if ok && now.Before(until) {
		return true
	}
	delete(g.negativeUntil, key)
	return false
}

func (g *gatewayAbuseGuard) rememberNegativeRoute(key string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.negativeUntil) >= gatewayMaxNegativeRoutes {
		for existing := range g.negativeUntil {
			delete(g.negativeUntil, existing)
			break
		}
	}
	g.negativeUntil[key] = now.Add(gatewayNegativeRouteTTL)
}

func (g *gatewayAbuseGuard) allowUnknownAudit(source string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	source = boundedGatewayAuditSource(g.auditAfter, source, gatewayMaxTrackedSources)
	if next := g.auditAfter[source]; now.Before(next) {
		return false
	}
	global, allowed := advanceGatewayWindow(g.auditGlobal, now, gatewayUnknownAuditWindow, gatewayUnknownAuditsGlobal)
	g.auditAfter[source] = now.Add(gatewayUnknownAuditInterval)
	if !allowed {
		return false
	}
	g.auditGlobal = global
	return true
}

func advanceGatewayWindow(window gatewayRequestWindow, now time.Time, duration time.Duration, limit int) (gatewayRequestWindow, bool) {
	if window.started.IsZero() || now.Sub(window.started) >= duration {
		window = gatewayRequestWindow{started: now}
	}
	if window.count >= limit {
		return window, false
	}
	window.count++
	return window, true
}

func boundedGatewaySource(entries map[string]gatewayRequestWindow, source string, limit int) string {
	if source == "" {
		source = "unknown"
	}
	if _, ok := entries[source]; ok || len(entries) < limit {
		return source
	}
	if _, ok := entries[gatewayOverflowSource]; !ok {
		for key := range entries {
			delete(entries, key)
			break
		}
	}
	return gatewayOverflowSource
}

func boundedGatewayAuditSource(entries map[string]time.Time, source string, limit int) string {
	if source == "" {
		source = "unknown"
	}
	if _, ok := entries[source]; ok || len(entries) < limit {
		return source
	}
	if _, ok := entries[gatewayOverflowSource]; !ok {
		for key := range entries {
			delete(entries, key)
			break
		}
	}
	return gatewayOverflowSource
}
