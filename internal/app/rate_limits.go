package app

import (
	"net"
	"net/http"
	"strings"
	"time"
)

type limiter struct {
	Count int
	Reset time.Time
}

// take counts one attempt in a 15-minute window and reports whether it was within max.
func (l *limiter) take(now time.Time, max int) bool {
	if now.After(l.Reset) {
		*l = limiter{}
	}
	if l.Count >= max {
		return false
	}
	if l.Count == 0 {
		l.Reset = now.Add(15 * time.Minute)
	}
	l.Count++
	return true
}

// clientIP is the caller's address for rate limiting. IPv6 callers are keyed by their /64, since one
// subscriber can use any address in it.
func (a *application) clientIP(r *http.Request) string {
	ip := a.rawIP(r)
	if p := net.ParseIP(ip); p != nil && p.To4() == nil {
		return p.Mask(net.CIDRMask(64, 128)).String()
	}
	return ip
}

func (a *application) rawIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a.trustProxy && net.ParseIP(ip).IsLoopback() {
		// The proxy appends the address it saw, so the last entry is the one it vouches for.
		if f := r.Header.Values("X-Forwarded-For"); len(f) > 0 {
			parts := strings.Split(f[len(f)-1], ",")
			if v := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(v) != nil {
				return v
			}
		}
	}
	return ip
}

// allow counts an attempt under key: max per 15 minutes. It tracks at most 10,000 keys; when full, the
// entry closest to expiring is dropped, which gives up the least throttling. Expired entries are swept once a minute.
func (a *application) allow(key string, max int) bool {
	now := time.Now()
	if now.Sub(a.swept) > time.Minute {
		a.sweep(now)
	}
	l, seen := a.limits[key]
	if !l.take(now, max) {
		return false
	}
	if !seen && len(a.limits) >= 10000 {
		a.sweep(now)
		if len(a.limits) >= 10000 {
			oldest, first := "", true
			for k, v := range a.limits {
				if first || v.Reset.Before(a.limits[oldest].Reset) {
					oldest, first = k, false
				}
			}
			delete(a.limits, oldest)
		}
	}
	a.limits[key] = l
	return true
}

func (a *application) sweep(now time.Time) {
	a.swept = now
	for k, l := range a.limits {
		if now.After(l.Reset) {
			delete(a.limits, k)
		}
	}
	for k, l := range a.fails {
		if now.After(l.Reset) {
			delete(a.fails, k)
		}
	}
}
