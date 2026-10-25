// Package ratelimit is an in-memory token-bucket limiter keyed by string (a token id, a client
// address, an email being signed in to). Limits are per process: with several replicas the
// effective limit is the per-replica limit times the replica count.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter allows Burst events at once and refills at Rate events per second, per key.
type Limiter struct {
	Rate  float64
	Burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New allows `burst` events at once and `perMinute` events a minute after that.
func New(perMinute, burst int) *Limiter {
	return &Limiter{Rate: float64(perMinute) / 60, Burst: float64(burst), now: time.Now, buckets: map[string]*bucket{}}
}

// Allow takes one token for key. When none is left it reports how long until one is.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	return l.AllowN(key, 1)
}

// AllowN takes n tokens for key.
func (l *Limiter) AllowN(key string, n float64) (bool, time.Duration) {
	if l == nil || l.Rate <= 0 {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.Burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.Burst, b.tokens+now.Sub(b.last).Seconds()*l.Rate)
	b.last = now
	if b.tokens >= n {
		b.tokens -= n
		return true, 0
	}
	wait := time.Duration((n - b.tokens) / l.Rate * float64(time.Second))
	return false, wait
}

// Reset forgets a key (e.g. after a successful sign-in).
func (l *Limiter) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.buckets, key)
	l.mu.Unlock()
}

// sweep drops buckets that have refilled completely, at most once a minute.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	l.swept = now
	full := time.Duration(l.Burst / l.Rate * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) > full {
			delete(l.buckets, k)
		}
	}
}
