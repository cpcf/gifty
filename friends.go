package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Birthday is a month and day, and optionally the year. Without a year nobody is shown an age.
type Birthday struct {
	Month int `json:"month"`
	Day   int `json:"day"`
	Year  int `json:"year,omitempty"`
}

func midnight(t time.Time) time.Time { return t.UTC().Truncate(24 * time.Hour) }

// on is the birthday in year. A 29 February is kept on 28 February in years that have none.
func (b *Birthday) on(year int) time.Time {
	d := time.Date(year, time.Month(b.Month), b.Day, 0, 0, 0, 0, time.UTC)
	if d.Month() != time.Month(b.Month) {
		d = time.Date(year, time.February, 28, 0, 0, 0, 0, time.UTC)
	}
	return d
}

// next is the first birthday on or after today.
func (b *Birthday) next(now time.Time) time.Time {
	t := midnight(now)
	if d := b.on(t.Year()); !d.Before(t) {
		return d
	}
	return b.on(t.Year() + 1)
}

// last is the most recent birthday on or before today.
func (b *Birthday) last(now time.Time) time.Time {
	t := midnight(now)
	if d := b.on(t.Year()); !d.After(t) {
		return d
	}
	return b.on(t.Year() - 1)
}

// recent is whether the birthday has passed within the last two weeks, when the owner is nudged to sort their list.
func (b *Birthday) recent(now time.Time) bool {
	if b == nil {
		return false
	}
	days := int(midnight(now).Sub(b.last(now)).Hours() / 24)
	return days >= 1 && days <= 14
}

func (b *Birthday) check(now time.Time) error {
	if b.Month < 1 || b.Month > 12 || b.Day < 1 || b.Day > time.Date(2000, time.Month(b.Month)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return bad("Enter a real day and month.")
	}
	if b.Year != 0 && (b.Year < 1900 || b.Year > now.UTC().Year()) {
		return bad("Enter a year from 1900 to this year, or leave it empty.")
	}
	return nil
}

// shown is the birthday as friends see it, or nil if it isn't set or its owner keeps it hidden.
func (u *User) shown() *Birthday {
	if u.BirthdayHidden {
		return nil
	}
	return u.Birthday
}

func areFriends(u, v *User) bool {
	return u != nil && v != nil && u.ID != v.ID && slices.Contains(u.Friends, v.ID) && slices.Contains(v.Friends, u.ID)
}

const (
	maxFriends  = 200
	maxRequests = 20 // outstanding, sent by one person; a person can hold up to twice that waiting
)

func (a *App) friendCard(v *User) map[string]any {
	ideas := 0
	for _, w := range v.Wishes {
		if w.Friends && w.Status == "" {
			ideas++
		}
	}
	return map[string]any{"id": v.ID, "name": v.Name, "birthday": v.shown(), "ideas": ideas}
}

func (a *App) people(ids []string) []map[string]string {
	out := []map[string]string{}
	for _, id := range ids {
		if v := a.state.Users[id]; v != nil {
			out = append(out, map[string]string{"id": v.ID, "name": v.Name})
		}
	}
	return out
}

func (a *App) friendsView(u *User) map[string]any {
	friends := []map[string]any{}
	for _, id := range u.Friends {
		if v := a.state.Users[id]; v != nil {
			friends = append(friends, a.friendCard(v))
		}
	}
	return map[string]any{"code": u.FriendCode, "friends": friends, "requests": a.people(u.FriendsIn), "sent": a.people(u.FriendsOut)}
}

// wishesForFriend is what a friend sees of owner's list: ideas shown to friends, minus sorted ones (unless the
// friend had marked one, who is told to check).
func wishesForFriend(owner *User, friend *User) []wishOut {
	out := []wishOut{}
	for _, w := range owner.Wishes {
		if !w.Friends {
			continue
		}
		mine := w.ClaimedBy != "" && w.ClaimedBy == friend.ID
		if w.Status != "" && !mine {
			continue
		}
		o := publicWish(w)
		o.Exchanges, o.Friends, o.NoExchanges = nil, false, false
		switch {
		case mine:
			o.Claim = "mine"
		case w.ClaimedBy != "":
			o.Claim = "other"
		}
		out = append(out, o)
	}
	return out
}

func drop(s []string, id string) []string {
	return slices.DeleteFunc(s, func(x string) bool { return x == id })
}

func befriend(u, v *User) {
	for _, p := range [][2]*User{{u, v}, {v, u}} {
		p[0].FriendsIn, p[0].FriendsOut = drop(p[0].FriendsIn, p[1].ID), drop(p[0].FriendsOut, p[1].ID)
		if !slices.Contains(p[0].Friends, p[1].ID) {
			p[0].Friends = append(p[0].Friends, p[1].ID)
		}
	}
}

