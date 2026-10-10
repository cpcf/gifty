package app

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthAndValidation(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	c.req("GET", "me", nil, 401)
	c.req("POST", "signup", map[string]string{"Name": "Sam", "Email": "bad", "Password": "tiny"}, 400)
	c.signup("Sam")
	c.req("POST", "login", map[string]string{"Email": "Sam@example.com", "Password": "incorrect"}, 401)
	c.req("POST", "login", map[string]string{"Email": "SAM@example.com", "Password": "correct horse battery staple"}, 200)
	for _, url := range []string{"javascript:alert(1)", "//evil.test", "https://user:pass@evil.test"} {
		c.req("POST", "wishes", map[string]string{"title": "Bad link", "url": url}, 400)
	}
	v := c.req("POST", "wishes", map[string]string{"title": "A book"}, 200)
	id := v["wishes"].([]any)[0].(map[string]any)["id"].(string)
	c.req("POST", "wishes", map[string]string{"id": id, "title": "Two books"}, 200)
	other := &client{t: t, a: a}
	other.signup("Other")
	other.req("POST", "wishes/"+id, map[string]string{}, 404)
	c.req("POST", "wishes/"+id, map[string]string{}, 200)
	for _, budget := range []string{"NaN", "-1", "0", "0.50", "1.123", "999999", "1e2"} {
		c.req("POST", "exchanges", map[string]string{"Name": "Gifts", "Date": "2099-12-20", "Budget": budget, "Currency": "GBP"}, 400)
	}
	r := httptest.NewRequest("POST", "/api/logout", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://evil.test")
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	r = httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Gifty") {
		t.Fatal("app shell unavailable")
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	r = httptest.NewRequest("GET", "/style.css", nil)
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	etag := w.Header().Get("ETag")
	if w.Code != 200 || etag == "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal("stylesheet not served with a validator")
	}
	r = httptest.NewRequest("GET", "/style.css", nil)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatalf("revalidation returned %d", w.Code)
	}
	for _, path := range []string{"/index.html", "/missing.js"} {
		w = httptest.NewRecorder()
		a.handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
}

func TestAccessGate(t *testing.T) {
	a := setup(t)
	a.key = []byte("test key")
	a.access = a.gate("open sesame")
	c := &client{t: t, a: a}
	cfg := c.req("GET", "config", nil, 200)
	if cfg["gated"] != true || cfg["access"] != false {
		t.Fatalf("config %v", cfg)
	}
	signup := map[string]string{"Name": "ana", "Email": "ana@example.com", "Password": "correct horse battery staple"}
	c.req("POST", "signup", signup, 403)
	c.req("POST", "access", map[string]string{"Code": "wrong"}, 403)
	c.req("POST", "access", map[string]string{"Code": " open sesame "}, 200)
	if c.access == nil || strings.Contains(c.access.Value, "sesame") || !c.access.HttpOnly {
		t.Fatalf("access cookie %+v", c.access)
	}
	if c.req("GET", "config", nil, 200)["access"] != true {
		t.Fatal("access not remembered")
	}
	c.req("POST", "signup", signup, 200)
	e := c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	// An open invitation lets someone sign up without the code; a closed or made-up one doesn't.
	b := &client{t: t, a: a}
	b.req("POST", "signup", map[string]string{"Name": "ben", "Email": "ben@example.com", "Password": "correct horse battery staple", "Invite": "made-up"}, 403)
	b.req("POST", "signup", map[string]string{"Name": "ben", "Email": "ben@example.com", "Password": "correct horse battery staple", "Invite": e["invite"].(string)}, 200)
	// Signing in needs the gate too, but an invited guest was let past it when they joined, and a password reset is another way back in.
	login := map[string]string{"Email": "ana@example.com", "Password": "correct horse battery staple"}
	(&client{t: t, a: a}).req("POST", "login", login, 403)
	if b.access == nil {
		t.Fatal("an invited guest was not given the access cookie")
	}
	b.req("POST", "logout", nil, 200)
	b.req("POST", "login", map[string]string{"Email": "ben@example.com", "Password": "correct horse battery staple"}, 200)
	(&client{t: t, a: a, access: c.access}).req("POST", "login", login, 200)
}

// Without the gate, a login or signup is refused before any password is hashed, so anonymous callers can't
// keep the hashing slots busy.
func TestGatedRequestsSkipHashing(t *testing.T) {
	a := setup(t)
	a.key = []byte("test key")
	a.access = a.gate("open sesame")
	a.hashSlots <- struct{}{}
	a.hashSlots <- struct{}{} // both slots taken: anything that tried to hash would wait
	c := &client{t: t, a: a}
	start := time.Now()
	c.req("POST", "login", map[string]string{"Email": "nobody@example.com", "Password": "whatever it is"}, 403)
	c.req("POST", "signup", map[string]string{"Name": "x", "Email": "x@example.com", "Password": "a long enough password"}, 403)
	c.req("POST", "reset", map[string]string{"Token": "made-up", "Password": "a long enough password"}, 404)
	c.req("POST", "account/delete", map[string]string{"Password": "whatever it is"}, 401)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("refusals took %v: they waited for a hashing slot", d)
	}
}

