package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// photoFiles lists the files in a's photos directory.
func photoFiles(t *testing.T, a *application) []string {
	t.Helper()
	entries, err := os.ReadDir(a.photoDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestPhotosLiveOutsideTheDataFile(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana")
	ana := p["Ana"]
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("x"), 300)...)
	mug := idea(ana, "Mug", map[string]any{"photos": []string{dataURI("image/jpeg", jpeg)}})
	// The same photo twice is stored once.
	cup := idea(ana, "Cup", map[string]any{"photos": []string{dataURI("image/jpeg", jpeg)}})
	data, err := os.ReadFile(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), base64.StdEncoding.EncodeToString(jpeg)[:40]) || strings.Contains(string(data), "data:image") {
		t.Fatal("photo bytes are in the data file")
	}
	if files := photoFiles(t, a); len(files) != 1 {
		t.Fatalf("photo files = %v, want one", files)
	}
	if w := ana.c.get("/image/"+mug+"/0", 200); !bytes.Equal(w.Body.Bytes(), jpeg) {
		t.Fatal("stored photo came back changed")
	}
	// A file is removed once nothing refers to it, and only then.
	ana.c.req("POST", "wishes/"+mug, nil, 200)
	if len(photoFiles(t, a)) != 1 {
		t.Fatal("a photo still on another idea was removed")
	}
	ana.c.get("/image/"+cup+"/0", 200)
	ana.c.req("POST", "wishes", map[string]any{"id": cup, "title": "Cup", "exchanges": []string{}, "photos": []string{}}, 200)
	if files := photoFiles(t, a); len(files) != 0 {
		t.Fatalf("unused photo files kept: %v", files)
	}
	// Deleting an account removes its photos.
	idea(ana, "Pan", map[string]any{"photos": []string{dataURI("image/png", testPNG)}})
	ana.c.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	if files := photoFiles(t, a); len(files) != 0 {
		t.Fatalf("a deleted account's photos are kept: %v", files)
	}
	// A damaged or swapped file is never served as something else.
	ben := people(t, a, "Ben")["Ben"]
	hat := idea(ben, "Hat", map[string]any{"photos": []string{dataURI("image/png", testPNG)}})
	f := a.state.Users[ben.id].Wishes[0].Photos[0].File
	if err := os.WriteFile(filepath.Join(a.photoDir(), f), []byte("<script>alert(1)</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	ben.c.get("/image/"+hat+"/0", 404)
}

func TestPhotoBudgets(t *testing.T) {
	a := setup(t)
	ana := people(t, a, "Ana")["Ana"]
	defer func(each, site int) { maxImageTotal, maxPhotoTotal = each, site }(maxImageTotal, maxPhotoTotal)
	png2 := append(append([]byte{}, testPNG...), 1)
	maxPhotoTotal = len(testPNG) + 1
	idea(ana, "One", map[string]any{"photos": []string{dataURI("image/png", testPNG)}})
	// The site is out of room for new photos, but a photo already stored can be reused.
	ana.c.req("POST", "wishes", map[string]any{"title": "Two", "exchanges": []string{}, "photos": []string{dataURI("image/png", png2)}}, 507)
	maxPhotoTotal = 1 << 30
	maxImageTotal = len(testPNG) + len(png2) - 1
	ana.c.req("POST", "wishes", map[string]any{"title": "Two", "exchanges": []string{}, "photos": []string{dataURI("image/png", png2)}}, 400)
}

func TestPhotosMoveOutOfOlderDataFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	legacy := `{"Users":{"u1":{"ID":"u1","Name":"Old","Email":"old@example.com","Wishes":[{"id":"w1","title":"Mug","url":"","note":"","price":"","exchanges":null,"images":["` + dataURI("image/png", testPNG) + `","data:image/png;base64,bm90IGEgcGhvdG8="]}]}},"Exchanges":{},"Sessions":{}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := openApp(path)
	if err != nil {
		t.Fatal(err)
	}
	w := a.state.Users["u1"].Wishes[0]
	if len(w.Images) != 0 || len(w.Photos) != 1 || w.Photos[0].Type != "image/png" || w.Photos[0].Size != len(testPNG) {
		t.Fatalf("photos not moved: %+v", w)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "data:image") {
		t.Fatal("data file still holds photo data")
	}
	a.state.Sessions[digest("s")] = session{"u1", time.Now().Add(time.Hour)}
	c := &client{t: t, a: a, cookie: &http.Cookie{Name: "gifty_session", Value: "s"}}
	if got := c.get("/image/w1/0", 200); !bytes.Equal(got.Body.Bytes(), testPNG) {
		t.Fatal("moved photo changed")
	}
}

// stalled is a client that stops reading the response once it starts arriving.
type stalled struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
}

func (s *stalled) Header() http.Header { return s.header }
func (s *stalled) WriteHeader(int)     {}
func (s *stalled) Write(p []byte) (int, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-s.release
	return len(p), nil
}

func TestSlowReadersDoNotHoldTheLock(t *testing.T) {
	a := setup(t)
	ana := people(t, a, "Ana")["Ana"]
	mug := idea(ana, "Mug", map[string]any{"photos": []string{dataURI("image/png", testPNG)}})
	for _, path := range []string{"/api/me", "/image/" + mug + "/0"} {
		s := &stalled{header: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(ana.c.cookie)
		go a.handler().ServeHTTP(s, r)
		<-s.started
		done := make(chan bool)
		go func() {
			a.mu.Lock()
			a.mu.Unlock()
			done <- true
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("a stalled reader of %s holds the state lock", path)
		}
		close(s.release)
	}
}

func TestReadsAreRateLimited(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	for i := 0; i < maxLookups; i++ {
		c.req("GET", "friend-link/nope", nil, 404)
	}
	c.req("GET", "friend-link/nope", nil, 429)
	c.req("GET", "config", nil, 200) // other reads have their own, larger budget
	a.limits["read|192.0.2.1"] = limiter{maxReads, time.Now().Add(time.Hour)}
	c.req("GET", "config", nil, 429)
	(&client{t: t, a: a}).get("/image/x/0", 429)
}

func TestGroupGiftWithoutItsPersonStillLoads(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben")
	makeFriends(t, p["Ben"], p["Ana"])
	g := p["Ana"].c.req("POST", "exchanges", map[string]string{"Name": "Gift", "Date": "2099-12-20", "Budget": "30", "Currency": "GBP", "Kind": "group", "For": p["Ben"].id}, 200)
	a.state.Exchanges[g["id"].(string)].For = "missing" // as in a damaged data file
	if e := p["Ana"].c.req("GET", "exchanges/"+g["id"].(string), nil, 200); e["for"] != nil {
		t.Fatalf("for = %v", e["for"])
	}
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
