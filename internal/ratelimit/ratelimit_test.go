package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(60, 3) // one a second, bursts of three
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("burst %d refused", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != time.Second {
		t.Fatalf("over the burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("keys are independent")
	}
	now = now.Add(1500 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("refilled after a second")
	}
	l.Reset("a")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("reset gives a full bucket")
		}
	}
	now = now.Add(time.Hour)
	l.Allow("c")
	if len(l.buckets) != 1 {
		t.Fatalf("idle full buckets are swept: %d left", len(l.buckets))
	}
	var nilLimiter *Limiter
	if ok, _ := nilLimiter.Allow("x"); !ok {
		t.Fatal("a nil limiter allows everything")
	}
}
