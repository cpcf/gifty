package main

import (
	"bytes"
	"context"
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
	Unsub         string    `json:",omitempty"`
	// OwnReminders means Reminders replaces each organiser's default in every exchange.
	OwnReminders bool       `json:",omitempty"`
	Reminders    []Reminder `json:",omitempty"`
	LastMail     time.Time  `json:",omitzero"`  // last account email, for the resend cooldown
	MailDay      string     `json:",omitempty"` // with MailCount, caps account emails per day
	MailCount    int        `json:",omitempty"`
}
type Wish struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note"`
	// Exchanges limits who sees the idea to the people buying for this user in those exchanges; empty means every exchange.
	Exchanges []string `json:"exchanges"`
}

// wishesFor is what someone buying for u in exchange e can see.
func wishesFor(u *User, e *Exchange) []Wish {
	out := []Wish{}
	for _, w := range u.Wishes {
		if len(w.Exchanges) == 0 || slices.Contains(w.Exchanges, e.ID) {
			w.Exchanges = nil
			out = append(out, w)
		}
	}
	return out
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
	Done        []string              `json:",omitempty"` // members who have their gift and want no more reminders
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
type App struct {
	mu     sync.Mutex
	state  State
	path   string
	secure bool
	// trustProxy reads the client address from X-Forwarded-For on loopback requests, for running behind a local reverse proxy.
	trustProxy bool
	limits     map[string]limiter
	// mailer is nil when email is not configured.
	mailer func(to string, msg []byte) error
	base   string
	from   string
	wake   chan struct{}
	// access is the digest of GIFTY_ACCESS_CODE; empty means anyone may sign up.
	access string
	// hashSlots bounds concurrent password hashing, which takes about 300 ms of CPU each.
	hashSlots chan struct{}
	// hourStart and hourCount cap account emails across the whole site.
	hourStart time.Time
	hourCount int
}

// prehash carries a password hash computed before the request takes the state lock.
type prehash struct{ salt, hash string }
type prehashKey struct{}

// authPaths are rate limited per client before the lock; signup, login and reset also hash a password.
var authPaths = map[string]bool{"signup": true, "login": true, "reset": true, "reset/request": true, "access": true}

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
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func password(p, s string) string {
	b, err := pbkdf2.Key(sha256.New, p, []byte(s), 600000, 32)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func openApp(path string) (*App, error) {
	a := &App{path: path, limits: map[string]limiter{}, hashSlots: make(chan struct{}, 2), state: State{Users: map[string]*User{}, Exchanges: map[string]*Exchange{}, Sessions: map[string]Session{}}}
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
	return a, nil
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
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "wishes": u.Wishes, "verified": u.Verified, "notify": !u.NoEmail, "ownReminders": u.OwnReminders, "reminders": append([]Reminder{}, u.Reminders...)}
}
func (a *App) byToken(match func(*User) bool) *User {
	for _, u := range a.state.Users {
		if match(u) {
			return u
		}
	}
	return nil
}
func (a *App) clientIP(r *http.Request) string {
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
		members = append(members, map[string]any{"id": id, "name": m.Name})
	}
	v := map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "note": e.Note, "owner": e.Owner, "members": members, "drawn": len(e.Assignments) > 0, "archived": e.Archived, "reminders": e.defaultReminders()}
	mine, source := a.reminders(e, u.ID)
	v["mine"] = map[string]any{"reminders": mine, "source": source}
	if u.ID == e.Owner && !e.Archived && len(e.Assignments) == 0 {
		v["invite"] = e.Invite
	}
	if id := e.Assignments[u.ID]; id != "" {
		m := a.state.Users[id]
		v["recipient"] = map[string]any{"name": m.Name, "wishes": wishesFor(m, e)}
		v["done"] = slices.Contains(e.Done, u.ID)
	}
	return v
}
func (a *App) serveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if r.Method != "GET" && r.Method != "POST" {
		send(w, 405, map[string]string{"error": "Method not allowed."})
		return
	}
	if r.Method == "POST" {
		if o := r.Header.Get("Origin"); o != "" {
			p, err := url.Parse(o)
			if err != nil || p.Host != r.Host {
				send(w, 403, map[string]string{"error": "Open Gifty and try again."})
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			send(w, 403, map[string]string{"error": "Open Gifty and try again."})
			return
		}
	}
	if path := strings.TrimPrefix(r.URL.Path, "/api/"); r.Method == "POST" && authPaths[path] {
		var ok bool
		if r, ok = a.prepare(w, r, path); !ok {
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Roll back all in-memory changes when persistence fails.
	before, _ := json.Marshal(a.state)
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
	var in struct{ Email, Password string }
	json.Unmarshal(body, &in)
	a.mu.Lock()
	allowed := a.allow(a.clientIP(r))
	salt := token()
	if path == "login" {
		salt = "missing-account"
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if u := a.byToken(func(u *User) bool { return u.Email == email }); u != nil {
			salt = u.Salt
		}
	}
	a.mu.Unlock()
	if !allowed {
		send(w, 429, map[string]string{"error": "Too many attempts. Try again in 15 minutes."})
		return r, false
	}
	if path != "signup" && path != "login" && path != "reset" || len(in.Password) > 256 {
		return r, true
	}
	select {
	case a.hashSlots <- struct{}{}:
	case <-time.After(10 * time.Second):
		send(w, 503, map[string]string{"error": "Gifty is busy. Try again in a moment."})
		return r, false
	}
	h := password(in.Password, salt)
	<-a.hashSlots
	return r.WithContext(context.WithValue(r.Context(), prehashKey{}, prehash{salt, h})), true
}

// allow counts an attempt from ip: 30 per 15 minutes, tracking at most 10,000 addresses.
func (a *App) allow(ip string) bool {
	now := time.Now()
	for k, l := range a.limits {
		if now.After(l.Reset) {
			delete(a.limits, k)
		}
	}
	l, seen := a.limits[ip]
	if l.Count >= 30 || (!seen && len(a.limits) >= 10000) {
		return false
	}
	if l.Count == 0 {
		l.Reset = now.Add(15 * time.Minute)
	}
	l.Count++
	a.limits[ip] = l
	return true
}
func (a *App) hasAccess(r *http.Request) bool {
	if a.access == "" {
		return true
	}
	c, err := r.Cookie("gifty_access")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.access)) == 1
}
func (a *App) openInvite(code string) bool {
	for _, e := range a.state.Exchanges {
		if code != "" && e.Invite == code && !e.Archived && len(e.Assignments) == 0 {
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
		if a.access == "" || subtle.ConstantTimeCompare([]byte(digest("gifty-access:"+strings.TrimSpace(in.Code))), []byte(a.access)) != 1 {
			return nil, problem{403, "That code isn’t right."}
		}
		http.SetCookie(w, &http.Cookie{Name: "gifty_access", Value: a.access, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: 90 * 86400})
		return map[string]bool{"ok": true}, nil
	}
	if path == "verify" && r.Method == "POST" {
		var in struct{ Token string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		h := digest(in.Token)
		v := a.byToken(func(v *User) bool {
			return v.VerifyHash != "" && v.VerifyHash == h && time.Now().Before(v.VerifyExpires)
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
			return v.Unsub != "" && subtle.ConstantTimeCompare([]byte(v.Unsub), []byte(t)) == 1
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
			if v := a.byToken(func(v *User) bool { return v.Email == email }); v != nil && a.mailOn() && now.After(v.ResetExpires.Add(2*time.Minute-time.Hour)) {
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
			v := a.byToken(func(v *User) bool { return v.ResetHash != "" && v.ResetHash == h && now.Before(v.ResetExpires) })
			if v == nil {
				return nil, problem{404, "This reset link has expired or already been used. Ask for a new one."}
			}
			v.Salt, v.Hash = pre.salt, pre.hash
			// Opening the link proves the inbox, and a reset signs out every other device.
			v.ResetHash, v.ResetExpires, v.Verified = "", time.Time{}, true
			for k, s := range a.state.Sessions {
				if s.User == v.ID {
					delete(a.state.Sessions, k)
				}
			}
			a.login(w, v)
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
			if v.Email == in.Email {
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
				return nil, bad("An account already uses that email. Sign in instead.")
			}
			found = &User{ID: token(), Name: in.Name, Email: in.Email, Salt: pre.salt, Hash: pre.hash, Wishes: []Wish{}}
			a.state.Users[found.ID] = found
			a.sendVerify(found)
		} else {
			if len(in.Password) > 256 {
				return nil, bad("Email or password is incorrect.")
			}
			want := strings.Repeat("0", 64)
			// The hash was made with the salt seen before the lock; a reset in between makes it miss.
			if found != nil && found.Salt == pre.salt {
				want = found.Hash
			}
			if subtle.ConstantTimeCompare([]byte(pre.hash), []byte(want)) != 1 {
				return nil, problem{401, "Email or password is incorrect."}
			}
		}
		a.login(w, found)
		return a.publicUser(found), nil
	}
	if strings.HasPrefix(path, "invite/") && r.Method == "GET" {
		code := strings.TrimPrefix(path, "invite/")
		for _, e := range a.state.Exchanges {
			if e.Invite == code && !e.Archived && len(e.Assignments) == 0 {
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
		var in Wish
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		in.Title = strings.TrimSpace(in.Title)
		in.URL = strings.TrimSpace(in.URL)
		if in.Title == "" || len(in.Title) > 120 || len(in.Note) > 1000 || len(in.URL) > 2000 {
			return nil, bad("Add a title up to 120 characters and a note up to 1,000 characters.")
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
		in.Exchanges = scope
		if in.ID == "" {
			if len(u.Wishes) >= 100 {
				return nil, bad("Your list can hold up to 100 ideas.")
			}
			in.ID = token()
			u.Wishes = append(u.Wishes, in)
		} else {
			index := slices.IndexFunc(u.Wishes, func(v Wish) bool { return v.ID == in.ID })
			if index < 0 {
				return nil, problem{404, "Gift idea not found."}
			}
			u.Wishes[index] = in
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
			if e.Invite == in.Code && !e.Archived && len(e.Assignments) == 0 {
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
	if action == "done" {
		if len(e.Assignments) == 0 {
			return nil, bad("Names haven’t been drawn yet.")
		}
		var in struct{ Done bool }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		e.Done = slices.DeleteFunc(e.Done, func(id string) bool { return id == u.ID })
		if in.Done {
			e.Done = append(e.Done, u.ID)
		}
		return a.exchange(e, u), nil
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
			return nil, bad("Details are locked after the draw.")
		}
		if err := details(r, e); err != nil {
			return nil, err
		}
	case "rotate":
		if len(e.Assignments) > 0 {
			return nil, bad("Invitations are closed after the draw.")
		}
		e.Invite = token()
	case "remove":
		if len(e.Assignments) > 0 {
			return nil, bad("Membership is locked after the draw.")
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
			return nil, bad("Names have already been drawn.")
		}
		if len(e.Members) < 3 {
			return nil, bad("You need at least three people to draw names.")
		}
		e.Assignments = draw(e.Members)
		a.drawn(e)
	default:
		return nil, problem{404, "Page not found."}
	}
	return a.exchange(e, u), nil
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
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
	a.secure = os.Getenv("GIFTY_SECURE_COOKIES") == "true"
	a.trustProxy = os.Getenv("GIFTY_TRUST_PROXY") == "true"
	if code := strings.TrimSpace(os.Getenv("GIFTY_ACCESS_CODE")); code != "" {
		a.access = digest("gifty-access:" + code)
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