// unfriend ends a friendship and releases each side's claims on the other's list, so nobody is left holding an
// idea on a list they can no longer see.
func unfriend(u, v *User) {
	for _, p := range [][2]*User{{u, v}, {v, u}} {
		p[0].Friends, p[0].FriendsIn, p[0].FriendsOut = drop(p[0].Friends, p[1].ID), drop(p[0].FriendsIn, p[1].ID), drop(p[0].FriendsOut, p[1].ID)
		for i := range p[0].Wishes {
			if w := &p[0].Wishes[i]; w.ClaimedIn == friendClaim && w.ClaimedBy == p[1].ID {
				w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
			}
		}
	}
}

// forgetUser takes a deleted account out of everyone's friends and requests, and removes the group gifts
// that were for them (which only their friends could see).
func (a *App) forgetUser(id string) {
	for _, v := range a.state.Users {
		v.Friends, v.FriendsIn, v.FriendsOut = drop(v.Friends, id), drop(v.FriendsIn, id), drop(v.FriendsOut, id)
	}
	for eid, e := range a.state.Exchanges {
		if e.Kind == kindGroup && e.For == id {
			a.removeExchange(eid)
		}
	}
}

func (a *App) byCode(code string) *User {
	if code == "" {
		return nil
	}
	for _, v := range a.state.Users {
		if v.FriendCode != "" && same(v.FriendCode, code) {
			return v
		}
	}
	return nil
}

// friendsAPI serves /api/friends and everything under it.
func (a *App) friendsAPI(u *User, parts []string, r *http.Request) (any, error) {
	if len(parts) == 0 && r.Method == "GET" {
		return a.friendsView(u), nil
	}
	if len(parts) == 1 && r.Method == "POST" {
		switch parts[0] {
		case "rotate":
			u.FriendCode = token()
			return a.friendsView(u), nil
		case "request":
			var in struct{ Code string }
			if err := readJSON(r, &in); err != nil {
				return nil, err
			}
			t := a.byCode(in.Code)
			if t == nil {
				return nil, problem{404, "This friend link no longer works."}
			}
			if t.ID == u.ID {
				return nil, bad("That’s your own friend link.")
			}
			switch {
			case areFriends(u, t) || slices.Contains(t.FriendsIn, u.ID):
			case slices.Contains(u.FriendsIn, t.ID):
				// They already asked, so opening their link as well settles it.
				befriend(u, t)
			default:
				if len(u.FriendsOut) >= maxRequests || len(t.FriendsIn) >= 2*maxRequests || len(u.Friends) >= maxFriends || len(t.Friends) >= maxFriends {
					return nil, bad("You can’t send another request right now.")
				}
				t.FriendsIn = append(t.FriendsIn, u.ID)
				u.FriendsOut = append(u.FriendsOut, t.ID)
			}
			return a.friendsView(u), nil
		case "accept", "decline", "cancel", "remove":
			var in struct{ ID string }
			if err := readJSON(r, &in); err != nil {
				return nil, err
			}
			v := a.state.Users[in.ID]
			if v == nil {
				return nil, problem{404, "Person not found."}
			}
			switch parts[0] {
			case "accept":
				if !slices.Contains(u.FriendsIn, v.ID) {
					return nil, problem{404, "That request is no longer there."}
				}
				if len(u.Friends) >= maxFriends || len(v.Friends) >= maxFriends {
					return nil, bad("Friend lists hold up to 200 people.")
				}
				befriend(u, v)
			case "decline":
				u.FriendsIn, v.FriendsOut = drop(u.FriendsIn, v.ID), drop(v.FriendsOut, u.ID)
			case "cancel":
				u.FriendsOut, v.FriendsIn = drop(u.FriendsOut, v.ID), drop(v.FriendsIn, u.ID)
			case "remove":
				unfriend(u, v)
			}
			return a.friendsView(u), nil
		}
	}
	if len(parts) == 0 || len(parts) > 2 {
		return nil, problem{404, "Page not found."}
	}
	// /api/friends/{id} and /api/friends/{id}/claim
	v := a.state.Users[parts[0]]
	if v == nil || !areFriends(u, v) {
		return nil, problem{404, "Friend not found."}
	}
	if len(parts) == 1 && r.Method == "GET" {
		return map[string]any{"id": v.ID, "name": v.Name, "birthday": v.shown(), "wishes": wishesForFriend(v, u)}, nil
	}
	if len(parts) == 2 && parts[1] == "claim" && r.Method == "POST" {
		if err := a.claimAsFriend(u, v, r); err != nil {
			return nil, err
		}
		return map[string]any{"id": v.ID, "name": v.Name, "birthday": v.shown(), "wishes": wishesForFriend(v, u)}, nil
	}
	return nil, problem{404, "Page not found."}
}

