package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a simple in-memory, per-key sliding-window limiter. It
// fits the single-binary model: no external store, no dependency. It is
// used to slow down credential guessing on the login endpoint.
//
// rateLimiter, basit bir bellek-içi, anahtar-başına kayan-pencere
// sınırlayıcıdır. Tek-binary modeline uyar: harici depo yok, bağımlılık
// yok. Giriş uç noktasında kimlik bilgisi tahminini yavaşlatmak için
// kullanılır.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

// allow records an attempt for key and reports whether it is within the
// limit. Timestamps older than the window are pruned on each call.
// allow, key için bir deneme kaydeder ve limitin içinde olup olmadığını
// bildirir. Pencereden eski zaman damgaları her çağrıda budanır.
func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}

	l.hits[key] = append(recent, now)
	return true
}

// clientIP extracts the remote IP without the port. Behind a trusted
// reverse proxy this would consult X-Forwarded-For; direct exposure uses
// RemoteAddr as the source of truth.
// clientIP, uzak IP'yi port olmadan çıkarır. Güvenilir bir ters vekil
// arkasında X-Forwarded-For'a bakılırdı; doğrudan erişimde RemoteAddr
// doğruluğun kaynağıdır.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
