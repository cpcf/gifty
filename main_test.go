package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type client struct {
	t      *testing.T
	a      *App
	cookie *http.Cookie
}

func (c *client) req(method, path string, data any, status int) map[string]any {
	c.t.Helper()
	var b bytes.Buffer
	if data != nil {
		json.NewEncoder(&b).Encode(data)
	}
	r := httptest.NewRequest(method, "/api/"+path, &b)
	r.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.a.handler().ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	for _, k := range w.Result().Cookies() {
		c.cookie = k
	}
	var result map[string]any
	if w.Body.Bytes()[0] == '{' {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			c.t.Fatal(err)
		}
	}
	return result
}
func (c *client) signup(name string) string {
	return c.req("POST", "signup", map[string]string{"Name": name, "Email": name + "@example.com", "Password": "correct horse battery staple"}, 200)["id"].(string)
}
func setup(t *testing.T) *App {
	t.Helper()
	a, err := openApp(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestExchangeLifecycleAndPrivacy(t *testing.T) {
	a := setup(t)
	owner := &client{t: t, a: a}
	one := &client{t: t, a: a}
	two := &client{t: t, a: a}
	stranger := &client{t: t, a: a}
	ownerID := owner.signup("Alex")
	oneID := one.signup("Bea")
	two.signup("Charlie")
	stranger.signup("Dana")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Winter gifts", "Date": "2099-12-20", "Budget": "25.50", "Currency": "GBP", "Note": "At our place"}, 200)
	id := e["id"].(string)
	code := e["invite"].(string)
	path := "exchanges/" + id
	owner.req("POST", path+"/draw", map[string]string{}, 400)
	stranger.req("GET", path, nil, 404)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	two.req("POST", "join", map[string]string{"Code": code}, 200)
	private := one.req("GET", path, nil, 200)
	if _, ok := private["invite"]; ok {
		t.Fatal("non-owner received invite management token")
	}
	if len(private["members"].([]any)) != 3 {
		t.Fatal("join was not idempotent")
	}
	one.req("POST", path+"/draw", map[string]string{}, 403)
	one.req("POST", "wishes", map[string]string{"title": "Blue mug", "url": "https://example.com/mug", "note": "Large, please"}, 200)
	drawn := owner.req("POST", path+"/draw", map[string]string{}, 200)
	if drawn["drawn"] != true {
		t.Fatal("draw not recorded")
	}
	for _, c := range []*client{owner, one, two} {
		v := c.req("GET", path, nil, 200)
		if v["recipient"] == nil {
			t.Fatal("missing recipient")
		}
		for _, key := range []string{"assignments", "invite", "email", "hash", "salt"} {
			if _, ok := v[key]; ok {
				t.Fatalf("leaked %s", key)
			}
		}
	}
	for from, to := range a.state.Exchanges[id].Assignments {
		if from == to {
			t.Fatal("self draw")
		}
	}
	one.req("POST", path+"/leave", map[string]string{}, 400)
	owner.req("POST", path+"/remove", map[string]string{"ID": oneID}, 400)
	owner.req("POST", path+"/draw", map[string]string{}, 400)
	stranger.req("POST", "join", map[string]string{"Code": code}, 404)
	owner.req("POST", path+"/edit", map[string]string{"Name": "changed"}, 400)
	reopened, err := openApp(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Exchanges[id].Assignments[ownerID] != a.state.Exchanges[id].Assignments[ownerID] {
		t.Fatal("draw not durable")
	}
	owner.a = reopened
	owner.req("GET", "me", nil, 200)
	owner.req("POST", path+"/archive", map[string]string{}, 200)
	owner.req("POST", path+"/rotate", map[string]string{}, 400)
	owner.req("POST", "logout", map[string]string{}, 200)
	owner.req("GET", "me", nil, 401)
}
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
func TestMembershipAndInviteRotation(t *testing.T) {
	a := setup(t)
	o := &client{t: t, a: a}
	b := &client{t: t, a: a}
	o.signup("Owner")
	bid := b.signup("Guest")
	e := o.req("POST", "exchanges", map[string]string{"Name": "Friends", "Date": "2099-01-01", "Budget": "10", "Currency": "EUR"}, 200)
	path := "exchanges/" + e["id"].(string)
	code := e["invite"].(string)
	if inv := b.req("GET", "invite/"+code, nil, 200); inv["organiser"] != "Owner" || inv["people"] != float64(1) {
		t.Fatalf("invite = %v", inv)
	}
	b.req("POST", "join", map[string]string{"Code": code}, 200)
	b.req("POST", path+"/leave", map[string]string{}, 200)
	b.req("GET", path, nil, 404)
	b.req("POST", "join", map[string]string{"Code": code}, 200)
	o.req("POST", path+"/remove", map[string]string{"ID": bid}, 200)
	b.req("GET", path, nil, 404)
	o.req("POST", path+"/rotate", map[string]string{}, 200)
	b.req("GET", "invite/"+code, nil, 404)
	b.req("POST", "join", map[string]string{"Code": code}, 404)
}
func TestDrawProperties(t *testing.T) {
	for n := 3; n <= 100; n++ {
		ids := []string{}
		for i := 0; i < n; i++ {
			ids = append(ids, fmt.Sprint(i))
		}
		for k := 0; k < 20; k++ {
			d := draw(ids)
			seen := map[string]bool{}
			for _, id := range ids {
				to := d[id]
				if to == id || seen[to] || to == "" {
					t.Fatalf("invalid draw for %d members", n)
				}
				seen[to] = true
			}
		}
	}
}
func TestFailedSaveRollsBack(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	c.signup("Rollback")
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a.path = filepath.Join(block, "data.json")
	c.req("POST", "wishes", map[string]string{"title": "Should not save"}, 500)
	v := c.req("GET", "me", nil, 200)
	if len(v["wishes"].([]any)) != 0 {
		t.Fatal("failed transaction changed memory")
	}
}
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
func TestWishesScopedToExchanges(t *testing.T) {
	a := setup(t)
	cs := map[string]*client{}
	for _, n := range []string{"ana", "ben", "cat"} {
		cs[n] = &client{t: t, a: a}
		cs[n].signup(n)
	}
	mk := func(name string) string {
		e := cs["ana"].req("POST", "exchanges", map[string]string{"Name": name, "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
		for _, n := range []string{"ben", "cat"} {
			cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
		}
		return e["id"].(string)
	}
	work, family := mk("Work"), mk("Family")
	other := cs["cat"].req("POST", "exchanges", map[string]string{"Name": "Cat's", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)["id"].(string)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Not in that exchange", "Exchanges": []string{other}}, 400)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Anything", "Exchanges": []string{}}, 200)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Mug", "Exchanges": []string{work, work}}, 200)
	me := cs["ben"].req("POST", "wishes", map[string]any{"Title": "Scarf", "Exchanges": []string{family}}, 200)
	if got := me["wishes"].([]any)[1].(map[string]any)["exchanges"].([]any); len(got) != 1 {
		t.Fatalf("duplicate exchange kept: %v", got)
	}
	ben := a.byToken(func(u *User) bool { return u.Email == "ben@example.com" }).ID
	titles := func(ex string) []string {
		var out []string
		for _, w := range wishesFor(a.state.Users[ben], a.state.Exchanges[ex]) {
			out = append(out, w.Title)
			if w.Exchanges != nil {
				t.Fatal("recipient view reveals the giver's other exchanges")
			}
		}
		return out
	}
	if got := titles(work); !slices.Equal(got, []string{"Anything", "Mug"}) {
		t.Fatalf("work sees %v", got)
	}
	if got := titles(family); !slices.Equal(got, []string{"Anything", "Scarf"}) {
		t.Fatalf("family sees %v", got)
	}
	// The draw view uses the same filter.
	cs["ana"].req("POST", "exchanges/"+family+"/draw", nil, 200)
	for _, n := range []string{"ana", "cat"} {
		r := cs[n].req("GET", "exchanges/"+family, nil, 200)["recipient"].(map[string]any)
		if r["name"] == "ben" {
			for _, w := range r["wishes"].([]any) {
				if w.(map[string]any)["title"] == "Mug" {
					t.Fatal("work-only idea shown in the family exchange")
				}
			}
		}
	}
}
func TestAccessGate(t *testing.T) {
	a := setup(t)
	a.access = digest("gifty-access:" + "open sesame")
	c := &client{t: t, a: a}
	cfg := c.req("GET", "config", nil, 200)
	if cfg["gated"] != true || cfg["access"] != false {
		t.Fatalf("config %v", cfg)
	}
	signup := map[string]string{"Name": "ana", "Email": "ana@example.com", "Password": "correct horse battery staple"}
	c.req("POST", "signup", signup, 403)
	c.req("POST", "access", map[string]string{"Code": "wrong"}, 403)
	c.req("POST", "access", map[string]string{"Code": " open sesame "}, 200)
	if c.cookie.Name != "gifty_access" || strings.Contains(c.cookie.Value, "sesame") || !c.cookie.HttpOnly {
		t.Fatalf("access cookie %+v", c.cookie)
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
	// Existing accounts can always sign in.
	(&client{t: t, a: a}).req("POST", "login", map[string]string{"Email": "ana@example.com", "Password": "correct horse battery staple"}, 200)
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
func TestAbuseLimits(t *testing.T) {
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	c := &client{t: t, a: a}
	c.signup("ana")
	u := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
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
	// The limiter tracks a bounded number of addresses.
	for i := len(a.limits); i < 10000; i++ {
		a.limits[fmt.Sprint("10.0.", i)] = limiter{1, time.Now().Add(time.Hour)}
	}
	if a.allow("203.0.113.1") {
		t.Fatal("limiter grew past its cap")
	}
}

func TestAccountDeletion(t *testing.T) {
	a := setup(t)
	owner, one, two, loner := &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}
	owner.signup("Alex")
	oneID := one.signup("Bea")
	two.signup("Charlie")
	loner.signup("Dana")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Winter", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	id, code := e["id"].(string), e["invite"].(string)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	two.req("POST", "join", map[string]string{"Code": code}, 200)
	// Wrong password changes nothing.
	one.req("POST", "account/delete", map[string]string{"Password": "not the password"}, 403)
	// The organiser must archive an exchange that has other people in it.
	owner.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 400)
	// Before the draw a member just disappears.
	one.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	if a.state.Users[oneID] != nil || len(a.state.Exchanges[id].Members) != 2 {
		t.Fatal("member was not removed")
	}
	one.req("GET", "me", nil, 401)
	// Someone alone in an exchange takes it with them.
	solo := loner.req("POST", "exchanges", map[string]string{"Name": "Solo", "Date": "2099-12-20", "Budget": "5", "Currency": "GBP"}, 200)["id"].(string)
	loner.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	if a.state.Exchanges[solo] != nil {
		t.Fatal("solo exchange survived its only member")
	}
	// After the draw a deleted member stays on as an empty account.
	three := &client{t: t, a: a}
	threeID := three.signup("Eve")
	three.req("POST", "join", map[string]string{"Code": code}, 200)
	owner.req("POST", "exchanges/"+id+"/draw", map[string]string{}, 200)
	three.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	u := a.state.Users[threeID]
	if u == nil || u.Name != deletedName || u.Email != "" || u.Hash != "" || len(a.state.Exchanges[id].Members) != 3 {
		t.Fatalf("expected an emptied account, got %+v", u)
	}
	owner.req("GET", "exchanges/"+id, nil, 200)
	(&client{t: t, a: a}).req("POST", "login", map[string]string{"Email": "", "Password": "correct horse battery staple"}, 401)
	// The address can be used again.
	(&client{t: t, a: a}).signup("Eve")
}

func TestAdmin(t *testing.T) {
	a := setup(t)
	a.admins = map[string]bool{"boss@example.com": true}
	boss, member := &client{t: t, a: a}, &client{t: t, a: a}
	bossID := boss.signup("boss")
	memberID := member.signup("Fran")
	// Not confirmed yet: no admin powers.
	if boss.req("GET", "me", nil, 200)["admin"] != false {
		t.Fatal("unconfirmed address got admin")
	}
	boss.req("GET", "admin/overview", nil, 404)
	a.state.Users[bossID].Verified = true
	if boss.req("GET", "me", nil, 200)["admin"] != true {
		t.Fatal("admin not recognised")
	}
	member.req("GET", "admin/overview", nil, 404)
	member.req("POST", "admin/users/delete", map[string]string{"ID": bossID}, 404)
	o := boss.req("GET", "admin/overview", nil, 200)
	if len(o["users"].([]any)) != 2 {
		t.Fatal("overview should list both accounts")
	}
	boss.req("POST", "admin/users/delete", map[string]string{"ID": bossID}, 400)
	e := member.req("POST", "exchanges", map[string]string{"Name": "Fran’s", "Date": "2099-12-20", "Budget": "5", "Currency": "GBP"}, 200)
	id := e["id"].(string)
	boss.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	boss.req("POST", "admin/users/delete", map[string]string{"ID": memberID}, 400)
	boss.req("POST", "admin/exchanges/delete", map[string]string{"ID": id}, 200)
	boss.req("POST", "admin/users/delete", map[string]string{"ID": memberID}, 200)
	if a.state.Users[memberID] != nil || len(a.state.Exchanges) != 0 {
		t.Fatal("admin deletion left data behind")
	}
}

func TestExchangeDeletion(t *testing.T) {
	a := setup(t)
	owner, member, stranger := &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}
	owner.signup("Alex")
	memberID := member.signup("Bea")
	stranger.signup("Cara")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Old gifts", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	id, code := e["id"].(string), e["invite"].(string)
	path := "exchanges/" + id
	member.req("POST", "join", map[string]string{"Code": code}, 200)
	// An idea scoped to the exchange loses that scope when the exchange goes.
	member.req("POST", "wishes", map[string]any{"title": "Blue mug", "exchanges": []string{id}}, 200)
	// Deleting is for organisers, and only once the exchange is archived.
	stranger.req("POST", path+"/delete", map[string]string{}, 404)
	member.req("POST", path+"/delete", map[string]string{}, 403)
	owner.req("POST", path+"/delete", map[string]string{}, 400)
	owner.req("POST", path+"/archive", map[string]string{}, 200)
	member.req("POST", path+"/delete", map[string]string{}, 403)
	owner.req("POST", path+"/delete", map[string]string{}, 200)
	owner.req("GET", path, nil, 404)
	member.req("GET", path, nil, 404)
	if a.state.Exchanges[id] != nil {
		t.Fatal("exchange not deleted")
	}
	for _, w := range a.state.Users[memberID].Wishes {
		if len(w.Exchanges) != 0 {
			t.Fatal("wish scope not cleared")
		}
	}
	// The deletion is durable.
	reopened, err := openApp(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Exchanges[id] != nil {
		t.Fatal("deleted exchange persisted")
	}
}
