package main

import (
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
	"io/fs"
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
}
type Wish struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note"`
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
}
type Session struct {
	User    string
	Expires time.Time
}
type State struct {
	Users     map[string]*User
	Exchanges map[string]*Exchange
	Sessions  map[string]Session
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
	limits map[string]limiter
}
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
	a := &App{path: path, limits: map[string]limiter{}, state: State{map[string]*User{}, map[string]*Exchange{}, map[string]Session{}}}
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
func publicUser(u *User) any {
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "wishes": u.Wishes}
}
func (a *App) exchange(e *Exchange, u *User) any {
	members := []any{}
	for _, id := range e.Members {
		m := a.state.Users[id]
		members = append(members, map[string]any{"id": id, "name": m.Name})
	}
	v := map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "note": e.Note, "owner": e.Owner, "members": members, "drawn": len(e.Assignments) > 0, "archived": e.Archived}
	if u.ID == e.Owner && !e.Archived && len(e.Assignments) == 0 {
		v["invite"] = e.Invite
	}
	if id := e.Assignments[u.ID]; id != "" {
		m := a.state.Users[id]
		v["recipient"] = map[string]any{"name": m.Name, "wishes": m.Wishes}
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
func (a *App) dispatch(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	u := a.user(r)
	if path == "signup" || path == "login" {
		if r.Method != "POST" {
			return nil, problem{405, "Method not allowed."}
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		now := time.Now()
		for k, l := range a.limits {
			if now.After(l.Reset) {
				delete(a.limits, k)
			}
		}
		l := a.limits[ip]
		if l.Count >= 30 {
			return nil, problem{429, "Too many attempts. Try again in 15 minutes."}
		}
		if l.Count == 0 {
			l.Reset = now.Add(15 * time.Minute)
		}
		l.Count++
		a.limits[ip] = l
		var in struct{ Name, Email, Password string }
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
			if found != nil {
				return nil, bad("An account already uses that email. Sign in instead.")
			}
			salt := token()
			found = &User{ID: token(), Name: in.Name, Email: in.Email, Salt: salt, Hash: password(in.Password, salt), Wishes: []Wish{}}
			a.state.Users[found.ID] = found
		} else {
			if len(in.Password) > 256 {
				return nil, bad("Email or password is incorrect.")
			}
			salt := "missing-account"
			want := strings.Repeat("0", 64)
			if found != nil {
				salt = found.Salt
				want = found.Hash
			}
			got := password(in.Password, salt)
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				return nil, problem{401, "Email or password is incorrect."}
			}
		}
		for k, s := range a.state.Sessions {
			if now.After(s.Expires) {
				delete(a.state.Sessions, k)
			}
		}
		t := token()
		a.state.Sessions[digest(t)] = Session{found.ID, now.Add(30 * 24 * time.Hour)}
		a.cookie(w, t, 30*86400)
		return publicUser(found), nil
	}
	if strings.HasPrefix(path, "invite/") && r.Method == "GET" {
		code := strings.TrimPrefix(path, "invite/")
		for _, e := range a.state.Exchanges {
			if e.Invite == code && !e.Archived && len(e.Assignments) == 0 {
				return map[string]any{"name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency}, nil
			}
		}
		return nil, problem{404, "This invitation is closed or no longer exists."}
	}
	if u == nil {
		return nil, problem{401, "Sign in to continue."}
	}
	if path == "me" && r.Method == "GET" {
		return publicUser(u), nil
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
		return publicUser(u), nil
	}
	if strings.HasPrefix(path, "wishes/") && r.Method == "POST" {
		id := strings.TrimPrefix(path, "wishes/")
		index := slices.IndexFunc(u.Wishes, func(v Wish) bool { return v.ID == id })
		if index < 0 {
			return nil, problem{404, "Gift idea not found."}
		}
		u.Wishes = slices.Delete(u.Wishes, index, index+1)
		return publicUser(u), nil
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
		e := &Exchange{ID: token(), Owner: u.ID, Invite: token(), Members: []string{u.ID}}
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
	if e.Owner != u.ID {
		return nil, problem{403, "Only the organiser can do that."}
	}
	if e.Archived {
		return nil, bad("This exchange is archived.")
	}
	switch action {
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
	if err != nil || d.Before(time.Now().UTC().Truncate(24*time.Hour)) {
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
	files, _ := fs.Sub(assets, "web")
	static := http.FileServer(http.FS(files))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if r.URL.Path == "/app.js" || r.URL.Path == "/style.css" || r.URL.Path == "/favicon.svg" {
			static.ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
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
	addr := os.Getenv("GIFTY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
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
