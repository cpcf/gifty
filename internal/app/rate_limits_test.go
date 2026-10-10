package app

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPBehindProxy(t *testing.T) {
	a := setup(t)
	r := httptest.NewRequest("POST", "/api/login", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 203.0.113.9")
	if got := a.clientIP(r); got != "127.0.0.1" {
		t.Fatalf("trusted the header without GIFTY_TRUST_PROXY: %s", got)
	}
	a.trustProxy = true
	if got := a.clientIP(r); got != "203.0.113.9" {
		t.Fatalf("got %s, want the address the proxy appended", got)
	}
	r.RemoteAddr = "198.51.100.7:5000"
	if got := a.clientIP(r); got != "198.51.100.7" {
		t.Fatalf("trusted the header from a remote client: %s", got)
	}
	r.RemoteAddr, r.Header["X-Forwarded-For"] = "127.0.0.1:5000", []string{"not-an-ip"}
	if got := a.clientIP(r); got != "127.0.0.1" {
		t.Fatalf("accepted a malformed header: %s", got)
	}
}

func TestLimiterEvictsTheEntryNearestExpiry(t *testing.T) {
	a := setup(t)
	now := time.Now()
	for i := 0; i < 10000; i++ {
		a.limits[fmt.Sprint("k", i)] = limiter{1, now.Add(time.Hour + time.Duration(i)*time.Second)}
	}
	a.limits["soonest"] = limiter{1, now.Add(time.Minute)}
	delete(a.limits, "k9999")
	if !a.allow("new", 30) {
		t.Fatal("a new key was refused")
	}
	if _, ok := a.limits["soonest"]; ok {
		t.Fatal("evicted something other than the entry nearest expiry")
	}
	if len(a.limits) != 10000 {
		t.Fatalf("limiter holds %d keys", len(a.limits))
	}
	// IPv6 callers share a limit across their /64.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:1"
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "[2001:db8:1:2::1]:1"
	if a.clientIP(r) != a.clientIP(r2) {
		t.Fatal("addresses in one /64 are limited separately")
	}
}

func TestAbuseLimits(t *testing.T) {
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	c := &client{t: t, a: a}
	c.signup("ana")
	u := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
	// Resending a confirmation every minute stops after 5 account emails in a day.
	for i := 0; i < 4; i++ {
		u.LastMail = time.Time{}
		c.req("POST", "verify/resend", nil, 200)
	}
	u.LastMail = time.Time{}
	c.req("POST", "verify/resend", nil, 429)
	if len(a.state.Outbox) != 5 {
		t.Fatalf("queued %d account emails, want 5", len(a.state.Outbox))
	}
	before := len(a.state.Outbox)
	u.ResetExpires = time.Time{}
	(&client{t: t, a: a}).req("POST", "reset/request", map[string]string{"Email": "ana@example.com"}, 200)
	if len(a.state.Outbox) != before {
		t.Fatal("reset email sent past the daily limit")
	}
	for i := 0; i < 20; i++ {
		c.req("POST", "exchanges", map[string]string{"Name": fmt.Sprint("Swap ", i), "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	}
	c.req("POST", "exchanges", map[string]string{"Name": "One too many", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 400)
	// The limiter tracks a bounded number of addresses but still admits new ones when full.
	for i := len(a.limits); i < 10000; i++ {
		a.limits[fmt.Sprint("10.0.", i)] = limiter{1, time.Now().Add(time.Hour)}
	}
	if !a.allow("203.0.113.1", 30) {
		t.Fatal("a new address was locked out of a full limiter")
	}
	if len(a.limits) > 10000 {
		t.Fatal("limiter grew past its cap")
	}
}

func TestWriteAndTokenLimits(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	c.signup("ana")
	// Cheap authenticated writes are throttled per client.
	for i := 0; i < 300; i++ {
		c.req("POST", "account", map[string]bool{"Notify": true}, 200)
	}
	c.req("POST", "account", map[string]bool{"Notify": true}, 429)
	c.req("GET", "me", nil, 200)
	// Token endpoints share the auth limiter.
	d := &client{t: t, a: a}
	for i := 0; i < 29; i++ { // the signup above used one of the 30
		d.req("POST", "verify", map[string]string{"Token": "nope"}, 404)
	}
	d.req("POST", "verify", map[string]string{"Token": "nope"}, 429)
}
