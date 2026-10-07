package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS

type User struct {
	ID     string
	Name   string
	Email  string
	Salt   string
	Hash   string
	Wishes []Wish
	// Email state: tokens are stored as digests, like session tokens.
	Verified      bool      `json:",omitempty"`
	VerifyHash    string    `json:",omitempty"`
	VerifyExpires time.Time `json:",omitzero"`
	ResetHash     string    `json:",omitempty"`
	ResetExpires  time.Time `json:",omitzero"`
	NoEmail       bool      `json:",omitempty"`
	// OwnReminders means Reminders replaces each organiser's default in every exchange.
	OwnReminders bool       `json:",omitempty"`
	Reminders    []Reminder `json:",omitempty"`
	LastMail     time.Time  `json:",omitzero"`  // last account email, for the resend cooldown
	MailDay      string     `json:",omitempty"` // with MailCount, caps account emails per day
	MailCount    int        `json:",omitempty"`
	NotifyDay    string     `json:",omitempty"` // with NotifyCount, caps exchange emails per day
	NotifyCount  int        `json:",omitempty"`
}
type Wish struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note"`
	Price string `json:"price"`
	// Exchanges limits who sees the idea to the people buying for this user in those exchanges; empty means every exchange.
	Exchanges []string `json:"exchanges"`
	// Images are the idea's photos as data URIs, in display order. They live in the data file; clients
	// are never sent them, only same-origin URLs to fetch the bytes from /image/.
	Images []string `json:"images,omitempty"`
}

// wishOut is an idea as clients see it: photos are URLs, never the stored data.
type wishOut struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Note      string   `json:"note"`
	Price     string   `json:"price"`
	Exchanges []string `json:"exchanges"`
	Images    []string `json:"images,omitempty"`
}

func publicWish(w Wish) wishOut {
	out := wishOut{ID: w.ID, Title: w.Title, URL: w.URL, Note: w.Note, Price: w.Price, Exchanges: w.Exchanges}
	for i := range w.Images {
		out.Images = append(out.Images, imageURL(w.ID, i))
	}
	return out
}

func imageURL(wishID string, i int) string { return "/image/" + wishID + "/" + strconv.Itoa(i) }

// wishesFor is what someone buying for u in exchange e can see.
func wishesFor(u *User, e *Exchange) []wishOut {
	out := []wishOut{}
	for _, w := range u.Wishes {
		if len(w.Exchanges) == 0 || slices.Contains(w.Exchanges, e.ID) {
			w.Exchanges = nil
			out = append(out, publicWish(w))
		}
	}
	return out
}

// Photo limits, far above what a phone photo needs after the app resizes it. They are variables so
// tests can shrink them.
var (
	maxImageBytes = 512 * 1024       // decoded bytes per photo
	maxImages     = 5                // photos per idea
	maxImageTotal = 16 * 1024 * 1024 // decoded bytes of photos per person
)

// sniffImage returns the content type of image bytes by their magic numbers, or "".
func sniffImage(b []byte) string {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) > 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// decodeImage validates a photo data URI and returns its bytes and content type. Only real image
// bytes of the declared type are accepted, so a photo can never smuggle markup or script.
func decodeImage(s string) ([]byte, string, error) {
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return nil, "", bad("Send the photo as image data.")
	}
	comma := strings.Index(s, ",")
	if comma < 0 {
		return nil, "", bad("That photo data is malformed.")
	}
	meta := strings.Split(s[len(prefix):comma], ";")
	if len(meta) != 2 || meta[1] != "base64" {
		return nil, "", bad("Send the photo as base64 image data.")
	}
	b, err := base64.StdEncoding.DecodeString(s[comma+1:])
	if err != nil {
		return nil, "", bad("That photo data is malformed.")
	}
	if len(b) == 0 || len(b) > maxImageBytes {
		return nil, "", bad("A photo can be up to 512 KB. Try a smaller one.")
	}
	if sniffImage(b) != meta[0] {
		return nil, "", bad("That file isn’t a JPEG, PNG, GIF or WebP photo.")
	}
	return b, meta[0], nil
}

// imageBytes is the decoded size of a stored photo data URI, without decoding it.
func imageBytes(s string) int {
	if comma := strings.Index(s, ","); comma >= 0 {
		return base64.StdEncoding.DecodedLen(len(s) - comma - 1)
	}
	return 0
}

// imageBudget refuses photos that would push u past the per-person total. wishID's own stored
// photos are not counted, because newBytes replaces them.
func (a *App) imageBudget(u *User, wishID string, newBytes int) error {
	total := newBytes
	for _, w := range u.Wishes {
		if w.ID != wishID {
			for _, img := range w.Images {
				total += imageBytes(img)
			}
		}
	}
	if total > maxImageTotal {
		return bad("Your photos add up to more than 16 MB. Remove some from an older idea first.")
	}
	return nil
}

type Exchange struct {
	ID          string
	Name        string
	Date        string
	Budget      string
	Currency    string
	Note        string
	Owner       string
	Invite      string
	Members     []string
	Assignments map[string]string
	Archived    bool
	// Reminders is the organiser's default schedule; nil means a week and a day before.
	Reminders   []Reminder
	MyReminders map[string][]Reminder `json:",omitempty"` // members' own schedules, overriding the default
	Reminded    map[string][]string   `json:",omitempty"` // keys of reminders already sent (or skipped), per member
	Ready       []string              `json:",omitempty"` // members with their gift ready to give, who want no more reminders
}
type Session struct {
	User    string
	Expires time.Time
}
type State struct {
	Users     map[string]*User
	Exchanges map[string]*Exchange
	Sessions  map[string]Session
	Outbox    []Mail `json:",omitempty"`
}
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

