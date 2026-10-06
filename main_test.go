package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