// claimAsFriend records that u is getting one of owner's ideas, as a friend. Others who can see the idea, friends or
// the people buying for owner, are told someone has it. Owner never is.
func (a *App) claimAsFriend(u, owner *User, r *http.Request) error {
	var in struct {
		Wish  string
		Claim bool
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	i := slices.IndexFunc(owner.Wishes, func(w Wish) bool { return w.ID == in.Wish })
	if i < 0 {
		return problem{404, "Gift idea not found."}
	}
	w := &owner.Wishes[i]
	mine := w.ClaimedBy == u.ID
	if !in.Claim {
		if mine {
			w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
		}
		return nil
	}
	if !w.Friends || (w.Status != "" && !mine) {
		return problem{404, "Gift idea not found."}
	}
	switch {
	case w.ClaimedBy != "" && !mine:
		return problem{409, "Someone else is already getting this one."}
	case w.Status != "":
		return bad("They’ve sorted this one out already.")
	case !mine:
		w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = u.ID, friendClaim, time.Now().UTC()
	}
	return nil
}

func (a *App) setBirthday(u *User, r *http.Request) error {
	var in struct {
		Month, Day, Year int
		Show             bool
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Month == 0 && in.Day == 0 {
		u.Birthday, u.BirthdayHidden = nil, false
		return nil
	}
	b := &Birthday{in.Month, in.Day, in.Year}
	if err := b.check(time.Now()); err != nil {
		return err
	}
	u.Birthday, u.BirthdayHidden = b, !in.Show
	return nil
}

// serveBirthdays sends u a calendar file with each friend's birthday as a yearly all-day event.
func (a *App) serveBirthdays(w http.ResponseWriter, u *User) {
	now := time.Now()
	var b strings.Builder
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Gifty//EN", "CALSCALE:GREGORIAN"} {
		icsLine(&b, l)
	}
	for _, id := range u.Friends {
		f := a.state.Users[id]
		if f == nil || f.shown() == nil {
			continue
		}
		day := f.shown().next(now)
		rule := "RRULE:FREQ=YEARLY"
		if f.shown().Month == 2 && f.shown().Day == 29 {
			rule += ";BYMONTH=2;BYMONTHDAY=-1" // the last day of February, whichever it is
		}
		for _, l := range []string{"BEGIN:VEVENT", "UID:birthday-" + f.ID + "@gifty", "DTSTAMP:" + now.UTC().Format("20060102T150405Z"),
			"DTSTART;VALUE=DATE:" + day.Format("20060102"), "DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102"),
			rule, "SUMMARY:" + icsText(f.Name+"’s birthday"), "TRANSP:TRANSPARENT"} {
			icsLine(&b, l)
		}
		if a.base != "" {
			icsLine(&b, "URL:"+a.base+"/#friends/"+f.ID)
		}
		icsLine(&b, "END:VEVENT")
	}
	icsLine(&b, "END:VCALENDAR")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gifty-birthdays.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write([]byte(b.String()))
}

// claimLapse is how long after a birthday friends' claims on the owner's ideas last.
const claimLapse = 3 * 24 * time.Hour

// birthdays releases friends' claims once the birthday has passed, so next year's list starts fresh, and queues
// the birthday emails. It reports whether anything changed.
func (a *App) birthdays(now time.Time) bool {
	changed := false
	today := midnight(now)
	for _, o := range a.state.Users {
		if o.Birthday == nil {
			continue
		}
		cutoff := o.Birthday.last(now).Add(claimLapse)
		if today.Before(cutoff) {
			continue
		}
		for i := range o.Wishes {
			if w := &o.Wishes[i]; w.ClaimedIn == friendClaim && w.ClaimedAt.Before(cutoff) {
				w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
				changed = true
			}
		}
	}
	if !a.mailOn() {
		return changed
	}
	for _, u := range a.state.Users {
		if len(u.Friends) == 0 && len(u.BirthdayMailed) == 0 {
			continue
		}
		if u.NoEmail || u.NoBirthdayMail || !u.Verified {
			continue
		}
		for _, id := range u.Friends {
			f := a.state.Users[id]
			if f == nil || f.shown() == nil {
				continue
			}
			day := f.shown().next(now)
			left := int(day.Sub(today).Hours() / 24)
			if left != 7 && left != 0 {
				continue
			}
			key := fmt.Sprintf("%s:%d:%d", f.ID, day.Year(), left)
			if slices.Contains(u.BirthdayMailed, key) {
				continue
			}
			u.BirthdayMailed = append(u.BirthdayMailed, key)
			changed = true
			subject := f.Name + "’s birthday is in a week"
			when := "in a week, on " + day.Format("Monday 2 January")
			if left == 0 {
				subject, when = "It’s "+f.Name+"’s birthday today", "today"
			}
			a.queue(u, true, subject, fmt.Sprintf("%s’s birthday is %s.\n\nSee their list on Gifty:\n%s/#friends/%s", f.Name, when, a.base, f.ID))
		}
		// Keys for years gone by are no use any more.
		u.BirthdayMailed = slices.DeleteFunc(u.BirthdayMailed, func(k string) bool {
			var id string
			var year, left int
			fmt.Sscanf(strings.ReplaceAll(k, ":", " "), "%s %d %d", &id, &year, &left)
			return year < today.Year()
		})
	}
	return changed
}
