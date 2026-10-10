package app

import (
	"net/http"
	"slices"
	"strings"
	"time"
)

type exchange struct {
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
	Reminders   []reminder
	MyReminders map[string][]reminder `json:",omitempty"` // members' own schedules, overriding the default
	Reminded    map[string][]string   `json:",omitempty"` // keys of reminders already sent (or skipped), per member
	Ready       []string              `json:",omitempty"` // members with their gift ready to give, who want no more reminders
	// Apart lists pairs who must not draw each other (couples, housemates). Only the organiser sees them, and only before the draw.
	Apart [][2]string `json:",omitempty"`
	// Revealed means the organiser has shown everyone who bought for whom.
	Revealed bool `json:",omitempty"`
	// Threads holds the anonymous conversations, one per giver (keyed by the giver's ID) with the person they buy for.
	Threads map[string][]message `json:",omitempty"`
	// Kind is "" for Secret Santa, "elephant" or "group" (see exchange_kinds.go).
	Kind string `json:",omitempty"`
	// Order is the pick order of a white elephant, set when entries close. Steals is the room's rule.
	Order  []string `json:",omitempty"`
	Steals int      `json:",omitempty"`
	// For is who a group gift is for. They are not a member and never see it.
	For string `json:",omitempty"`
}

// removeExchange deletes an exchange and clears it from the scope of ideas that named it.
func (a *application) removeExchange(id string) {
	a.dropped = true
	delete(a.state.Exchanges, id)
	for _, v := range a.state.Users {
		// An idea meant only for this exchange goes with it: with no exchange left in its scope it would
		// otherwise be shown in every exchange, to people it was kept from.
		v.Wishes = slices.DeleteFunc(v.Wishes, func(w wish) bool {
			return len(w.Exchanges) > 0 && !slices.ContainsFunc(w.Exchanges, func(x string) bool { return x != id })
		})
		for i := range v.Wishes {
			v.Wishes[i].Exchanges = slices.DeleteFunc(v.Wishes[i].Exchanges, func(x string) bool { return x == id })
		}
	}
	a.clearClaims(func(w *wish) bool { return w.ClaimedIn == id })
}

func (a *application) exchange(e *exchange, u *user) any {
	members := []any{}
	for _, id := range e.Members {
		m := a.state.Users[id]
		member := map[string]any{"id": id, "name": m.Name}
		if e.locked() {
			// Whether someone has their gift is shared with the group; who it is for never is.
			member["ready"] = slices.Contains(e.Ready, id)
		}
		members = append(members, member)
	}
	v := map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "note": e.Note, "owner": e.Owner, "members": members, "drawn": e.locked(), "archived": e.Archived, "reminders": e.defaultReminders()}
	mine, source := a.reminders(e, u.ID)
	v["mine"] = map[string]any{"reminders": mine, "source": source}
	if u.ID == e.Owner && !e.Archived && !e.locked() {
		v["invite"] = e.Invite
	}
	if id := e.Assignments[u.ID]; id != "" {
		m := a.state.Users[id]
		v["recipient"] = map[string]any{"id": m.ID, "name": m.Name, "wishes": wishesFor(m, e, u.ID)}
		v["ready"] = slices.Contains(e.Ready, u.ID)
	}
	a.exchangeExtras(v, e, u)
	return v
}

func (a *application) details(r *http.Request, e *exchange, u *user, create bool) error {
	var in struct{ Name, Date, Budget, Currency, Note, Kind, Steals, For string }
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
	return a.kindDetails(e, u, create, in.Kind, in.Steals, in.For)
}

// dropMember takes someone out of an exchange that hasn't drawn yet, along with any pair they were in.
func (e *exchange) dropMember(id string) {
	e.Members = slices.DeleteFunc(e.Members, func(m string) bool { return m == id })
	e.Apart = slices.DeleteFunc(e.Apart, func(p [2]string) bool { return p[0] == id || p[1] == id })
}

// exchangeExtras adds what depends on the draw, the conversations and the reveal. A person sees only the two
// conversations they are in, and the pairs kept apart only if they organise the exchange.
func (a *application) exchangeExtras(v map[string]any, e *exchange, u *user) {
	a.kindExtras(v, e, u)
	drawn := e.locked()
	if u.ID == e.Owner && !drawn && e.Kind == "" {
		apart := [][2]string{}
		v["apart"] = append(apart, e.Apart...)
	}
	if drawn {
		if e.Assignments[u.ID] != "" {
			v["toRecipient"] = threadView(e.Threads[u.ID], true)
		}
		for giver, to := range e.Assignments {
			if to == u.ID {
				v["fromGiver"] = threadView(e.Threads[giver], false)
			}
		}
	}
	if u.ID == e.Owner && e.revealable(time.Now()) {
		v["canReveal"] = true
	}
	if e.Revealed {
		v["revealed"] = true
		pairs := []map[string]string{}
		for _, id := range e.Members {
			if to := e.Assignments[id]; to != "" {
				pairs = append(pairs, map[string]string{"giver": id, "recipient": to})
			}
		}
		v["pairs"] = pairs
	}
}

func (a *application) handleInvite(path string) (any, error) {
	code := strings.TrimPrefix(path, "invite/")
	for _, e := range a.state.Exchanges {
		if same(e.Invite, code) && !e.Archived && !e.locked() {
			return map[string]any{"name": e.Name, "date": e.Date, "budget": e.Budget, "currency": e.Currency, "organiser": a.state.Users[e.Owner].Name, "people": len(e.Members), "kind": kindName(e.Kind)}, nil
		}
	}
	return nil, problem{404, "This invitation is closed or no longer exists."}
}

func (a *application) handleListExchanges(u *user) (any, error) {
	out := []any{}
	for _, e := range a.state.Exchanges {
		if slices.Contains(e.Members, u.ID) {
			out = append(out, a.exchange(e, u))
		}
	}
	return out, nil
}

func (a *application) handleJoinExchange(u *user, r *http.Request) (any, error) {
	var in struct{ Code string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	for _, e := range a.state.Exchanges {
		if same(e.Invite, in.Code) && !e.Archived && !e.locked() {
			if !slices.Contains(e.Members, u.ID) {
				if err := a.canJoin(e, u); err != nil {
					return nil, err
				}
				if len(e.Members) >= 100 {
					return nil, bad("This exchange is full.")
				}
				e.Members = append(e.Members, u.ID)
				if e.live() {
					a.dueReminders(e, u.ID, time.Now())
				}
			}
			return a.exchange(e, u), nil
		}
	}
	return nil, problem{404, "This invitation is closed or no longer exists."}
}

func (a *application) handleCreateExchange(u *user, r *http.Request) (any, error) {
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
	e := &exchange{ID: token(), Owner: u.ID, Invite: token(), Members: []string{u.ID}, Reminders: slices.Clone(defaultReminders)}
	if err := a.details(r, e, u, true); err != nil {
		return nil, err
	}
	a.state.Exchanges[e.ID] = e
	if e.live() {
		a.dueReminders(e, u.ID, time.Now()) // a group gift has no draw to skip reminders that are already late
	}
	return a.exchange(e, u), nil
}
