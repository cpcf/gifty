package app

import (
	"net/http"
	"slices"
	"time"
)

func (a *application) handleLeaveExchange(e *exchange, u *user, r *http.Request) (any, error) {
	if e.Owner == u.ID {
		return nil, bad("The organiser cannot leave their exchange.")
	}
	if e.locked() || e.Archived {
		return nil, bad("Membership is locked for this exchange.")
	}
	e.dropMember(u.ID)
	return map[string]bool{"ok": true}, nil
}

func (a *application) handleExchangeReminders(e *exchange, u *user, r *http.Request) (any, error) {
	if e.Archived {
		return nil, bad("This exchange is archived.")
	}
	// Default true goes back to the organiser's schedule.
	var in struct {
		Default   bool
		Reminders []reminder
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
			e.MyReminders = map[string][]reminder{}
		}
		e.MyReminders[u.ID] = days
	}
	if e.locked() {
		a.dueReminders(e, u.ID, time.Now())
	}
	return a.exchange(e, u), nil
}

func (a *application) handleReady(e *exchange, u *user, r *http.Request) (any, error) {
	if !e.locked() {
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

func (a *application) handleMessageOrClaim(e *exchange, u *user, r *http.Request, action string) (any, error) {
	var err error
	if action == "message" {
		err = a.say(e, u, r)
	} else {
		err = a.claim(e, u, r)
	}
	if err != nil {
		return nil, err
	}
	return a.exchange(e, u), nil
}

func (a *application) handleDeleteExchange(e *exchange, u *user, r *http.Request) (any, error) {
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

func (a *application) handleReveal(e *exchange, u *user, r *http.Request) (any, error) {
	// Archiving doesn't stop the organiser showing everyone who bought for whom.
	if err := a.reveal(e); err != nil {
		return nil, err
	}
	return a.exchange(e, u), nil
}

func (a *application) handleApart(e *exchange, u *user, r *http.Request) (any, error) {
	if e.Kind != "" {
		return nil, bad("Only a Secret Santa has pairs to keep apart.")
	}
	if e.locked() {
		return nil, bad("Pairs can’t be changed once entries are closed.")
	}
	if err := a.setApart(e, r); err != nil {
		return nil, err
	}
	return a.exchange(e, u), nil
}

func (a *application) handleReminderSchedule(e *exchange, u *user, r *http.Request) (any, error) {
	// Unlike the other details, the schedule can change after the draw.
	var in struct{ Reminders []reminder }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	days, err := parseReminders(in.Reminders)
	if err != nil {
		return nil, err
	}
	e.Reminders = days
	if e.locked() {
		for _, id := range e.Members {
			a.dueReminders(e, id, time.Now())
		}
	}
	return a.exchange(e, u), nil
}

func (a *application) handleArchive(e *exchange, u *user, r *http.Request) (any, error) {
	e.Archived = true
	return a.exchange(e, u), nil
}

func (a *application) handleEditExchange(e *exchange, u *user, r *http.Request) (any, error) {
	if e.locked() {
		return nil, bad("Details can’t be changed once entries are closed.")
	}
	if err := a.details(r, e, u, false); err != nil {
		return nil, err
	}
	return a.exchange(e, u), nil
}

func (a *application) handleRotateInvite(e *exchange, u *user, r *http.Request) (any, error) {
	if e.locked() {
		return nil, bad("Invitations stop working once entries are closed.")
	}
	e.Invite = token()
	return a.exchange(e, u), nil
}

func (a *application) handleRemoveMember(e *exchange, u *user, r *http.Request) (any, error) {
	if e.locked() {
		return nil, bad("Nobody can be removed or leave once entries are closed.")
	}
	var in struct{ ID string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	if in.ID == u.ID {
		return nil, bad("The organiser must stay in the exchange.")
	}
	e.dropMember(in.ID)
	return a.exchange(e, u), nil
}

func (a *application) handleDraw(e *exchange, u *user, r *http.Request) (any, error) {
	if e.locked() {
		return nil, bad("Entries are already closed.")
	}
	if len(e.Members) < 3 {
		return nil, bad("You need at least three people to close entries.")
	}
	switch e.Kind {
	case kindGroup:
		return nil, bad("A group gift has no draw.")
	case kindElephant:
		e.lockElephant()
	default:
		as, ok := draw(e.Members, e.Apart)
		if !ok {
			return nil, bad("The pairs kept apart leave someone with nobody to draw. Remove a pair and try again.")
		}
		e.Assignments = as
	}
	a.drawn(e)
	return a.exchange(e, u), nil
}
