// Package ratelimit provides rate limiting middleware for the isola-gw service.
package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/gateway/metrics"
)

// Config holds rate limiter configuration.
type Config struct {
	// RequestsPerSecond is the maximum requests per second per tenant.
	RequestsPerSecond float64
	// BurstSize is the maximum burst size.
	BurstSize int
	// CleanupInterval is how often to clean up expired entries.
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults for rate limiting.
func DefaultConfig() Config {
	return Config{
		RequestsPerSecond: 50,  // 50 requests per second
		BurstSize:         100, // Burst up to 100 requests
		CleanupInterval:   time.Minute,
	}
}

// tokenBucket implements a simple token bucket rate limiter.
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	// Refill tokens based on elapsed time
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	// Check if we have a token available
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// Limiter manages rate limits per tenant.
type Limiter struct {
	config  Config
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// NewLimiter creates a new rate limiter with the given config.
func NewLimiter(config Config) *Limiter {
	l := &Limiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
		stopCh:  make(chan struct{}),
	}

	// Start cleanup goroutine
	go l.cleanup()

	return l
}

// Allow checks if a request from the given tenant should be allowed.
func (l *Limiter) Allow(tenantID string) bool {
	l.mu.RLock()
	bucket, exists := l.buckets[tenantID]
	l.mu.RUnlock()

	if !exists {
		l.mu.Lock()
		// Double-check after acquiring write lock
		bucket, exists = l.buckets[tenantID]
		if !exists {
			bucket = newTokenBucket(l.config.RequestsPerSecond, l.config.BurstSize)
			l.buckets[tenantID] = bucket
		}
		l.mu.Unlock()
	}

	return bucket.allow()
}

// cleanup periodically removes stale bucket entries.
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(l.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for tenantID, bucket := range l.buckets {
				bucket.mu.Lock()
				// Remove buckets that haven't been used in 5 minutes
				if now.Sub(bucket.lastRefill) > 5*time.Minute {
					delete(l.buckets, tenantID)
				}
				bucket.mu.Unlock()
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop stops the cleanup goroutine.
func (l *Limiter) Stop() {
	close(l.stopCh)
}

// Middleware returns a gin middleware that applies rate limiting.
func Middleware(limiter *Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get tenant ID from context (set by auth middleware)
		tenantID, exists := c.Get("tenant_id")
		if !exists {
			// Fall back to IP-based limiting for unauthenticated requests
			tenantID = c.ClientIP()
		}

		tenant, ok := tenantID.(string)
		if !ok {
			tenant = c.ClientIP()
		}

		if !limiter.Allow(tenant) {
			endpoint := c.FullPath()
			if endpoint == "" {
				endpoint = c.Request.URL.Path
			}
			metrics.RateLimitRejections.WithLabelValues(endpoint).Inc()

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "TooManyRequests",
				"message": "Rate limit exceeded. Please slow down.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
