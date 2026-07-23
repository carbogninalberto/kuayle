package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     float64 // tokens per second
	burst    int     // max tokens
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		burst:    burst,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > 3*time.Minute {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst) - 1, lastSeen: time.Now()}
		return true
	}

	elapsed := time.Since(v.lastSeen).Seconds()
	v.lastSeen = time.Now()
	v.tokens += elapsed * rl.rate
	if v.tokens > float64(rl.burst) {
		v.tokens = float64(rl.burst)
	}

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

// RateLimit returns middleware that limits requests per IP.
// rate: requests refilled per second, burst: max requests in a burst.
func RateLimit(rate float64, burst int) echo.MiddlewareFunc {
	return rateLimit(rate, burst, func(c echo.Context) string { return "ip:" + c.RealIP() })
}

// MachineTokenRateLimit gives each collector credential its own bucket without
// retaining the raw secret. Requests without a usable credential share an IP bucket.
func MachineTokenRateLimit(rate float64, burst int) echo.MiddlewareFunc {
	return rateLimit(rate, burst, machineTokenRateLimitKey)
}

func rateLimit(rate float64, burst int, key func(echo.Context) string) echo.MiddlewareFunc {
	rl := NewRateLimiter(rate, burst)
	retryAfter := 1
	if rate > 0 {
		retryAfter = max(1, int(math.Ceil(1/rate)))
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !rl.allow(key(c)) {
				c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
				return response.Error(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please try again later")
			}
			return next(c)
		}
	}
}

func machineTokenRateLimitKey(c echo.Context) string {
	parts := strings.Fields(c.Request().Header.Get(echo.HeaderAuthorization))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && len(parts[1]) == 64 {
		if _, err := hex.DecodeString(parts[1]); err == nil {
			fingerprint := sha256.Sum256([]byte(parts[1]))
			return "machine-token:" + hex.EncodeToString(fingerprint[:])
		}
	}
	return "ip:" + c.RealIP()
}
