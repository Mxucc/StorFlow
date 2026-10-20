package shop

import (
	"net/http"
	"sync"
	"time"

	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// rateLimiter is a per-key token bucket used to protect the public endpoints of
// the module (storefront reads and the payment callback) from abuse. It is kept
// inside the module on purpose: no reverse proxy configuration is required for a
// freshly deployed instance to be safe.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	capacity float64
	perSec   float64
	lastGC   time.Time
}

type tokenBucket struct {
	tokens float64
	seen   time.Time
}

const (
	// rateLimiterMaxKeys bounds the memory used by the limiter.
	rateLimiterMaxKeys = 20000
	// rateLimiterGCInterval is how often stale buckets are dropped.
	rateLimiterGCInterval = 5 * time.Minute
)

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*tokenBucket, 64),
		lastGC:  time.Now(),
	}
}

// setRate configures the limiter: perMinute is the sustained rate, burst the
// bucket size. A perMinute of 0 disables the limiter.
func (l *rateLimiter) setRate(perMinute, burst int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.perSec = float64(perMinute) / 60
	if burst <= 0 {
		burst = perMinute
	}
	l.capacity = float64(burst)
}

// allow consumes one token for the key. It reports false when the key is over
// its budget.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.perSec <= 0 {
		return true
	}

	now := time.Now()
	if now.Sub(l.lastGC) > rateLimiterGCInterval || len(l.buckets) > rateLimiterMaxKeys {
		l.gcLocked(now)
	}

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{tokens: l.capacity, seen: now}
		l.buckets[key] = bucket
	}

	// Refill proportionally to the elapsed time.
	bucket.tokens += now.Sub(bucket.seen).Seconds() * l.perSec
	if bucket.tokens > l.capacity {
		bucket.tokens = l.capacity
	}
	bucket.seen = now

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// gcLocked drops the buckets that were not used recently.
func (l *rateLimiter) gcLocked(now time.Time) {
	for key, bucket := range l.buckets {
		if now.Sub(bucket.seen) > rateLimiterGCInterval {
			delete(l.buckets, key)
		}
	}
	l.lastGC = now
}

// rateLimit builds a gin middleware for the module. rate is read per request so
// that an administrator changing the limit takes effect without a restart.
func rateLimit(limiter *rateLimiter, rate func() int) gin.HandlerFunc {
	return func(c *gin.Context) {
		perMinute := rate()
		if perMinute <= 0 {
			c.Next()
			return
		}

		limiter.setRate(perMinute, perMinute)
		if limiter.allow(c.ClientIP()) {
			c.Next()
			return
		}

		// 429 with a plain serializer error: the code stays in the usual range
		// so existing clients show the message instead of failing to parse it.
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, serializer.Err(c, serializer.NewError(
			serializer.CodeNoPermissionErr, "Too many requests, please slow down", nil)))
		c.Abort()
	}
}
