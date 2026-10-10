package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// party is an exchange of named people, each with a signed-in client.
type party struct {
	a   *application
	ids map[string]string
	c   map[string]*client
	ex  string
}

func newParty(t *testing.T, names ...string) *party {
	t.Helper()
	a := setup(t)
	p := &party{a: a, ids: map[string]string{}, c: map[string]*client{}}
	for _, n := range names {
		p.c[n] = &client{t: t, a: a}
		p.ids[n] = p.c[n].signup(n)
	}
	e := p.c[names[0]].req("POST", "exchanges", map[string]string{"Name": "Winter, gifts", "Date": "2099-12-20", "Budget": "25", "Currency": "GBP", "Note": "Bring a hat; wrap it"}, 200)
	p.ex = e["id"].(string)
	for _, n := range names[1:] {
		p.c[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	return p
}

func (p *party) path(action string) string {
	if action == "" {
		return "exchanges/" + p.ex
	}
	return "exchanges/" + p.ex + "/" + action
}

func (p *party) name(id string) string {
	for n, v := range p.ids {
		if v == id {
			return n
		}
	}
	return ""
}

// assign fixes who draws whom, so a test doesn't depend on the random draw.
func (p *party) assign(as map[string]string) {
	m := map[string]string{}
	for g, r := range as {
		m[p.ids[g]] = p.ids[r]
	}
	p.a.state.Exchanges[p.ex].Assignments = m
}

// idOf is the signed-in user's ID.
func (c *client) idOf() string { return c.req("GET", "me", nil, 200)["id"].(string) }

// raw sends a body exactly as given, for the requests req can't make: broken JSON, unknown fields, other methods.
func raw(c *client, method, path, contentType, body string, status int) {
	c.t.Helper()
	r := httptest.NewRequest(method, "/api/"+path, bytes.NewBufferString(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.a.handler().ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s %q: got %d want %d: %s", method, path, body, w.Code, status, w.Body.String())
	}
}

type person struct {
	c  *client
	id string
}

func people(t *testing.T, a *application, names ...string) map[string]*person {
	t.Helper()
	out := map[string]*person{}
	for _, n := range names {
		c := &client{t: t, a: a}
		out[n] = &person{c, c.signup(n)}
	}
	return out
}

// makeFriends has x open y's friend link and y accept.
func makeFriends(t *testing.T, x, y *person) {
	t.Helper()
	code := y.c.req("GET", "friends", nil, 200)["code"].(string)
	x.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	y.c.req("POST", "friends/accept", map[string]string{"ID": x.id}, 200)
}

func idea(p *person, title string, extra map[string]any) string {
	body := map[string]any{"title": title, "exchanges": []string{}}
	for k, v := range extra {
		body[k] = v
	}
	p.c.req("POST", "wishes", body, 200)
	for _, w := range p.c.req("GET", "me", nil, 200)["wishes"].([]any) {
		if w.(map[string]any)["title"] == title {
			return w.(map[string]any)["id"].(string)
		}
	}
	p.c.t.Fatalf("idea %q not saved", title)
	return ""
}

func wishTitles(r map[string]any) map[string]string {
	out := map[string]string{}
	for _, w := range r["wishes"].([]any) {
		m := w.(map[string]any)
		claim, _ := m["claim"].(string)
		out[m["title"].(string)] = claim
	}
	return out
}

type outbox struct{ sent []string }

func (o *outbox) send(to string, msg []byte) error {
	o.sent = append(o.sent, string(msg))
	return nil
}

func mailSetup(t *testing.T) (*application, *outbox) {
	t.Helper()
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	return a, o
}

func linkToken(t *testing.T, body, route string) string {
	t.Helper()
	m := regexp.MustCompile(`#` + route + `/([A-Za-z0-9_-]+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s link in %q", route, body)
	}
	return m[1]
}

func lastMail(t *testing.T, a *application, to string) queuedMail {
	t.Helper()
	for i := len(a.state.Outbox) - 1; i >= 0; i-- {
		if a.state.Outbox[i].To == to {
			return a.state.Outbox[i]
		}
	}
	t.Fatalf("no email to %s", to)
	return queuedMail{}
}

func future(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02") }

func rem(spec ...any) []map[string]any {
	out := []map[string]any{}
	for i := 0; i < len(spec); i += 2 {
		out = append(out, map[string]any{"n": spec[i], "unit": spec[i+1]})
	}
	return out
}

// reminderSetup makes a three-person exchange 40 days out and returns clients by name and its ID.
func reminderSetup(t *testing.T) (*application, map[string]*client, string) {
	a, _ := mailSetup(t)
	cs := map[string]*client{}
	for _, n := range []string{"ana", "ben", "cat"} {
		cs[n] = &client{t: t, a: a}
		cs[n].signup(n)
	}
	for _, u := range a.state.Users {
		u.Verified = true
	}
	e := cs["ana"].req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": future(40), "Budget": "20", "Currency": "GBP"}, 200)
	for _, n := range []string{"ben", "cat"} {
		cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	return a, cs, e["id"].(string)
}

func sentOn(a *application, at time.Time) map[string]bool {
	a.state.Outbox = nil
	a.remind(at)
	out := map[string]bool{}
	for _, m := range a.state.Outbox {
		out[strings.TrimSuffix(m.To, "@example.com")] = true
	}
	return out
}

type client struct {
	t      *testing.T
	a      *application
	cookie *http.Cookie // the session
	access *http.Cookie // the access-gate cookie
}

func (c *client) req(method, path string, data any, status int) map[string]any {
	c.t.Helper()
	var b bytes.Buffer
	if data != nil {
		json.NewEncoder(&b).Encode(data)
	}
	r := httptest.NewRequest(method, "/api/"+path, &b)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.access != nil {
		r.AddCookie(c.access)
	}
	w := httptest.NewRecorder()
	c.a.handler().ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	for _, k := range w.Result().Cookies() {
		if k.Name == "gifty_access" {
			c.access = k
		} else {
			c.cookie = k
		}
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

func setup(t *testing.T) *application {
	t.Helper()
	a, err := openApp(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// PNG magic bytes, enough for the photo type check.
var testPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}

func dataURI(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// get fetches a non-API path, like /image/, with the client's session.
func (c *client) get(path string, status int) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.a.handler().ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("GET %s: got %d want %d: %s", path, w.Code, status, w.Body.String())
	}
	return w
}
