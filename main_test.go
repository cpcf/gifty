package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
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
			d, _ := draw(ids, nil)
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
		for _, w := range wishesFor(a.state.Users[ben], a.state.Exchanges[ex], "") {
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

func TestWishPhotos(t *testing.T) {
	a := setup(t)
	cs := map[string]*client{}
	ids := map[string]string{}
	for _, n := range []string{"ana", "ben", "cat", "dana"} {
		cs[n] = &client{t: t, a: a}
		ids[n] = cs[n].signup(n)
	}
	ana, ben, cat, dana := cs["ana"], cs["ben"], cs["cat"], cs["dana"]
	// Two exchanges with fixed assignments: in the first cat buys for ana, in the second ben does.
	mk := func(name string) string {
		e := ana.req("POST", "exchanges", map[string]string{"Name": name, "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
		for _, n := range []string{"ben", "cat"} {
			cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
		}
		return e["id"].(string)
	}
	first, second := mk("First"), mk("Second")
	a.state.Exchanges[first].Assignments = map[string]string{ids["ana"]: ids["ben"], ids["ben"]: ids["cat"], ids["cat"]: ids["ana"]}
	a.state.Exchanges[second].Assignments = map[string]string{ids["ana"]: ids["cat"], ids["cat"]: ids["ben"], ids["ben"]: ids["ana"]}

	// A photo is stored but never serialised: clients get a same-origin URL.
	v := ana.req("POST", "wishes", map[string]any{"title": "Mug", "price": "about £8", "photos": []string{dataURI("image/png", testPNG)}}, 200)
	wish := v["wishes"].([]any)[0].(map[string]any)
	id := wish["id"].(string)
	if wish["price"] != "about £8" {
		t.Fatalf("price not saved: %v", wish)
	}
	if wish["images"].([]any)[0] != "/image/"+id+"/0" {
		t.Fatalf("photo not exposed as a URL: %v", wish)
	}
	b, _ := json.Marshal(ana.req("GET", "me", nil, 200))
	if strings.Contains(string(b), "base64") {
		t.Fatal("photo data leaked into an API response")
	}

	// The owner can fetch it; a stranger and an anonymous caller cannot.
	w := ana.get("/image/"+id+"/0", 200)
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), testPNG) {
		t.Fatal("photo bytes changed on the way out")
	}
	dana.get("/image/"+id+"/0", 404)
	(&client{t: t, a: a}).get("/image/"+id+"/0", 401)

	// The people buying for ana can fetch it, each in the exchange that assigns them; nobody else can.
	cat.get("/image/"+id+"/0", 200) // assigned to ana in the first exchange
	ben.get("/image/"+id+"/0", 200) // assigned to ana in the second
	// An idea scoped to one exchange is a photo only that exchange's giver can fetch.
	v = ana.req("POST", "wishes", map[string]any{"title": "Scarf", "exchanges": []string{second}, "photos": []string{dataURI("image/png", testPNG)}}, 200)
	scoped := v["wishes"].([]any)[1].(map[string]any)["id"].(string)
	ben.get("/image/"+scoped+"/0", 200)
	cat.get("/image/"+scoped+"/0", 404)
	ana.get("/image/"+scoped+"/0", 200)
	// The recipient view carries the URL, not the data.
	r := ben.req("GET", "exchanges/"+second, nil, 200)["recipient"].(map[string]any)
	for _, x := range r["wishes"].([]any) {
		m := x.(map[string]any)
		if m["title"] == "Scarf" && m["images"].([]any)[0] != "/image/"+scoped+"/0" {
			t.Fatalf("recipient view photo: %v", m)
		}
	}

	// Editing without touching the photo keeps it; removing it drops it; a new photo replaces it.
	ana.req("POST", "wishes", map[string]any{"id": id, "title": "Mug, large"}, 200)
	ana.get("/image/"+id+"/0", 200)
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, testPNG...)
	ana.req("POST", "wishes", map[string]any{"id": id, "title": "Mug, large", "photos": []string{dataURI("image/jpeg", jpeg)}}, 200)
	w = ana.get("/image/"+id+"/0", 200)
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), jpeg) {
		t.Fatalf("replaced photo: %q %v", ct, w.Body.Bytes())
	}
	ana.req("POST", "wishes", map[string]any{"id": id, "title": "Mug, large", "photos": []string{}}, 200)
	ana.get("/image/"+id+"/0", 404)
	if me := ana.req("GET", "me", nil, 200)["wishes"].([]any)[0].(map[string]any); me["images"] != nil {
		t.Fatalf("removed photo still listed: %v", me)
	}

	// Only real image bytes of the declared type are accepted.
	for _, image := range []string{
		dataURI("text/html", []byte("<script>alert(1)</script>")),
		dataURI("image/png", jpeg),            // declared PNG, actually JPEG
		dataURI("image/png", []byte("hello")), // no image magic
		"data:image/png;base64,not base64!",
		"data:image/png;base64,",
		"data:image/png," + string(testPNG),                              // not base64
		"image/png;base64," + base64.StdEncoding.EncodeToString(testPNG), // not a data URI
	} {
		ana.req("POST", "wishes", map[string]any{"title": "Bad photo", "photos": []string{image}}, 400)
	}
	// Size limits: per photo and per person.
	defer func() { maxImageBytes, maxImageTotal = 512*1024, 8*1024*1024 }()
	maxImageBytes = 10
	ana.req("POST", "wishes", map[string]any{"title": "Too big", "photos": []string{dataURI("image/png", testPNG)}}, 400)
	maxImageBytes = 512 * 1024
	maxImageTotal = 10
	ana.req("POST", "wishes", map[string]any{"title": "Over budget", "photos": []string{dataURI("image/png", testPNG)}}, 400)
	maxImageTotal = 8 * 1024 * 1024

	// The idea form may carry a photo, so its body is allowed past the usual cap; past its own cap it is refused.
	big := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 500*1024)...)
	ana.req("POST", "wishes", map[string]any{"title": "Big photo", "photos": []string{dataURI("image/png", big)}}, 200)
	huge := "data:image/png;base64," + strings.Repeat("A", 4200*1024)
	ana.req("POST", "wishes", map[string]any{"title": "Huge photo", "photos": []string{huge}}, 413)

	// An idea holds several photos in order; entries already on the idea are kept by URL, new ones are
	// data URIs, and the list can be reordered or trimmed in one save.
	jpeg2 := append([]byte{0xFF, 0xD8, 0xFF, 0xE1}, testPNG...)
	v = ana.req("POST", "wishes", map[string]any{"title": "Set", "photos": []string{dataURI("image/png", testPNG), dataURI("image/jpeg", jpeg)}}, 200)
	var setID string
	for _, x := range v["wishes"].([]any) {
		if m := x.(map[string]any); m["title"] == "Set" {
			setID = m["id"].(string)
			if len(m["images"].([]any)) != 2 {
				t.Fatalf("two photos expected: %v", m)
			}
		}
	}
	keep1, keep0 := "/image/"+setID+"/1", "/image/"+setID+"/0"
	ana.req("POST", "wishes", map[string]any{"id": setID, "title": "Set", "photos": []string{keep1, keep0, dataURI("image/jpeg", jpeg2)}}, 200)
	if w := ana.get("/image/"+setID+"/0", 200); !bytes.Equal(w.Body.Bytes(), jpeg) {
		t.Fatal("photos not reordered")
	}
	if w := ana.get("/image/"+setID+"/2", 200); !bytes.Equal(w.Body.Bytes(), jpeg2) {
		t.Fatal("new photo not appended")
	}
	ana.req("POST", "wishes", map[string]any{"id": setID, "title": "Set", "photos": []string{"/image/" + setID + "/2"}}, 200)
	ana.get("/image/"+setID+"/1", 404)
	if w := ana.get("/image/"+setID+"/0", 200); !bytes.Equal(w.Body.Bytes(), jpeg2) {
		t.Fatal("photo not trimmed")
	}
	// Stale or foreign URLs, and too many photos, are refused.
	ana.req("POST", "wishes", map[string]any{"id": setID, "title": "Set", "photos": []string{"/image/" + setID + "/5"}}, 400)
	ana.req("POST", "wishes", map[string]any{"id": setID, "title": "Set", "photos": []string{"/image/" + id + "/0"}}, 400)
	six := []string{}
	for i := 0; i < maxImages+1; i++ {
		six = append(six, dataURI("image/png", testPNG))
	}
	ana.req("POST", "wishes", map[string]any{"title": "Too many", "photos": six}, 400)
	ana.get("/image/"+setID, 404)
	ana.get("/image/"+setID+"/x", 404)

	// The photos survive a restart with the data file.
	reopened, err := openApp(a.path)
	if err != nil {
		t.Fatal(err)
	}
	ana.a = reopened
	ana.get("/image/"+scoped+"/0", 200)
	bigID := ""
	for _, x := range ana.req("GET", "me", nil, 200)["wishes"].([]any) {
		if x.(map[string]any)["title"] == "Big photo" {
			bigID = x.(map[string]any)["id"].(string)
		}
	}
	if bigID == "" {
		t.Fatal("big photo not saved")
	}
	ana.get("/image/"+bigID+"/0", 200)
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
	u := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
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
	u := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
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

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gifty.json")
	a, err := openApp(path)
	if err != nil {
		t.Fatal(err)
	}
	// The secret lives beside the data file, never in it, and is private to the service user.
	if fi, err := os.Stat(filepath.Join(dir, "gifty.key")); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("key file %v %v", fi, err)
	}
	a.state.Users["x"] = &User{ID: "x", Name: "x"}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), string(a.key)) {
		t.Fatal("the key is in the data file")
	}
	// It survives a restart, so access cookies and unsubscribe links keep working.
	b, err := openApp(path)
	if err != nil || string(b.key) != string(a.key) {
		t.Fatalf("reopen: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "gifty.key"), []byte("short\n"), 0600)
	if _, err := openApp(path); err == nil {
		t.Fatal("a damaged key file was accepted")
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
func TestNotificationCapAndExistingEmailNotice(t *testing.T) {
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	(&client{t: t, a: a}).signup("ana")
	u := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
	u.Verified = true
	start := len(a.state.Outbox)
	for i := 0; i < 25; i++ {
		a.queue(u, true, "Reminder", "body")
	}
	if len(a.state.Outbox)-start != 20 {
		t.Fatalf("queued %d exchange emails, want 20", len(a.state.Outbox))
	}
	// Signing up again with a confirmed address is refused and tells the owner.
	before := len(a.state.Outbox)
	(&client{t: t, a: a}).req("POST", "signup", map[string]string{"Name": "x", "Email": "ana@example.com", "Password": "another long password"}, 400)
	if len(a.state.Outbox) != before+1 || !strings.Contains(a.state.Outbox[before].Subject, "tried to sign up") {
		t.Fatal("the account owner was not told")
	}
}

func TestSecurityEventsAreLoggedAndCounted(t *testing.T) {
	a := setup(t)
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	(&client{t: t, a: a}).signup("ana")
	c := &client{t: t, a: a}
	c.req("POST", "login", map[string]string{"Email": "ana@example.com", "Password": "not the password"}, 401)
	if !strings.Contains(out.String(), "security: login_failed ip=192.0.2.1") {
		t.Fatalf("a failed login was not logged: %q", out.String())
	}
	if strings.Contains(out.String(), "not the password") || strings.Contains(out.String(), "ana@example.com") {
		t.Fatal("the log names the attempt's credentials")
	}
	if a.watch.events["login_failed"] != 1 || a.watch.ips["192.0.2.1"] != 1 {
		t.Fatalf("not counted: %v %v", a.watch.events, a.watch.ips)
	}
	r := httptest.NewRequest("POST", "/api/logout", strings.NewReader("{}"))
	a.handler().ServeHTTP(httptest.NewRecorder(), r)
	if !strings.Contains(out.String(), "security: origin_refused ip=192.0.2.1") {
		t.Fatalf("a cross-site post was not logged: %q", out.String())
	}
}

func TestAlertsAndSummaries(t *testing.T) {
	a := setup(t)
	var sent []string
	a.mailer = func(to string, msg []byte) error { sent = append(sent, to+"\n"+string(msg)); return nil }
	a.alertTo, a.base, a.from = "me@example.com", "https://gifty.test", "Gifty <g@gifty.test>"
	day := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < spikeThreshold-1; i++ {
		a.watch.add(day, "login_failed", "198.51.100.7")
	}
	a.alertTick(day)
	if len(sent) != 0 {
		t.Fatal("alerted below the threshold")
	}
	a.watch.add(day, "login_failed", "198.51.100.7")
	a.alertTick(day)
	if len(sent) != 1 || !strings.HasPrefix(sent[0], "me@example.com\n") || !strings.Contains(sent[0], "198.51.100.7") {
		t.Fatalf("no alert for a burst: %q", sent)
	}
	a.alertTick(day.Add(time.Minute))
	if len(sent) != 1 {
		t.Fatal("alerted twice inside the cooldown")
	}
	// Old events fall out of the window, and the summary goes out once the date turns over, only if something happened.
	a.alertTick(day.Add(spikeWindow + time.Minute))
	if len(a.watch.recent) != 0 {
		t.Fatal("old events stayed in the window")
	}
	a.alertTick(day.Add(24 * time.Hour))
	if len(sent) != 2 || !strings.Contains(sent[1], "summary for 2026-01-05") || !strings.Contains(sent[1], "login_failed: 40") {
		t.Fatalf("no summary: %q", sent)
	}
	a.alertTick(day.Add(48 * time.Hour))
	if len(sent) != 2 {
		t.Fatal("sent a summary for a quiet day")
	}
}
