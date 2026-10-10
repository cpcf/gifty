package app

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestNewEndpointsNeedASessionAndSameOrigin(t *testing.T) {
	a := setup(t)
	out := &client{t: t, a: a}
	for _, p := range []string{"friends", "friend/x", "friends/x", "birthday", "account/birthday-mail"} {
		method := "GET"
		if p == "birthday" || p == "account/birthday-mail" {
			method = "POST"
		}
		out.req(method, p, nil, 401)
	}
	for _, p := range []string{"friends/request", "friends/accept", "friends/rotate", "friends/x/claim"} {
		out.req("POST", p, map[string]string{}, 401)
	}
	r := httptest.NewRequest("GET", "/calendar/birthdays", nil)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("calendar file signed out = %d", w.Code)
	}
	// A POST that doesn't say it came from the page is refused before anything happens.
	ana := people(t, a, "Ana")["Ana"]
	for _, p := range []string{"friends/request", "friends/rotate", "birthday"} {
		r := httptest.NewRequest("POST", "/api/"+p, bytes.NewBufferString(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(ana.c.cookie)
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-site POST %s = %d", p, w.Code)
		}
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin POST %s = %d", p, w.Code)
		}
	}
}

func TestNewEndpointsRejectBadInput(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben")
	ana, ben := p["Ana"], p["Ben"]
	makeFriends(t, ben, ana)
	for _, path := range []string{"friends/request", "friends/accept", "friends/decline", "friends/cancel", "friends/remove", "birthday", "account/birthday-mail", "friends/" + ana.id + "/claim"} {
		raw(ben.c, "POST", path, "text/plain", `{}`, 415)                   // JSON only
		raw(ben.c, "POST", path, "application/json", `{`, 400)              // not JSON
		raw(ben.c, "POST", path, "application/json", `{"nonsense":1}`, 400) // unknown fields are refused
		raw(ben.c, "POST", path, "application/json", `{} {}`, 400)          // one form at a time
	}
	raw(ben.c, "DELETE", "friends", "", "", 405)
	ben.c.req("POST", "friends", map[string]string{}, 404)
	ben.c.req("GET", "friends/"+ana.id+"/claim", nil, 404)
	ben.c.req("GET", "friends/"+ana.id+"/nonsense", nil, 404)
	ben.c.req("POST", "friends/nonsense", map[string]string{}, 404)
	// Unknown people and ideas are plain 404s, never a hint about who exists.
	for _, path := range []string{"friends/accept", "friends/decline", "friends/cancel", "friends/remove"} {
		ben.c.req("POST", path, map[string]string{"ID": "nobody"}, 404)
	}
	ben.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": "nothing", "Claim": true}, 404)
	// A birthday needs both a month and a day.
	ana.c.req("POST", "birthday", map[string]any{"Month": 0, "Day": 5}, 400)
	ana.c.req("POST", "birthday", map[string]any{"Month": 5, "Day": 0}, 400)
	ana.c.req("POST", "birthday", map[string]any{"Month": -1, "Day": 5}, 400)
	// Requests can be declined or cancelled only by the right person, and only when they exist.
	cy := people(t, a, "Cy")["Cy"]
	ana.c.req("POST", "friends/decline", map[string]string{"ID": cy.id}, 200) // nothing to decline is a no-op, not an error
	cy.c.req("POST", "friends/request", map[string]string{"Code": ana.c.req("GET", "friends", nil, 200)["code"].(string)}, 200)
	ben.c.req("POST", "friends/cancel", map[string]string{"ID": ana.id}, 200) // not his request: changes nothing
	if len(cy.c.req("GET", "friends", nil, 200)["sent"].([]any)) != 1 {
		t.Fatal("someone else cancelled a request")
	}
	cy.c.req("POST", "friends/cancel", map[string]string{"ID": ana.id}, 200)
	if r := ana.c.req("GET", "friends", nil, 200); len(r["requests"].([]any)) != 0 {
		t.Fatalf("a cancelled request is still waiting: %v", r)
	}
	ana.c.req("POST", "friends/accept", map[string]string{"ID": cy.id}, 404)
}
