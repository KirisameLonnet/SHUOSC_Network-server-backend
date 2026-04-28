package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimitBucket struct {
	mu        sync.Mutex
	timestamps []time.Time
	limit     int
	window    time.Duration
}

func (b *rateLimitBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-b.window)

	valid := b.timestamps[:0]
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	b.timestamps = valid

	if len(b.timestamps) >= b.limit {
		return false
	}

	b.timestamps = append(b.timestamps, now)
	return true
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateLimitBucket
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*rateLimitBucket),
	}
}

func (rl *rateLimiter) getOrCreate(key string, limit int, window time.Duration) *rateLimitBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if b, ok := rl.buckets[key]; ok {
		return b
	}

	b := &rateLimitBucket{
		limit:  limit,
		window: window,
	}
	rl.buckets[key] = b
	return b
}

func (rl *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	b := rl.getOrCreate(key, limit, window)
	return b.allow()
}

var globalRateLimiter = newRateLimiter()

func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		var req struct {
			StudentID string `json:"student_id"`
		}
		if json.Unmarshal(body, &req) != nil || req.StudentID == "" {
			c.Next()
			return
		}

		key := "login:" + req.StudentID
		if !globalRateLimiter.allow(key, 5, time.Minute) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many login attempts, try again later",
				"code":  "RATE_LIMITED",
			})
			return
		}
		c.Next()
	}
}

func RegisterRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := "register:" + ip
		if !globalRateLimiter.allow(key, 10, time.Hour) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many registration attempts, try again later",
				"code":  "RATE_LIMITED",
			})
			return
		}
		c.Next()
	}
}

func PeerRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.Next()
			return
		}
		key := "peer:" + userID
		if !globalRateLimiter.allow(key, 30, time.Minute) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, try again later",
				"code":  "RATE_LIMITED",
			})
			return
		}
		c.Next()
	}
}
