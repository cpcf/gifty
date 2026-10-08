package main

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// photoFiles lists the files in a's photos directory.
func photoFiles(t *testing.T, a *App) []string {
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
	a.state.Sessions[digest("s")] = Session{"u1", time.Now().Add(time.Hour)}
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
