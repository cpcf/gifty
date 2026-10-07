package main

import (
	"slices"
	"strconv"
	"strings"
)

// An exchange is one of three kinds. A Secret Santa gives everyone a recipient. A white elephant gives everyone a
// number for the order of picking and nobody a recipient. A group gift is for one friend, who is not a member and
// never sees it.
const (
	kindElephant = "elephant"
	kindGroup    = "group"
)

const defaultSteals = 3

// locked is whether entries have closed: names, or pick numbers, have been given out.
func (e *Exchange) locked() bool { return len(e.Assignments) > 0 || len(e.Order) > 0 }

// live is whether the exchange is under way, so reminders apply. A group gift has no draw and is live from the start.
func (e *Exchange) live() bool { return e.locked() || e.Kind == kindGroup }

func kindName(k string) string {
	if k == "" {
		return "secret"
	}
	return k
}

// kindDetails reads the parts of the create and edit forms that depend on the kind. The kind and who a group gift
// is for are fixed at creation.
func (a *App) kindDetails(e *Exchange, u *User, create bool, kind, steals, forID string) error {
	if create {
		switch kind {
		case "", "secret":
			e.Kind = ""
		case kindElephant, kindGroup:
			e.Kind = kind
		default:
			return bad("Choose a kind of exchange.")
		}
		if e.Kind == kindGroup {
			f := a.state.Users[forID]
			if f == nil || !areFriends(u, f) {
				return bad("Choose a friend to give to. Add friends first.")
			}
			e.For = f.ID
		}
	}
	if e.Kind == kindElephant {
		n := defaultSteals
		if s := strings.TrimSpace(steals); s != "" {
			var err error
			if n, err = strconv.Atoi(s); err != nil || n < 0 || n > 10 {
				return bad("Steals per gift: use a whole number from 0 to 10.")
			}
		}
		e.Steals = n
	}
	return nil
}

// lockElephant gives everyone a pick number, uniformly at random.
func (e *Exchange) lockElephant() {
	e.Order = slices.Clone(e.Members)
	shuffle(e.Order)
}

// kindExtras adds what depends on the kind to an exchange as one member sees it.
func (a *App) kindExtras(v map[string]any, e *Exchange, u *User) {
	v["kind"] = kindName(e.Kind)
	switch e.Kind {
	case kindElephant:
		v["steals"] = e.Steals
		if e.locked() {
			v["order"] = e.Order
			v["ready"] = slices.Contains(e.Ready, u.ID)
		}
	case kindGroup:
		f := a.state.Users[e.For]
		out := map[string]any{"id": f.ID, "name": f.Name, "friends": areFriends(u, f)}
		if areFriends(u, f) {
			out["birthday"] = f.shown()
			out["wishes"] = wishesForFriend(f, u)
		}
		v["for"] = out
	}
}

// canJoin refuses the people a group gift can't take: the person it is for, who must never learn of it, and anyone
// who isn't their friend.
func (a *App) canJoin(e *Exchange, u *User) error {
	if e.Kind != kindGroup {
		return nil
	}
	f := a.state.Users[e.For]
	if f == nil || f.ID == u.ID {
		return problem{404, "This invitation is closed or no longer exists."}
	}
	if !areFriends(u, f) {
		return bad("This is a gift for one of your friends’ friends. Add the person it’s for as a friend first.")
	}
	return nil
}
