package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type problem struct {
	status  int
	message string
}

func (p problem) Error() string { return p.message }

func bad(s string) error { return problem{400, s} }

func readJSON(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return problem{415, "Send JSON data."}
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return problem{413, "That form is too large."}
		}
		return bad("Check the form and try again.")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad("Send one form at a time.")
	}
	return nil
}

// buffered holds a response in memory, so it can be built under the state lock and sent once the lock is free.
// Headers go straight to the real response's header map, which nothing sends before flush.
type buffered struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (b *buffered) Header() http.Header { return b.header }

func (b *buffered) WriteHeader(code int) {
	if b.code == 0 {
		b.code = code
	}
}

func (b *buffered) Write(p []byte) (int, error) {
	b.WriteHeader(200)
	return b.body.Write(p)
}

func (b *buffered) flush(w http.ResponseWriter) {
	w.WriteHeader(max(b.code, 200))
	w.Write(b.body.Bytes())
}

func send(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// sameOrigin accepts a POST that names our own origin, or that the browser says came from the page itself
// (or from no page, like a bookmark). A POST that says nothing, or "same-site" or "cross-site", is refused.
func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		p, err := url.Parse(o)
		return err == nil && p.Host == r.Host && r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "same-origin" || site == "none"
}

func (a *application) serveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// An idea can carry a photo, so its form gets a larger body than everything else.
	limit := int64(32768)
	if r.Method == "POST" && strings.TrimPrefix(r.URL.Path, "/api/") == "wishes" {
		limit = 4 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if r.Method != "GET" && r.Method != "POST" {
		send(w, 405, map[string]string{"error": "Method not allowed."})
		return
	}
	// Browsers always say where a POST came from. Mail providers' one-click unsubscribe is the one
	// legitimate cross-site POST, and its token is its credential.
	if r.Method == "POST" && !strings.HasPrefix(r.URL.Path, "/api/unsubscribe/") && !sameOrigin(r) {
		a.sec(r, "origin_refused")
		send(w, 403, map[string]string{"error": "Open Gifty and try again."})
		return
	}
	if path := strings.TrimPrefix(r.URL.Path, "/api/"); r.Method == "POST" && isAuthPath(path) {
		var ok bool
		if r, ok = a.prepare(w, r, path); !ok {
			return
		}
	}
	// The response is built under the state lock but sent after it is released, so a client that reads slowly
	// holds up nobody else.
	out := &buffered{header: w.Header()}
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.serveLocked(out, r)
	}()
	out.flush(w)
}

// Reads are rate limited too, so nobody can keep the lock busy with them. Looking up an invitation or a friend
// link scans every exchange or account, so those get a tighter budget of their own.
const (
	maxReads   = 2000
	maxLookups = 60
)

func isLookup(path string) bool {
	return strings.HasPrefix(path, "invite/") || strings.HasPrefix(path, "friend-link/") || strings.HasPrefix(path, "friend/")
}

// serveLocked is the part of serveAPI that runs under the state lock.
func (a *application) serveLocked(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if r.Method == "POST" && !isAuthPath(path) && !a.allow("write|"+a.clientIP(r), 300) {
		a.sec(r, "write_rate_limited")
		send(w, 429, map[string]string{"error": "Too many changes at once. Try again in a few minutes."})
		return
	}
	if r.Method == "GET" && (!a.allow("read|"+a.clientIP(r), maxReads) || isLookup(path) && !a.allow("lookup|"+a.clientIP(r), maxLookups)) {
		a.sec(r, "read_rate_limited")
		send(w, 429, map[string]string{"error": "Too many requests. Try again in a few minutes."})
		return
	}
	// Roll back all in-memory changes when persistence fails. Reads change nothing, so only writes pay for a snapshot.
	var before []byte
	if r.Method == "POST" {
		before, _ = json.Marshal(a.state)
		a.dropped = false
	}
	result, err := a.dispatch(w, r)
	if err == nil && r.Method == "POST" {
		err = a.save()
		if err != nil {
			var restored stateData
			if json.Unmarshal(before, &restored) == nil {
				a.state = restored
			}
		} else if a.dropped {
			a.sweepPhotos()
		}
	}
	if err != nil {
		code := 500
		msg := "We couldn’t save that. Please try again."
		var p problem
		if errors.As(err, &p) {
			code = p.status
			msg = p.message
		} else {
			log.Printf("request failed: %v", err)
		}
		send(w, code, map[string]string{"error": msg})
		return
	}
	send(w, 200, result)
}
