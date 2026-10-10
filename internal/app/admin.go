package app

import (
	"log"
	"net/http"
	"slices"
	"strings"
)

// admin serves the administrator's pages. It lists accounts and exchanges and can
// remove them, but never exposes assignments, wish lists or invitation links.
func (a *application) admin(path string, r *http.Request, u *user) (any, error) {
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
			exchanges = append(exchanges, map[string]any{"id": e.ID, "name": e.Name, "date": e.Date, "organiser": a.state.Users[e.Owner].Name, "people": len(e.Members), "drawn": e.locked(), "archived": e.Archived})
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
