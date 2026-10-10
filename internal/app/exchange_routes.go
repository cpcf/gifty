package app

import (
	"net/http"
	"slices"
)

func (a *application) exchangeAPI(parts []string, r *http.Request, u *user) (any, error) {
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
		return a.handleLeaveExchange(e, u, r)
	}
	if action == "my-reminders" {
		return a.handleExchangeReminders(e, u, r)
	}
	if action == "ready" {
		return a.handleReady(e, u, r)
	}
	if action == "message" || action == "claim" {
		return a.handleMessageOrClaim(e, u, r, action)
	}
	if action == "delete" {
		return a.handleDeleteExchange(e, u, r)
	}
	if e.Owner != u.ID {
		return nil, problem{403, "Only the organiser can do that."}
	}
	if action == "reveal" {
		return a.handleReveal(e, u, r)
	}
	if e.Archived {
		return nil, bad("This exchange is archived.")
	}
	switch action {
	case "apart":
		return a.handleApart(e, u, r)
	case "reminders":
		return a.handleReminderSchedule(e, u, r)
	case "archive":
		return a.handleArchive(e, u, r)
	case "edit":
		return a.handleEditExchange(e, u, r)
	case "rotate":
		return a.handleRotateInvite(e, u, r)
	case "remove":
		return a.handleRemoveMember(e, u, r)
	case "draw":
		return a.handleDraw(e, u, r)
	default:
		return nil, problem{404, "Page not found."}
	}

}
