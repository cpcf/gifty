package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// prehash carries a password hash computed before the request takes the state lock.
type prehash struct{ salt, hash string }

type prehashKey struct{}

// authPaths are rate limited per client before the lock; signup, login and reset also hash a password.
var authPaths = map[string]bool{"signup": true, "login": true, "reset": true, "reset/request": true, "access": true, "account/delete": true, "verify": true}

// isAuthPath includes the unauthenticated token endpoints, which scan every account.
func isAuthPath(path string) bool { return authPaths[path] || strings.HasPrefix(path, "unsubscribe/") }

func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// gate is the access-gate credential for code: an HMAC under the server's own key.
func (a *application) gate(code string) string {
	return a.mac("gifty-access:" + code)
}

// mac is an HMAC of s under the server key.
func (a *application) mac(s string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}

// unsubToken is the unsubscribe credential in u's emails. It is derived, so it isn't stored anywhere.
func (a *application) unsubToken(u *user) string { return a.mac("gifty-unsub:" + u.ID) }

// same compares two secrets in constant time.
func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

func password(p, s string) string {
	b, err := pbkdf2.Key(sha256.New, p, []byte(s), 600000, 32)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// prepare rate limits an authentication request and, for those carrying a
// password, hashes it without holding the state lock, so slow hashing never
// stalls other requests. It answers the request itself when it refuses it.
func (a *application) prepare(w http.ResponseWriter, r *http.Request, path string) (*http.Request, bool) {
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
		if u := a.byToken(func(u *user) bool { return u.Email != "" && u.Email == email }); u != nil {
			salt = u.Salt
			if allowed && hash {
				l := a.fails[u.ID]
				locked = !l.take(now, 10)
				a.fails[u.ID] = l
			}
		}
	case "reset":
		h := digest(in.Token)
		hash = a.byToken(func(v *user) bool { return v.ResetHash != "" && v.ResetHash == h && now.Before(v.ResetExpires) }) != nil
	case "account/delete":
		salt = "missing-account"
		if u := a.user(r); u != nil {
			salt, hash = u.Salt, true
		}
	}
	a.mu.Unlock()
	if !allowed {
		a.sec(r, "auth_rate_limited")
		send(w, 429, map[string]string{"error": "Too many attempts. Try again in 15 minutes."})
		return r, false
	}
	if locked {
		a.sec(r, "account_locked")
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

func (a *application) hasAccess(r *http.Request) bool {
	if a.access == "" {
		return true
	}
	c, err := r.Cookie("gifty_access")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.access)) == 1
}

// grantAccess remembers on this browser that it is past the access gate.
func (a *application) grantAccess(w http.ResponseWriter) {
	if a.access != "" {
		http.SetCookie(w, &http.Cookie{Name: "gifty_access", Value: a.access, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: 90 * 86400})
	}
}

func (a *application) openInvite(code string) bool {
	for _, e := range a.state.Exchanges {
		if code != "" && same(e.Invite, code) && !e.Archived && !e.locked() {
			return true
		}
	}
	return false
}

func (a *application) handleAccess(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct{ Code string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	// Wrong guesses share one budget across every client, so rotating addresses doesn't help. Past it, even
	// the right code is refused until the window ends, so the answer never confirms a guess.
	now := time.Now()
	if a.gateFails.Count >= 20 && now.Before(a.gateFails.Reset) {
		a.sec(r, "access_code_exhausted")
		return nil, problem{429, "Too many wrong codes. Try again in 15 minutes."}
	}
	if a.access == "" || !same(a.gate(strings.TrimSpace(in.Code)), a.access) {
		a.sec(r, "access_code_wrong")
		a.gateFails.take(now, 20)
		return nil, problem{403, "That code isn’t right."}
	}
	a.grantAccess(w)
	return map[string]bool{"ok": true}, nil
}

func (a *application) handleVerify(r *http.Request) (any, error) {
	var in struct{ Token string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	h := digest(in.Token)
	v := a.byToken(func(v *user) bool {
		return v.VerifyHash != "" && same(v.VerifyHash, h) && time.Now().Before(v.VerifyExpires)
	})
	if v == nil {
		a.sec(r, "token_refused")
		return nil, problem{404, "This confirmation link has expired or already been used."}
	}
	v.Verified, v.VerifyHash, v.VerifyExpires = true, "", time.Time{}
	return map[string]bool{"ok": true}, nil
}

func (a *application) handleUnsubscribe(path string, r *http.Request) (any, error) {
	// Also used by mail clients' one-click unsubscribe, which posts a form, so the body is ignored.
	t := strings.TrimPrefix(path, "unsubscribe/")
	v := a.byToken(func(v *user) bool {
		return same(a.unsubToken(v), t)
	})
	if v == nil {
		a.sec(r, "token_refused")
		return nil, problem{404, "This link doesn’t match an account."}
	}
	v.NoEmail = true
	return map[string]bool{"ok": true}, nil
}

func (a *application) handleCredentials(w http.ResponseWriter, r *http.Request, path string) (any, error) {
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
		if v := a.byToken(func(v *user) bool { return v.Email != "" && v.Email == email }); v != nil && a.mailOn() && now.After(v.ResetExpires.Add(2*time.Minute-time.Hour)) {
			a.sendReset(v, r)
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
		v := a.byToken(func(v *user) bool { return v.ResetHash != "" && same(v.ResetHash, h) && now.Before(v.ResetExpires) })
		if v == nil {
			a.sec(r, "token_refused")
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
	var found *user
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
			a.sec(r, "access_denied")
			return nil, problem{403, "Enter the access code or open your invitation link first."}
		}
		if found != nil {
			// Tell the owner, so a probe for their address doesn't go unnoticed.
			if found.Verified && a.accountMailAllowed(found, r, now) {
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
		found = &user{ID: token(), Name: in.Name, Email: in.Email, Salt: pre.salt, Hash: pre.hash, Wishes: []wish{}, FriendCode: token()}
		a.state.Users[found.ID] = found
		a.sendVerify(found, r)
		a.grantAccess(w) // an invited guest never sees the code, and still needs to sign in again later
	} else {
		if !a.hasAccess(r) {
			a.sec(r, "access_denied")
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
			a.sec(r, "login_failed")
			return nil, problem{401, "Email or password is incorrect."}
		}
		delete(a.fails, found.ID)
	}
	a.login(w, found)
	return a.publicUser(found), nil
}

func (a *application) handleLogout(w http.ResponseWriter, r *http.Request) (any, error) {
	c, _ := r.Cookie("gifty_session")
	delete(a.state.Sessions, digest(c.Value))
	a.cookie(w, "", -1)
	return map[string]bool{"ok": true}, nil
}