func TestAccessCodeGuessesShareOneBudget(t *testing.T) {
	a := setup(t)
	a.key = []byte("test key")
	a.access = a.gate("open sesame")
	for i := 0; i < 20; i++ {
		// Every guess comes from a different address, so only the site-wide budget can stop them.
		r := httptest.NewRequest("POST", "/api/access", strings.NewReader(`{"Code":"guess"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", i+1)
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("guess %d: %d", i, w.Code)
		}
	}
	c := &client{t: t, a: a}
	c.req("POST", "access", map[string]string{"Code": "open sesame"}, 429)
	a.gateFails = limiter{}
	c.req("POST", "access", map[string]string{"Code": "open sesame"}, 200)
}

func TestAccountLockout(t *testing.T) {
	a := setup(t)
	(&client{t: t, a: a}).signup("ana")
	c := &client{t: t, a: a}
	wrong := map[string]string{"Email": "ana@example.com", "Password": "not the password"}
	for i := 0; i < 10; i++ {
		c.req("POST", "login", wrong, 401)
		a.limits = map[string]limiter{} // each guess from a fresh address
	}
	// Past ten tries the account refuses even the right password, without hashing it.
	right := map[string]string{"Email": "ana@example.com", "Password": "correct horse battery staple"}
	c.req("POST", "login", right, 429)
	// Another account is unaffected.
	(&client{t: t, a: a}).signup("ben")
	c.req("POST", "login", map[string]string{"Email": "ben@example.com", "Password": "correct horse battery staple"}, 200)
	// The window ends, and a good login clears the count.
	u := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
	a.fails[u.ID] = limiter{10, time.Now().Add(-time.Second)}
	c.req("POST", "login", right, 200)
	if _, ok := a.fails[u.ID]; ok {
		t.Fatal("a successful login did not clear the failure count")
	}
}

func TestPostsMustSayWhereTheyCameFrom(t *testing.T) {
	a := setup(t)
	post := func(headers map[string]string) int {
		r := httptest.NewRequest("POST", "/api/logout", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		return w.Code
	}
	for name, h := range map[string]map[string]string{
		"no headers":        {},
		"same-site":         {"Sec-Fetch-Site": "same-site"},
		"cross-site":        {"Sec-Fetch-Site": "cross-site"},
		"foreign origin":    {"Origin": "https://evil.test", "Sec-Fetch-Site": "same-origin"},
		"opaque origin":     {"Origin": "null"},
		"origin, cross":     {"Origin": "http://example.com", "Sec-Fetch-Site": "cross-site"},
		"sibling subdomain": {"Origin": "http://evil.example.com", "Sec-Fetch-Site": "same-site"},
	} {
		if code := post(h); code != 403 {
			t.Errorf("%s: got %d, want 403", name, code)
		}
	}
	for name, h := range map[string]map[string]string{
		"same-origin": {"Sec-Fetch-Site": "same-origin"},
		"typed in":    {"Sec-Fetch-Site": "none"},
		"our origin":  {"Origin": "http://example.com"},
	} {
		if code := post(h); code == 403 {
			t.Errorf("%s was refused", name)
		}
	}
}

func TestResetRecoversAnAddressSquatterHolds(t *testing.T) {
	a, _ := mailSetup(t)
	a.admins = map[string]bool{"ana@example.com": true}
	// Someone signs up with the administrator's address first, and can never confirm it.
	squatter := &client{t: t, a: a}
	squatter.signup("ana")
	u := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
	if a.isAdmin(u) {
		t.Fatal("an unconfirmed address was treated as administrator")
	}
	// The real owner takes the account over through a password reset, which proves the inbox.
	(&client{t: t, a: a}).req("POST", "reset/request", map[string]string{"Email": "ana@example.com"}, 200)
	var tok string
	for _, m := range a.state.Outbox {
		if i := strings.Index(m.Body, "#reset/"); i >= 0 {
			tok = strings.Fields(m.Body[i+len("#reset/"):])[0]
		}
	}
	owner := &client{t: t, a: a}
	owner.req("POST", "reset", map[string]string{"Token": tok, "Password": "the owner's new password"}, 200)
	if !a.isAdmin(u) {
		t.Fatal("the owner could not become administrator after resetting")
	}
	squatter.req("GET", "me", nil, 401)
}

func TestHashingDoesNotHoldTheLock(t *testing.T) {
	a := setup(t)
	(&client{t: t, a: a}).signup("ana")
	done := make(chan bool)
	go func() {
		(&client{t: t, a: a}).req("POST", "login", map[string]string{"Email": "ana@example.com", "Password": "correct horse battery staple"}, 200)
		done <- true
	}()
	// While a login is hashing, other requests must still get the lock straight away.
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	(&client{t: t, a: a}).req("GET", "config", nil, 200)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("a request waited %v behind password hashing", d)
	}
	<-done
}