type App struct {
	mu     sync.Mutex
	state  State
	path   string
	secure bool
	// trustProxy reads the client address from X-Forwarded-For on loopback requests, for running behind a local reverse proxy.
	trustProxy bool
	limits     map[string]limiter
	swept      time.Time
	// fails counts login attempts per account, and gateFails wrong access codes across the whole site.
	// Both are bounded (by the number of accounts, and by one) so flooding them with other keys can't reset them.
	fails     map[string]limiter
	gateFails limiter
	// key is the server secret in gifty.key, kept out of the data file and its backups. It keys the access-gate
	// cookie and the unsubscribe links.
	key []byte
	// mailer is nil when email is not configured.
	mailer func(to string, msg []byte) error
	base   string
	from   string
	wake   chan struct{}
	// access is the keyed hash of GIFTY_ACCESS_CODE (see gate); empty means anyone may sign up.
	access string
	// admins holds the lowercased emails from GIFTY_ADMINS.
	admins map[string]bool
	// hashSlots bounds concurrent password hashing, which takes about 300 ms of CPU each. Requests only reach it
	// once they are past the access gate and have a plausible target, so it can't be filled anonymously.
	hashSlots chan struct{}
	// hourStart and hourCount cap account emails across the whole site.
	hourStart time.Time
	hourCount int
}

// prehash carries a password hash computed before the request takes the state lock.
type prehash struct{ salt, hash string }
type prehashKey struct{}

// authPaths are rate limited per client before the lock; signup, login and reset also hash a password.
var authPaths = map[string]bool{"signup": true, "login": true, "reset": true, "reset/request": true, "access": true, "account/delete": true, "verify": true}

// isAuthPath includes the unauthenticated token endpoints, which scan every account.
func isAuthPath(path string) bool { return authPaths[path] || strings.HasPrefix(path, "unsubscribe/") }

type problem struct {
	status  int
	message string
}

func (p problem) Error() string { return p.message }
func bad(s string) error        { return problem{400, s} }
func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// gate is the access-gate credential for code: an HMAC under the server's own key.
func (a *App) gate(code string) string {
	return a.mac("gifty-access:" + code)
}

// mac is an HMAC of s under the server key.
func (a *App) mac(s string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}

// unsubToken is the unsubscribe credential in u's emails. It is derived, so it isn't stored anywhere.
func (a *App) unsubToken(u *User) string { return a.mac("gifty-unsub:" + u.ID) }

// same compares two secrets in constant time.
func same(a, b string) bool  { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func password(p, s string) string {
	b, err := pbkdf2.Key(sha256.New, p, []byte(s), 600000, 32)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func openApp(path string) (*App, error) {
	a := &App{path: path, limits: map[string]limiter{}, fails: map[string]limiter{}, hashSlots: make(chan struct{}, 2), state: State{Users: map[string]*User{}, Exchanges: map[string]*Exchange{}, Sessions: map[string]Session{}}}
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &a.state)
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if a.state.Users == nil || a.state.Exchanges == nil || a.state.Sessions == nil {
		return nil, errors.New("invalid data file")
	}
	if err := a.loadKey(); err != nil {
		return nil, err
	}
	return a, nil
}

// loadKey reads the server secret from gifty.key beside the data file, creating it on first run.
func (a *App) loadKey() error {
	if a.path == "" {
		a.key = []byte(token())
		return nil
	}
	dir := filepath.Dir(a.path)
	kp := filepath.Join(dir, "gifty.key")
	b, err := os.ReadFile(kp)
	if err == nil {
		if a.key = bytes.TrimSpace(b); len(a.key) < 32 {
			return errors.New("gifty.key is damaged")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	k := token()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gifty-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(k + "\n"); err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), kp)
	}
	if err != nil {
		return err
	}
	a.key = []byte(k)
	return nil
}
func (a *App) save() error {
	if a.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.path), ".gifty-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), a.path)
}
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
func send(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
func (a *App) user(r *http.Request) *User {
	c, err := r.Cookie("gifty_session")
	if err != nil {
		return nil
	}
	s, ok := a.state.Sessions[digest(c.Value)]
	if !ok || time.Now().After(s.Expires) {
		return nil
	}
	return a.state.Users[s.User]
}
func (a *App) cookie(w http.ResponseWriter, t string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "gifty_session", Value: t, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *App) publicUser(u *User) any {
	wishes := make([]wishOut, len(u.Wishes))
	for i, w := range u.Wishes {
		wishes[i] = publicWish(w)
	}
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "wishes": wishes, "verified": u.Verified, "notify": !u.NoEmail, "ownReminders": u.OwnReminders, "reminders": append([]Reminder{}, u.Reminders...), "admin": a.isAdmin(u)}
}

// isAdmin needs a confirmed address, so nobody can become an administrator by signing up with an admin's email first.
func (a *App) isAdmin(u *User) bool { return u.Verified && u.Email != "" && a.admins[u.Email] }

// Site-wide ceilings, far above what friends and family need, so the data file and the work done on every
// save stay bounded however it is used.
const (
	maxUsers     = 2000
	maxExchanges = 5000
)

const deletedName = "Deleted account"

// removeUser deletes an account. People in drawn exchanges stay on as an empty
// "Deleted account" so everyone else's draw still adds up; otherwise the record goes.
func (a *App) removeUser(u *User) error {
	for _, e := range a.state.Exchanges {
		if e.Owner == u.ID && !e.Archived && len(e.Members) > 1 {
			return bad("You organise “" + e.Name + "” with other people in it. Archive it first.")
		}
	}
	kept := false
	for id, e := range a.state.Exchanges {
		if !slices.Contains(e.Members, u.ID) {
			continue
		}
		if len(e.Members) == 1 {
			delete(a.state.Exchanges, id)
			continue
		}
		delete(e.MyReminders, u.ID)
		delete(e.Reminded, u.ID)
		e.Ready = slices.DeleteFunc(e.Ready, func(m string) bool { return m == u.ID })
		if len(e.Assignments) == 0 {
			e.Members = slices.DeleteFunc(e.Members, func(m string) bool { return m == u.ID })
		} else {
			kept = true
		}
		if e.Owner == u.ID {
			kept = true // the organiser's name is still shown
		}
	}
	for k, s := range a.state.Sessions {
		if s.User == u.ID {
			delete(a.state.Sessions, k)
		}
	}
	a.state.Outbox = slices.DeleteFunc(a.state.Outbox, func(m Mail) bool { return m.To == u.Email })
	if !kept {
		delete(a.state.Users, u.ID)
		return nil
	}
	*a.state.Users[u.ID] = User{ID: u.ID, Name: deletedName, Wishes: []Wish{}}
	return nil
}

