package app

import (
	"crypto/subtle"
	"net/http"
	"slices"
	"time"
)

type user struct {
	ID     string
	Name   string
	Email  string
	Salt   string
	Hash   string
	Wishes []wish
	// Email state: tokens are stored as digests, like session tokens.
	Verified      bool      `json:",omitempty"`
	VerifyHash    string    `json:",omitempty"`
	VerifyExpires time.Time `json:",omitzero"`
	ResetHash     string    `json:",omitempty"`
	ResetExpires  time.Time `json:",omitzero"`
	NoEmail       bool      `json:",omitempty"`
	// OwnReminders means Reminders replaces each organiser's default in every exchange.
	OwnReminders bool       `json:",omitempty"`
	Reminders    []reminder `json:",omitempty"`
	LastMail     time.Time  `json:",omitzero"`  // last account email, for the resend cooldown
	MailDay      string     `json:",omitempty"` // with MailCount, caps account emails per day
	MailCount    int        `json:",omitempty"`
	NotifyDay    string     `json:",omitempty"` // with NotifyCount, caps exchange emails per day
	NotifyCount  int        `json:",omitempty"`
	// Friends are people who have agreed to see each other's birthday and shared ideas (see friends.go).
	Birthday       *birthday `json:",omitempty"`
	BirthdayHidden bool      `json:",omitempty"` // saved, but not shown to friends
	Friends        []string  `json:",omitempty"`
	FriendsIn      []string  `json:",omitempty"` // people who asked to be this person's friend
	FriendsOut     []string  `json:",omitempty"` // people this person asked
	FriendCode     string    `json:",omitempty"` // opening the friend link with it sends a request
	NoBirthdayMail bool      `json:",omitempty"`
	BirthdayMailed []string  `json:",omitempty"` // keys of birthday emails already queued
}

type session struct {
	User    string
	Expires time.Time
}

func (a *application) user(r *http.Request) *user {
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

func (a *application) cookie(w http.ResponseWriter, t string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "gifty_session", Value: t, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: age})
}

func (a *application) publicUser(u *user) any {
	wishes := make([]wishOut, len(u.Wishes))
	for i, w := range u.Wishes {
		wishes[i] = publicWish(w)
	}
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "wishes": wishes, "verified": u.Verified, "notify": !u.NoEmail, "ownReminders": u.OwnReminders, "reminders": append([]reminder{}, u.Reminders...), "admin": a.isAdmin(u), "birthday": u.Birthday, "birthdayHidden": u.BirthdayHidden, "birthdayMail": !u.NoBirthdayMail, "birthdayRecent": u.Birthday.recent(time.Now())}
}

// isAdmin needs a confirmed address, so nobody can become an administrator by signing up with an admin's email first.
func (a *application) isAdmin(u *user) bool { return u.Verified && u.Email != "" && a.admins[u.Email] }

const deletedName = "Deleted account"

// removeUser deletes an account. People in drawn exchanges stay on as an empty
// "Deleted account" so everyone else's draw still adds up; otherwise the record goes.
func (a *application) removeUser(u *user) error {
	a.dropped = true
	a.forgetUser(u.ID)
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
		if !e.locked() {
			e.dropMember(u.ID)
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
	a.state.Outbox = slices.DeleteFunc(a.state.Outbox, func(m queuedMail) bool { return m.To == u.Email })
	a.clearClaims(func(w *wish) bool { return w.ClaimedBy == u.ID })
	if !kept {
		delete(a.state.Users, u.ID)
		return nil
	}
	*a.state.Users[u.ID] = user{ID: u.ID, Name: deletedName, Wishes: []wish{}}
	return nil
}

func (a *application) byToken(match func(*user) bool) *user {
	for _, u := range a.state.Users {
		if match(u) {
			return u
		}
	}
	return nil
}

func (a *application) login(w http.ResponseWriter, u *user) {
	now := time.Now()
	for k, s := range a.state.Sessions {
		if now.After(s.Expires) {
			delete(a.state.Sessions, k)
		}
	}
	t := token()
	a.state.Sessions[digest(t)] = session{u.ID, now.Add(30 * 24 * time.Hour)}
	a.cookie(w, t, 30*86400)
}

func (a *application) handleResendVerification(u *user, r *http.Request) (any, error) {
	if !a.mailOn() {
		return nil, bad("This Gifty server doesn’t send emails.")
	}
	if u.Verified {
		return nil, bad("Your email address is already confirmed.")
	}
	if time.Now().Before(u.LastMail.Add(time.Minute)) {
		return nil, problem{429, "Wait a minute before asking for another email."}
	}
	if !a.sendVerify(u, r) {
		return nil, problem{429, "You’ve asked for several emails today. Try again tomorrow."}
	}
	return a.publicUser(u), nil
}

func (a *application) handleAccountMail(u *user, r *http.Request) (any, error) {
	var in struct{ Notify bool }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	u.NoEmail = !in.Notify
	return a.publicUser(u), nil
}

func (a *application) handleDeleteAccount(w http.ResponseWriter, r *http.Request, u *user) (any, error) {
	pre, _ := r.Context().Value(prehashKey{}).(prehash)
	if u.Salt != pre.salt || subtle.ConstantTimeCompare([]byte(pre.hash), []byte(u.Hash)) != 1 {
		a.sec(r, "login_failed")
		return nil, problem{403, "That password isn’t right."}
	}
	if err := a.removeUser(u); err != nil {
		return nil, err
	}
	a.cookie(w, "", -1)
	return map[string]bool{"ok": true}, nil
}