// removeExchange deletes an exchange and clears it from the scope of ideas that named it.
func (a *App) removeExchange(id string) {
	delete(a.state.Exchanges, id)
	for _, v := range a.state.Users {
		for i := range v.Wishes {
			v.Wishes[i].Exchanges = slices.DeleteFunc(v.Wishes[i].Exchanges, func(x string) bool { return x == id })
		}
	}
}
func (a *App) byToken(match func(*User) bool) *User {
	for _, u := range a.state.Users {
		if match(u) {
			return u
		}
	}
	return nil
}

// clientIP is the caller's address for rate limiting. IPv6 callers are keyed by their /64, since one
// subscriber can use any address in it.
func (a *App) clientIP(r *http.Request) string {
	ip := a.rawIP(r)
	if p := net.ParseIP(ip); p != nil && p.To4() == nil {
		return p.Mask(net.CIDRMask(64, 128)).String()
	}
	return ip
}
func (a *App) rawIP(r *http.Request) string {
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
func (a *App) login(w http.ResponseWriter, u *User) {
	now := time.Now()
	for k, s := range a.state.Sessions {
		if now.After(s.Expires) {
			delete(a.state.Sessions, k)
		}
	}
	t := token()
	a.state.Sessions[digest(t)] = Session{u.ID, now.Add(30 * 24 * time.Hour)}
	a.cookie(w, t, 30*86400)
}
func (a *App) exchange(e *Exchange, u *User) any {
	members := []any{}
	for _, id := range e.Members {
		m := a.state.Users[id]
		member := map[string]any{"id": id, "name": m.Name}
		if len(e.Assignments) > 0 {
			// Whether someone has their gift is shared with the group; who it is for never is.
			member["ready"] = slices.Contains(e.Ready, id)
		}
		members = append(members, member)
	}
	v := map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "note": e.Note, "owner": e.Owner, "members": members, "drawn": len(e.Assignments) > 0, "archived": e.Archived, "reminders": e.defaultReminders()}
	mine, source := a.reminders(e, u.ID)
	v["mine"] = map[string]any{"reminders": mine, "source": source}
	if u.ID == e.Owner && !e.Archived && len(e.Assignments) == 0 {
		v["invite"] = e.Invite
	}
	if id := e.Assignments[u.ID]; id != "" {
		m := a.state.Users[id]
		v["recipient"] = map[string]any{"id": m.ID, "name": m.Name, "wishes": wishesFor(m, e)}
		v["ready"] = slices.Contains(e.Ready, u.ID)
	}
	return v
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
func (a *App) serveAPI(w http.ResponseWriter, r *http.Request) {
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
		send(w, 403, map[string]string{"error": "Open Gifty and try again."})
		return
	}
	if path := strings.TrimPrefix(r.URL.Path, "/api/"); r.Method == "POST" && isAuthPath(path) {
		var ok bool
		if r, ok = a.prepare(w, r, path); !ok {
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Method == "POST" && !isAuthPath(strings.TrimPrefix(r.URL.Path, "/api/")) && !a.allow("write|"+a.clientIP(r), 300) {
		send(w, 429, map[string]string{"error": "Too many changes at once. Try again in a few minutes."})
		return
	}
	// Roll back all in-memory changes when persistence fails. Reads change nothing, so only writes pay for a snapshot.
	var before []byte
	if r.Method == "POST" {
		before, _ = json.Marshal(a.state)
	}
	result, err := a.dispatch(w, r)
	if err == nil && r.Method == "POST" {
		err = a.save()
		if err != nil {
			json.Unmarshal(before, &a.state)
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

// serveImage sends an idea's photo to its owner, or to the person buying for them in an exchange the
// idea is shown in — the same rule as the wish list itself. The bytes are never cached, so removing
// or replacing a photo takes effect at once.
func (a *App) serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.user(r)
	if u == nil {
		http.Error(w, "Sign in to continue.", 401)
		return
	}
	id, num, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/image/"), "/")
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		http.NotFound(w, r)
		return
	}
	for _, owner := range a.state.Users {
		for _, wish := range owner.Wishes {
			if wish.ID != id || n >= len(wish.Images) {
				continue
			}
			if owner.ID != u.ID {
				allowed := false
				for _, e := range a.state.Exchanges {
					if e.Assignments[u.ID] == owner.ID && (len(wish.Exchanges) == 0 || slices.Contains(wish.Exchanges, e.ID)) {
						allowed = true
						break
					}
				}
				if !allowed {
					http.NotFound(w, r)
					return
				}
			}
			data, mime, err := decodeImage(wish.Images[n])
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", mime)
			w.Header().Set("Cache-Control", "private, no-store")
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
			return
		}
	}
	http.NotFound(w, r)
}

// prepare rate limits an authentication request and, for those carrying a
// password, hashes it without holding the state lock, so slow hashing never
// stalls other requests. It answers the request itself when it refuses it.
func (a *App) prepare(w http.ResponseWriter, r *http.Request, path string) (*http.Request, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		send(w, 413, map[string]string{"error": "That form is too large."})
		return r, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var in struct{ Email, Password, Invite, Token string }
	json.Unmarshal(body, &in)
	a.mu.Lock()
	now := time.Now()
	allowed := a.allow("auth|"+a.clientIP(r), 30)
	salt := token()
	// Hashing is the expensive step, so only requests that could succeed get it: the rest are refused or
	// fail in dispatch without it. Anonymous callers therefore can't use up the hashing slots.
	hash, locked := false, false
	switch path {
	case "signup":
		hash = a.hasAccess(r) || a.openInvite(in.Invite)
	case "login":
		salt = "missing-account"
		hash = a.hasAccess(r)
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if u := a.byToken(func(u *User) bool { return u.Email != "" && u.Email == email }); u != nil {
			salt = u.Salt
			if allowed && hash {
				l := a.fails[u.ID]
				locked = !l.take(now, 10)
				a.fails[u.ID] = l
			}
		}
	case "reset":
		h := digest(in.Token)
		hash = a.byToken(func(v *User) bool { return v.ResetHash != "" && v.ResetHash == h && now.Before(v.ResetExpires) }) != nil
	case "account/delete":
		salt = "missing-account"
		if u := a.user(r); u != nil {
			salt, hash = u.Salt, true
		}
	}
	a.mu.Unlock()
	if !allowed {
		send(w, 429, map[string]string{"error": "Too many attempts. Try again in 15 minutes."})
		return r, false
	}
	if locked {
		send(w, 429, map[string]string{"error": "Too many attempts on this account. Try again in 15 minutes, or reset the password."})
		return r, false
	}
	if !hash || len(in.Password) > 256 {
		return r, true
	}
	select {
	case a.hashSlots <- struct{}{}:
	case <-time.After(3 * time.Second):
		send(w, 503, map[string]string{"error": "Gifty is busy. Try again in a moment."})
		return r, false
	}
	h := password(in.Password, salt)
	<-a.hashSlots
	return r.WithContext(context.WithValue(r.Context(), prehashKey{}, prehash{salt, h})), true
}

// allow counts an attempt under key: max per 15 minutes. It tracks at most 10,000 keys; when full, the
// entry closest to expiring is dropped, which gives up the least throttling. Expired entries are swept once a minute.
func (a *App) allow(key string, max int) bool {
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
func (a *App) sweep(now time.Time) {
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
func (a *App) hasAccess(r *http.Request) bool {
	if a.access == "" {
		return true
	}
	c, err := r.Cookie("gifty_access")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.access)) == 1
}

// grantAccess remembers on this browser that it is past the access gate.
func (a *App) grantAccess(w http.ResponseWriter) {
	if a.access != "" {
		http.SetCookie(w, &http.Cookie{Name: "gifty_access", Value: a.access, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: 90 * 86400})
	}
}
func (a *App) openInvite(code string) bool {
	for _, e := range a.state.Exchanges {
		if code != "" && same(e.Invite, code) && !e.Archived && len(e.Assignments) == 0 {
			return true
		}
	}
	return false
}
func (a *App) dispatch(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	u := a.user(r)
	if path == "config" && r.Method == "GET" {
		return map[string]bool{"mail": a.mailOn(), "gated": a.access != "", "access": u != nil || a.hasAccess(r)}, nil
	}
	if path == "access" && r.Method == "POST" {
		var in struct{ Code string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		// Wrong guesses share one budget across every client, so rotating addresses doesn't help. Past it, even
		// the right code is refused until the window ends, so the answer never confirms a guess.
		now := time.Now()
		if a.gateFails.Count >= 20 && now.Before(a.gateFails.Reset) {
			return nil, problem{429, "Too many wrong codes. Try again in 15 minutes."}
		}
		if a.access == "" || !same(a.gate(strings.TrimSpace(in.Code)), a.access) {
			a.gateFails.take(now, 20)
			return nil, problem{403, "That code isn’t right."}
		}
		a.grantAccess(w)
		return map[string]bool{"ok": true}, nil
	}
	if path == "verify" && r.Method == "POST" {
		var in struct{ Token string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		h := digest(in.Token)
		v := a.byToken(func(v *User) bool {
			return v.VerifyHash != "" && same(v.VerifyHash, h) && time.Now().Before(v.VerifyExpires)
		})
		if v == nil {
			return nil, problem{404, "This confirmation link has expired or already been used."}
		}
		v.Verified, v.VerifyHash, v.VerifyExpires = true, "", time.Time{}
		return map[string]bool{"ok": true}, nil
	}
	if strings.HasPrefix(path, "unsubscribe/") && r.Method == "POST" {
		// Also used by mail clients' one-click unsubscribe, which posts a form, so the body is ignored.
		t := strings.TrimPrefix(path, "unsubscribe/")
		v := a.byToken(func(v *User) bool {
			return same(a.unsubToken(v), t)
		})
		if v == nil {
			return nil, problem{404, "This link doesn’t match an account."}
		}
		v.NoEmail = true
		return map[string]bool{"ok": true}, nil
	}
	if path == "signup" || path == "login" || path == "reset" || path == "reset/request" {
		if r.Method != "POST" {
			return nil, problem{405, "Method not allowed."}
		}
		// prepare has already rate limited this request and hashed any password outside the lock.
		now := time.Now()
		pre, _ := r.Context().Value(prehashKey{}).(prehash)
		if path == "reset/request" {
			var in struct{ Email string }
			if err := readJSON(r, &in); err != nil {
				return nil, err
			}
			email := strings.ToLower(strings.TrimSpace(in.Email))
			// Always answer the same way so the form can't reveal which addresses have accounts.
			if v := a.byToken(func(v *User) bool { return v.Email != "" && v.Email == email }); v != nil && a.mailOn() && now.After(v.ResetExpires.Add(2*time.Minute-time.Hour)) {
				a.sendReset(v)
			}
			return map[string]bool{"ok": true}, nil
		}
		if path == "reset" {
			var in struct{ Token, Password string }
			if err := readJSON(r, &in); err != nil {
				return nil, err
			}
			if len(in.Password) < 12 || len(in.Password) > 256 {
				return nil, bad("Use a password between 12 and 256 characters.")
			}
			h := digest(in.Token)
			v := a.byToken(func(v *User) bool { return v.ResetHash != "" && same(v.ResetHash, h) && now.Before(v.ResetExpires) })
			if v == nil {
				return nil, problem{404, "This reset link has expired or already been used. Ask for a new one."}
			}
			if pre.hash == "" {
				return nil, bad("Try again.")
			}
			v.Salt, v.Hash = pre.salt, pre.hash
			delete(a.fails, v.ID)
			// Opening the link proves the inbox, and a reset signs out every other device.
			v.ResetHash, v.ResetExpires, v.Verified = "", time.Time{}, true
			for k, s := range a.state.Sessions {
				if s.User == v.ID {
					delete(a.state.Sessions, k)
				}
			}
			a.login(w, v)
			a.grantAccess(w) // a reset is the way back in for someone who never had the code
			return a.publicUser(v), nil
		}
		var in struct{ Name, Email, Password, Invite string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Email = strings.ToLower(strings.TrimSpace(in.Email))
		var found *User
		for _, v := range a.state.Users {
			if v.Email != "" && v.Email == in.Email {
				found = v
				break
			}
		}
		if path == "signup" {
			addr, err := mail.ParseAddress(in.Email)
			if err != nil || addr.Address != in.Email || len(in.Email) > 254 {
				return nil, bad("Enter a valid email address.")
			}
			if len(in.Name) < 1 || len(in.Name) > 80 {
				return nil, bad("Enter a name of up to 80 characters.")
			}
			if len(in.Password) < 12 || len(in.Password) > 256 {
				return nil, bad("Use a password between 12 and 256 characters.")
			}
			if !a.hasAccess(r) && !a.openInvite(in.Invite) {
				return nil, problem{403, "Enter the access code or open your invitation link first."}
			}
			if found != nil {
				// Tell the owner, so a probe for their address doesn't go unnoticed.
				if found.Verified && a.accountMailAllowed(found, now) {
					a.queue(found, false, "Someone tried to sign up with your Gifty email", fmt.Sprintf("Hello %s,\n\nSomeone tried to create a Gifty account with this email address, which already has one. If that was you, sign in at %s/#login instead (you can reset your password from there). If it wasn’t, you can ignore this email.", found.Name, a.base))
				}
				return nil, bad("An account already uses that email. Sign in instead.")
			}
			if pre.hash == "" {
				return nil, bad("Try again.")
			}
			if len(a.state.Users) >= maxUsers {
				return nil, problem{503, "Gifty isn’t taking new accounts right now."}
			}
			found = &User{ID: token(), Name: in.Name, Email: in.Email, Salt: pre.salt, Hash: pre.hash, Wishes: []Wish{}}
			a.state.Users[found.ID] = found
			a.sendVerify(found)
			a.grantAccess(w) // an invited guest never sees the code, and still needs to sign in again later
		} else {
			if !a.hasAccess(r) {
				return nil, problem{403, "Enter the access code first."}
			}
			if len(in.Password) > 256 {
				return nil, bad("Email or password is incorrect.")
			}
			want := strings.Repeat("0", 64)
			// The hash was made with the salt seen before the lock; a reset in between makes it miss.
			if found != nil && found.Salt == pre.salt {
				want = found.Hash
			}
			if !same(pre.hash, want) {
				return nil, problem{401, "Email or password is incorrect."}
			}
			delete(a.fails, found.ID)
		}
		a.login(w, found)
		return a.publicUser(found), nil
	}
	if strings.HasPrefix(path, "invite/") && r.Method == "GET" {
		code := strings.TrimPrefix(path, "invite/")
		for _, e := range a.state.Exchanges {
			if same(e.Invite, code) && !e.Archived && len(e.Assignments) == 0 {
				return map[string]any{"name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "organiser": a.state.Users[e.Owner].Name, "people": len(e.Members)}, nil
			}
		}
		return nil, problem{404, "This invitation is closed or no longer exists."}
	}
	if u == nil {
		return nil, problem{401, "Sign in to continue."}
	}
	if path == "me" && r.Method == "GET" {
		return a.publicUser(u), nil
	}
	if path == "verify/resend" && r.Method == "POST" {
		if !a.mailOn() {
			return nil, bad("This Gifty server doesn’t send emails.")
		}
		if u.Verified {
			return nil, bad("Your email address is already confirmed.")
		}
		if time.Now().Before(u.LastMail.Add(time.Minute)) {
			return nil, problem{429, "Wait a minute before asking for another email."}
		}
		if !a.sendVerify(u) {
			return nil, problem{429, "You’ve asked for several emails today. Try again tomorrow."}
		}
		return a.publicUser(u), nil
	}
	if path == "account/reminders" && r.Method == "POST" {
		// Default true goes back to using each organiser's schedule.
		var in struct {
			Default   bool
			Reminders []Reminder
		}
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		days, err := parseReminders(in.Reminders)
		if err != nil {
			return nil, err
		}
		u.OwnReminders, u.Reminders = !in.Default, days
		if in.Default {
			u.Reminders = nil
		}
		for _, e := range a.state.Exchanges {
			if len(e.Assignments) > 0 && slices.Contains(e.Members, u.ID) {
				a.dueReminders(e, u.ID, time.Now())
			}
		}
		return a.publicUser(u), nil
	}
	if path == "account" && r.Method == "POST" {
		var in struct{ Notify bool }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		u.NoEmail = !in.Notify
		return a.publicUser(u), nil
	}
	if path == "account/delete" && r.Method == "POST" {
		pre, _ := r.Context().Value(prehashKey{}).(prehash)
		if u.Salt != pre.salt || subtle.ConstantTimeCompare([]byte(pre.hash), []byte(u.Hash)) != 1 {
			return nil, problem{403, "That password isn’t right."}
		}
		if err := a.removeUser(u); err != nil {
			return nil, err
		}
		a.cookie(w, "", -1)
		return map[string]bool{"ok": true}, nil
	}
	if strings.HasPrefix(path, "admin/") {
		if !a.isAdmin(u) {
			return nil, problem{404, "Page not found."}
		}
		return a.admin(strings.TrimPrefix(path, "admin/"), r, u)
	}
	if path == "logout" && r.Method == "POST" {
		c, _ := r.Cookie("gifty_session")
		delete(a.state.Sessions, digest(c.Value))
		a.cookie(w, "", -1)
		return map[string]bool{"ok": true}, nil
	}
	if path == "exchanges" && r.Method == "GET" {
		out := []any{}
		for _, e := range a.state.Exchanges {
			if slices.Contains(e.Members, u.ID) {
				out = append(out, a.exchange(e, u))
			}
		}
		return out, nil
	}
	if path == "wishes" && r.Method == "POST" {
		var in struct {
			ID        string   `json:"id"`
			Title     string   `json:"title"`
			URL       string   `json:"url"`
			Note      string   `json:"note"`
			Price     string   `json:"price"`
			Exchanges []string `json:"exchanges"`
			// Photos, when present, is the idea's whole photo list in order: each entry is either a
			// photo already on the idea (its /image/ URL) or a new one as a data URI. Absent means
			// leave the photos alone.
			Photos *[]string `json:"photos"`
		}
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		in.Title = strings.TrimSpace(in.Title)
		in.URL = strings.TrimSpace(in.URL)
		in.Price = strings.TrimSpace(in.Price)
		if in.Title == "" || len(in.Title) > 120 || len(in.Note) > 1000 || len(in.URL) > 2000 || len(in.Price) > 60 {
			return nil, bad("Add a title up to 120 characters, a note up to 1,000 characters and a price up to 60 characters.")
		}
		if in.URL != "" {
			p, err := url.Parse(in.URL)
			if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.User != nil {
				return nil, bad("Use a full link starting with https:// or http://.")
			}
		}
		scope := []string{}
		for _, id := range in.Exchanges {
			if e := a.state.Exchanges[id]; e == nil || !slices.Contains(e.Members, u.ID) {
				return nil, bad("Choose exchanges you’re taking part in.")
			}
			if !slices.Contains(scope, id) {
				scope = append(scope, id)
			}
		}
		index := -1
		var w Wish
		if in.ID == "" {
			if len(u.Wishes) >= 100 {
				return nil, bad("Your list can hold up to 100 ideas.")
			}
			w.ID = token()
		} else {
			index = slices.IndexFunc(u.Wishes, func(v Wish) bool { return v.ID == in.ID })
			if index < 0 {
				return nil, problem{404, "Gift idea not found."}
			}
			w = u.Wishes[index] // keeps the stored photos unless in.Photos replaces them
		}
		w.Title, w.URL, w.Note, w.Price, w.Exchanges = in.Title, in.URL, in.Note, in.Price, scope
		if in.Photos != nil {
			if len(*in.Photos) > maxImages {
				return nil, bad(fmt.Sprintf("An idea can have up to %d photos.", maxImages))
			}
			images, size := []string{}, 0
			for _, p := range *in.Photos {
				if num, ok := strings.CutPrefix(p, "/image/"+w.ID+"/"); ok {
					n, err := strconv.Atoi(num)
					if err != nil || n < 0 || n >= len(w.Images) {
						return nil, bad("That photo is no longer on this idea.")
					}
					images = append(images, w.Images[n])
					size += imageBytes(w.Images[n])
					continue
				}
				data, _, err := decodeImage(p)
				if err != nil {
					return nil, err
				}
				size += len(data)
				images = append(images, p)
			}
			if err := a.imageBudget(u, w.ID, size); err != nil {
				return nil, err
			}
			w.Images = images
		}
		if index < 0 {
			u.Wishes = append(u.Wishes, w)
		} else {
			u.Wishes[index] = w
		}
		return a.publicUser(u), nil
	}
	if strings.HasPrefix(path, "wishes/") && r.Method == "POST" {
		id := strings.TrimPrefix(path, "wishes/")
		index := slices.IndexFunc(u.Wishes, func(v Wish) bool { return v.ID == id })
		if index < 0 {
			return nil, problem{404, "Gift idea not found."}
		}
		u.Wishes = slices.Delete(u.Wishes, index, index+1)
		return a.publicUser(u), nil
	}
	if path == "join" && r.Method == "POST" {
		var in struct{ Code string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		for _, e := range a.state.Exchanges {
			if same(e.Invite, in.Code) && !e.Archived && len(e.Assignments) == 0 {
				if !slices.Contains(e.Members, u.ID) {
					if len(e.Members) >= 100 {
						return nil, bad("This exchange is full.")
					}
					e.Members = append(e.Members, u.ID)
				}
				return a.exchange(e, u), nil
			}
		}
		return nil, problem{404, "This invitation is closed or no longer exists."}
	}
	if path == "exchanges" && r.Method == "POST" {
		active := 0
		for _, x := range a.state.Exchanges {
			if x.Owner == u.ID && !x.Archived {
				active++
			}
		}
		if len(a.state.Exchanges) >= maxExchanges {
			return nil, problem{503, "Gifty isn’t taking new exchanges right now."}
		}
		if active >= 20 {
			return nil, bad("You can organise up to 20 exchanges at once. Archive one to make room.")
		}
		e := &Exchange{ID: token(), Owner: u.ID, Invite: token(), Members: []string{u.ID}, Reminders: slices.Clone(defaultReminders)}
		if err := details(r, e); err != nil {
			return nil, err
		}
		a.state.Exchanges[e.ID] = e
		return a.exchange(e, u), nil
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "exchanges" {
		return nil, problem{404, "Page not found."}
	}
	e := a.state.Exchanges[parts[1]]
	if e == nil || !slices.Contains(e.Members, u.ID) {
		return nil, problem{404, "Exchange not found."}
	}
	if len(parts) == 2 && r.Method == "GET" {
		return a.exchange(e, u), nil
	}
	if len(parts) != 3 || r.Method != "POST" {
		return nil, problem{404, "Page not found."}
	}
	action := parts[2]
	if action == "leave" {
		if e.Owner == u.ID {
			return nil, bad("The organiser cannot leave their exchange.")
		}
		if len(e.Assignments) > 0 || e.Archived {
			return nil, bad("Membership is locked for this exchange.")
		}
		e.Members = slices.DeleteFunc(e.Members, func(id string) bool { return id == u.ID })
		return map[string]bool{"ok": true}, nil
	}
	if action == "my-reminders" {
		if e.Archived {
			return nil, bad("This exchange is archived.")
		}
		// Default true goes back to the organiser's schedule.
		var in struct {
			Default   bool
			Reminders []Reminder
		}
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if in.Default {
			delete(e.MyReminders, u.ID)
		} else {
			days, err := parseReminders(in.Reminders)
			if err != nil {
				return nil, err
			}
			if e.MyReminders == nil {
				e.MyReminders = map[string][]Reminder{}
			}
			e.MyReminders[u.ID] = days
		}
		if len(e.Assignments) > 0 {
			a.dueReminders(e, u.ID, time.Now())
		}
		return a.exchange(e, u), nil
	}
	if action == "ready" {
		if len(e.Assignments) == 0 {
			return nil, bad("Entries aren’t closed yet.")
		}
		var in struct{ Ready bool }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		e.Ready = slices.DeleteFunc(e.Ready, func(id string) bool { return id == u.ID })
		if in.Ready {
			e.Ready = append(e.Ready, u.ID)
		}
		return a.exchange(e, u), nil
	}
	if action == "delete" {
		// Deleting is permanent for everyone in the exchange, so it is for organisers, and only once it is archived.
		if e.Owner != u.ID {
			return nil, problem{403, "Only the organiser can do that."}
		}
		if !e.Archived {
			return nil, bad("Archive the exchange before deleting it.")
		}
		a.removeExchange(e.ID)
		return map[string]bool{"ok": true}, nil
	}
	if e.Owner != u.ID {
		return nil, problem{403, "Only the organiser can do that."}
	}
	if e.Archived {
		return nil, bad("This exchange is archived.")
	}
	switch action {
	case "reminders":
		// Unlike the other details, the schedule can change after the draw.
		var in struct{ Reminders []Reminder }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		days, err := parseReminders(in.Reminders)
		if err != nil {
			return nil, err
		}
		e.Reminders = days
		if len(e.Assignments) > 0 {
			for _, id := range e.Members {
				a.dueReminders(e, id, time.Now())
			}
		}
	case "archive":
		e.Archived = true
	case "edit":
		if len(e.Assignments) > 0 {
			return nil, bad("Details can’t be changed once entries are closed.")
		}
		if err := details(r, e); err != nil {
			return nil, err
		}
	case "rotate":
		if len(e.Assignments) > 0 {
			return nil, bad("Invitations stop working once entries are closed.")
		}
		e.Invite = token()
	case "remove":
		if len(e.Assignments) > 0 {
			return nil, bad("Nobody can be removed or leave once entries are closed.")
		}
		var in struct{ ID string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if in.ID == u.ID {
			return nil, bad("The organiser must stay in the exchange.")
		}
		e.Members = slices.DeleteFunc(e.Members, func(id string) bool { return id == in.ID })
	case "draw":
		if len(e.Assignments) > 0 {
			return nil, bad("Entries are already closed.")
		}
		if len(e.Members) < 3 {
			return nil, bad("You need at least three people to close entries.")
		}
		e.Assignments = draw(e.Members)
		a.drawn(e)
	default:
		return nil, problem{404, "Page not found."}
	}
	return a.exchange(e, u), nil
}

// admin serves the administrator's pages. It lists accounts and exchanges and can
// remove them, but never exposes assignments, wish lists or invitation links.
func (a *App) admin(path string, r *http.Request, u *User) (any, error) {
	switch {
	case path == "overview" && r.Method == "GET":
		users, exchanges := []map[string]any{}, []map[string]any{}
		for _, v := range a.state.Users {
			if v.Email == "" {
				continue
			}
			n := 0
			for _, e := range a.state.Exchanges {
				if slices.Contains(e.Members, v.ID) {
					n++
				}
			}
			users = append(users, map[string]any{"id": v.ID, "name": v.Name, "email": v.Email, "verified": v.Verified, "exchanges": n, "admin": a.isAdmin(v), "self": v.ID == u.ID})
		}
		for _, e := range a.state.Exchanges {
			exchanges = append(exchanges, map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "organiser": a.state.Users[e.Owner].Name, "people": len(e.Members), "drawn": len(e.Assignments) > 0, "archived": e.Archived})
		}
		slices.SortFunc(users, func(x, y map[string]any) int {
			return strings.Compare(strings.ToLower(x["name"].(string)), strings.ToLower(y["name"].(string)))
		})
		slices.SortFunc(exchanges, func(x, y map[string]any) int { return strings.Compare(y["date"].(string), x["date"].(string)) })
		return map[string]any{"users": users, "exchanges": exchanges}, nil
	case path == "users/delete" && r.Method == "POST":
		var in struct{ ID string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		v := a.state.Users[in.ID]
		if v == nil || v.Email == "" {
			return nil, problem{404, "Account not found."}
		}
		if v.ID == u.ID {
			return nil, bad("Use your account page to delete your own account.")
		}
		if err := a.removeUser(v); err != nil {
			return nil, err
		}
		log.Printf("admin %s deleted account %s", u.ID, v.ID)
		return map[string]bool{"ok": true}, nil
	case path == "exchanges/delete" && r.Method == "POST":
		var in struct{ ID string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if a.state.Exchanges[in.ID] == nil {
			return nil, problem{404, "Exchange not found."}
		}
		a.removeExchange(in.ID)
		log.Printf("admin %s deleted exchange %s", u.ID, in.ID)
		return map[string]bool{"ok": true}, nil
	}
	return nil, problem{404, "Page not found."}
}
func details(r *http.Request, e *Exchange) error {
	var in struct{ Name, Date, Budget, Currency, Note string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 || len(in.Note) > 2000 {
		return bad("Use a name up to 100 characters and a note up to 2,000 characters.")
	}
	d, err := time.Parse("2006-01-02", in.Date)
	// Allow one day of leeway: "today" in the organiser's time zone can be yesterday in UTC.
	if err != nil || d.Before(time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)) {
		return bad("Choose today or a future date.")
	}
	if !slices.Contains([]string{"GBP", "USD", "EUR", "CAD", "AUD"}, in.Currency) {
		return bad("Choose a supported currency.")
	}
	// Parse money as decimal digits, without floating-point rounding.
	b := strings.Split(in.Budget, ".")
	if len(b) > 2 || len(b[0]) == 0 || len(b[0]) > 5 {
		return bad("Enter a budget between 1 and 99,999.")
	}
	for _, c := range in.Budget {
		if (c < '0' || c > '9') && c != '.' {
			return bad("Enter a valid budget.")
		}
	}
	if len(b) == 2 && (len(b[1]) < 1 || len(b[1]) > 2) {
		return bad("Use at most two decimal places.")
	}
	if strings.Trim(b[0], "0") == "" {
		return bad("The budget must be at least 1.")
	}
	e.Name = in.Name
	e.Date = in.Date
	e.Budget = in.Budget
	e.Currency = in.Currency
	e.Note = strings.TrimSpace(in.Note)
	return nil
}
func draw(ids []string) map[string]string {
	// Rejection sampling yields a uniform random derangement, including separate cycles.
	recipients := slices.Clone(ids)
	for {
		for i := len(recipients) - 1; i > 0; i-- {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
			if err != nil {
				panic(err)
			}
			j := int(n.Int64())
			recipients[i], recipients[j] = recipients[j], recipients[i]
		}
		valid := true
		for i, id := range ids {
			if id == recipients[i] {
				valid = false
				break
			}
		}
		if valid {
			break
		}
	}
	out := map[string]string{}
	for i, id := range ids {
		out[id] = recipients[i]
	}
	return out
}
func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", a.serveAPI)
	mux.HandleFunc("/image/", a.serveImage)
	// Embedded files have no modification time, so content hashes let browsers revalidate instead of re-downloading.
	type file struct {
		body []byte
		etag string
	}
	static := map[string]file{}
	for _, name := range []string{"index.html", "app.js", "style.css", "favicon.svg", "public-sans.woff2", "public-sans-OFL.txt"} {
		b, _ := assets.ReadFile("web/" + name)
		sum := sha256.Sum256(b)
		static["/"+name] = file{b, `"` + hex.EncodeToString(sum[:8]) + `"`}
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		} else if path == "/index.html" {
			path = ""
		}
		f, ok := static[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", f.etag)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(f.body))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}
func main() {
	path := os.Getenv("GIFTY_DATA")
	if path == "" {
		path = "data/gifty.json"
	}
	a, err := openApp(path)
	if err != nil {
		log.Fatal(err)
	}
	// Cookies are Secure whenever the public address is https; GIFTY_SECURE_COOKIES overrides that either way.
	a.secure = strings.HasPrefix(os.Getenv("GIFTY_BASE_URL"), "https://")
	if v := os.Getenv("GIFTY_SECURE_COOKIES"); v != "" {
		a.secure = v == "true"
	}
	if !a.secure {
		log.Print("warning: cookies are not marked Secure; use this only on a trusted network or behind HTTPS")
	}
	a.trustProxy = os.Getenv("GIFTY_TRUST_PROXY") == "true"
	if code := strings.TrimSpace(os.Getenv("GIFTY_ACCESS_CODE")); code != "" {
		a.access = a.gate(code)
	}
	a.admins = map[string]bool{}
	for _, v := range strings.Split(os.Getenv("GIFTY_ADMINS"), ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			a.admins[v] = true
		}
	}
	addr := os.Getenv("GIFTY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	// Email is off unless GIFTY_SMTP_HOST is set. "log" prints emails instead of sending them.
	if host := os.Getenv("GIFTY_SMTP_HOST"); host != "" {
		a.base = strings.TrimSuffix(os.Getenv("GIFTY_BASE_URL"), "/")
		a.from = os.Getenv("GIFTY_MAIL_FROM")
		if host == "log" {
			if a.base == "" {
				a.base = "http://" + addr
			}
			if a.from == "" {
				a.from = "Gifty <gifty@localhost>"
			}
			a.mailer = logSender
		} else {
			port := os.Getenv("GIFTY_SMTP_PORT")
			if port == "" {
				port = "587"
			}
			if a.base == "" || a.from == "" {
				log.Fatal("GIFTY_BASE_URL and GIFTY_MAIL_FROM are required when GIFTY_SMTP_HOST is set")
			}
			if _, err := mail.ParseAddress(a.from); err != nil {
				log.Fatalf("GIFTY_MAIL_FROM: %v", err)
			}
			a.mailer = smtpSender(host, port, os.Getenv("GIFTY_SMTP_USER"), os.Getenv("GIFTY_SMTP_PASSWORD"), a.from)
		}
		a.wake = make(chan struct{}, 1)
		go a.mailLoop()
		fmt.Printf("Email is on, sending from %s\n", a.from)
	}
	s := &http.Server{Addr: addr, Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()
	fmt.Printf("Gifty is running at http://%s\n", addr)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
