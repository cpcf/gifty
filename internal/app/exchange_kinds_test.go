package app

import (
	"strings"
	"testing"
	"time"
)

func TestGroupGiftMembership(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy", "Fay")
	ana, ben, cy, fay := p["Ana"], p["Ben"], p["Cy"], p["Fay"]
	for _, x := range []*person{ana, ben, cy} {
		makeFriends(t, x, fay)
	}
	e := ana.c.req("POST", "exchanges", map[string]string{"Name": "Gift", "Date": "2099-12-20", "Budget": "30", "Currency": "GBP", "Kind": "group", "For": fay.id}, 200)
	id := e["id"].(string)
	for _, x := range []*person{ben, cy} {
		x.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	// Editing changes the details but never who it is for or what kind it is.
	ana.c.req("POST", "exchanges/"+id+"/edit", map[string]string{"Name": "Renamed", "Date": "2099-12-21", "Budget": "40", "Currency": "GBP", "Kind": "elephant", "For": cy.id}, 200)
	if x := a.state.Exchanges[id]; x.Kind != kindGroup || x.For != fay.id || x.Name != "Renamed" {
		t.Fatalf("after edit: %+v", x)
	}
	// The organiser can remove a member, who can then come back; a member can leave.
	ana.c.req("POST", "exchanges/"+id+"/remove", map[string]string{"ID": ben.id}, 200)
	ben.c.req("GET", "exchanges/"+id, nil, 404)
	ben.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	cy.c.req("POST", "exchanges/"+id+"/leave", nil, 200)
	cy.c.req("GET", "exchanges/"+id, nil, 404)
	// Archiving closes the invitation, and nobody can join an archived gift.
	ana.c.req("POST", "exchanges/"+id+"/archive", nil, 200)
	cy.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 404)
	// The person it is for is never a member, however it ends.
	if m := a.state.Exchanges[id].Members; len(m) != 2 {
		t.Fatalf("members = %v", m)
	}
	// The kinds that have no draw refuse the draw's side effects: no recipient, no messages, no keep apart.
	if got := ana.c.req("GET", "exchanges/"+id, nil, 200); got["recipient"] != nil || got["apart"] != nil {
		t.Fatalf("group gift carries secret-santa data: %v", got)
	}
}

func TestWhiteElephantLifecycle(t *testing.T) {
	a, _ := mailSetup(t)
	p := people(t, a, "Ana", "Ben", "Cy", "Dee")
	for _, x := range p {
		a.state.Users[x.id].Verified = true
	}
	ana, ben, cy, dee := p["Ana"], p["Ben"], p["Cy"], p["Dee"]
	e := ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": future(30), "Budget": "20", "Currency": "GBP", "Kind": "elephant", "Steals": ""}, 200)
	id := e["id"].(string)
	if e["steals"].(float64) != defaultSteals {
		t.Fatalf("blank steals = %v", e["steals"])
	}
	for _, x := range []*person{ben, cy, dee} {
		x.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	// Before entries close people can leave and be removed, as in any exchange.
	dee.c.req("POST", "exchanges/"+id+"/leave", nil, 200)
	ana.c.req("POST", "exchanges/"+id+"/remove", map[string]string{"ID": cy.id}, 200)
	cy.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	dee.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	ana.c.req("POST", "exchanges/"+id+"/draw", nil, 200)
	order := a.state.Exchanges[id].Order
	if len(order) != 4 {
		t.Fatalf("order = %v", order)
	}
	// After it, nobody can leave or be removed, and a person who deletes their account keeps their place.
	ben.c.req("POST", "exchanges/"+id+"/leave", nil, 400)
	ana.c.req("POST", "exchanges/"+id+"/remove", map[string]string{"ID": ben.id}, 400)
	ben.c.req("POST", "account/delete", map[string]string{"password": "correct horse battery staple"}, 200)
	got := ana.c.req("GET", "exchanges/"+id, nil, 200)
	if len(got["order"].([]any)) != 4 || len(got["members"].([]any)) != 4 {
		t.Fatalf("a deleted account changed the order: %v", got)
	}
	// A reminder for a wrapped gift names the limit and never says who anything is for, and stops once the gift is ready.
	before := len(a.state.Outbox)
	a.state.Exchanges[id].Date = future(1)
	a.remind(time.Now())
	if len(a.state.Outbox) <= before {
		t.Fatal("no reminder for a white elephant")
	}
	m := lastMail(t, a, "ana@example.com")
	if !strings.Contains(m.Body, "wrapped gift") || !strings.Contains(m.Body, "£20") || strings.Contains(m.Body, "buying for") {
		t.Fatalf("reminder = %q", m.Body)
	}
	ana.c.req("POST", "exchanges/"+id+"/ready", map[string]bool{"Ready": true}, 200)
	n := len(a.state.Outbox)
	a.state.Exchanges[id].Reminded = nil
	a.remind(time.Now())
	for _, mail := range a.state.Outbox[n:] {
		if mail.To == "ana@example.com" {
			t.Fatal("a reminder was sent to someone whose gift is ready")
		}
	}
}

func TestWhiteElephant(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy", "Dee")
	ana := p["Ana"]
	for _, bad := range []string{"-1", "11", "x", "2.5"} {
		ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Kind": "elephant", "Steals": bad}, 400)
	}
	ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Kind": "nonsense"}, 400)
	e := ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Kind": "elephant"}, 200)
	if e["kind"] != "elephant" || e["steals"].(float64) != 3 || e["order"] != nil {
		t.Fatalf("new elephant = %v", e)
	}
	id := e["id"].(string)
	for _, n := range []string{"Ben", "Cy"} {
		p[n].c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	// Details can change until entries close, but not the kind.
	ana.c.req("POST", "exchanges/"+id+"/edit", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Steals": "2", "Kind": "group"}, 200)
	if got := ana.c.req("GET", "exchanges/"+id, nil, 200); got["kind"] != "elephant" || got["steals"].(float64) != 2 {
		t.Fatalf("after edit = %v", got)
	}
	ana.c.req("POST", "exchanges/"+id+"/apart", map[string]any{"Pairs": [][2]string{}}, 400)
	ana.c.req("POST", "exchanges/"+id+"/message", map[string]string{"To": "recipient", "Text": "hi"}, 400)
	ana.c.req("POST", "exchanges/"+id+"/ready", map[string]bool{"Ready": true}, 400) // not before entries close
	ana.c.req("POST", "exchanges/"+id+"/draw", nil, 200)
	for name, x := range p {
		if name == "Dee" {
			continue
		}
		got := x.c.req("GET", "exchanges/"+id, nil, 200)
		order := got["order"].([]any)
		if got["drawn"] != true || len(order) != 3 || got["recipient"] != nil || got["toRecipient"] != nil {
			t.Fatalf("%s sees %v", name, got)
		}
		seen := map[any]bool{}
		for _, m := range order {
			seen[m] = true
		}
		if len(seen) != 3 || !seen[p["Ana"].id] || !seen[p["Ben"].id] || !seen[p["Cy"].id] {
			t.Fatalf("order %v is not everyone once", order)
		}
	}
	if len(a.state.Exchanges[id].Assignments) != 0 {
		t.Fatal("a white elephant assigned recipients")
	}
	p["Ben"].c.req("POST", "exchanges/"+id+"/ready", map[string]bool{"Ready": true}, 200)
	ana.c.req("POST", "exchanges/"+id+"/edit", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 400)
	p["Dee"].c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 404) // closed
	ana.c.req("POST", "exchanges/"+id+"/draw", nil, 400)
	ana.c.req("POST", "exchanges/"+id+"/reveal", nil, 400)
}

func TestWhiteElephantNeedsThree(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben")
	e := p["Ana"].c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Kind": "elephant"}, 200)
	p["Ben"].c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	p["Ana"].c.req("POST", "exchanges/"+e["id"].(string)+"/draw", nil, 400)
}

func TestPickOrderIsRandom(t *testing.T) {
	first := map[string]int{}
	for i := 0; i < 300; i++ {
		e := &exchange{Members: []string{"a", "b", "c"}}
		e.lockElephant()
		first[e.Order[0]]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if first[id] < 60 {
			t.Fatalf("%s goes first only %d times in 300: %v", id, first[id], first)
		}
	}
}

func TestGroupGift(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy", "Fay", "Stranger")
	ana, ben, cy, fay, stranger := p["Ana"], p["Ben"], p["Cy"], p["Fay"], p["Stranger"]
	for _, x := range []*person{ana, ben, cy} {
		makeFriends(t, x, fay)
	}
	form := func(forID string) map[string]string {
		return map[string]string{"Name": "Birthday", "Date": "2099-12-20", "Budget": "30", "Currency": "GBP", "Kind": "group", "For": forID}
	}
	stranger.c.req("POST", "exchanges", form(fay.id), 400) // not their friend
	ana.c.req("POST", "exchanges", form(""), 400)
	ana.c.req("POST", "exchanges", form("nobody"), 400)
	e := ana.c.req("POST", "exchanges", form(fay.id), 200)
	id := e["id"].(string)
	if e["kind"] != "group" || e["for"].(map[string]any)["id"] != fay.id || e["drawn"] != false || e["invite"] == nil {
		t.Fatalf("new group gift = %v", e)
	}
	idea(fay, "Pan", map[string]any{"friends": true})
	idea(fay, "Socks", map[string]any{})
	ben.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	stranger.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 400)
	// The person it is for never learns of it: they can't join, see it, list it or open it.
	fay.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 404)
	fay.c.req("GET", "exchanges/"+id, nil, 404)
	if body := fay.c.get("/api/exchanges", 200).Body.String(); strings.Contains(body, id) || strings.Contains(body, "Birthday") {
		t.Fatalf("the person it is for sees it listed: %s", body)
	}
	fay.c.get("/calendar/"+id, 404)
	got := ben.c.req("GET", "exchanges/"+id, nil, 200)
	f := got["for"].(map[string]any)
	if titles := wishTitles(f); len(titles) != 1 || titles["Pan"] != "" {
		t.Fatalf("members see %v", titles)
	}
	// Members mark what they're getting from the friend's page, and others in the group see it.
	pan := ""
	for _, w := range f["wishes"].([]any) {
		pan = w.(map[string]any)["id"].(string)
	}
	ben.c.req("POST", "friends/"+fay.id+"/claim", map[string]any{"Wish": pan, "Claim": true}, 200)
	cy.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	if titles := wishTitles(cy.c.req("GET", "exchanges/"+id, nil, 200)["for"].(map[string]any)); titles["Pan"] != "other" {
		t.Fatalf("another member sees %v", titles)
	}
	// None of it reaches Fay.
	for _, path := range []string{"me", "friends", "exchanges"} {
		body := fay.c.get("/api/"+path, 200).Body.String()
		for _, leak := range []string{"claimedBy", "claimedIn", "claimedAt", `"claim"`} {
			if strings.Contains(body, leak) {
				t.Fatalf("GET %s leaks %q to the person the gift is for", path, leak)
			}
		}
	}
	ana.c.req("POST", "exchanges/"+id+"/draw", nil, 400)
	ana.c.req("POST", "exchanges/"+id+"/message", map[string]string{"To": "recipient", "Text": "hi"}, 400)
	ana.c.req("POST", "exchanges/"+id+"/claim", map[string]any{"Wish": pan, "Claim": true}, 400)
	// Losing the friendship hides the list and releases the claim, but not the exchange.
	ben.c.req("POST", "friends/remove", map[string]string{"ID": fay.id}, 200)
	got = ben.c.req("GET", "exchanges/"+id, nil, 200)
	if got["for"].(map[string]any)["friends"] != false || got["for"].(map[string]any)["wishes"] != nil {
		t.Fatalf("an ex-friend still sees the list: %v", got["for"])
	}
	// Deleting the account the gift is for removes the gift.
	fay.c.req("POST", "account/delete", map[string]string{"password": "correct horse battery staple"}, 200)
	ben.c.req("GET", "exchanges/"+id, nil, 404)
}

func TestGroupGiftReminders(t *testing.T) {
	a, _ := mailSetup(t)
	p := people(t, a, "Ana", "Ben", "Fay")
	for _, n := range []string{"Ana", "Ben", "Fay"} {
		a.state.Users[p[n].id].Verified = true
	}
	makeFriends(t, p["Ana"], p["Fay"])
	makeFriends(t, p["Ben"], p["Fay"])
	e := p["Ana"].c.req("POST", "exchanges", map[string]string{"Name": "Birthday", "Date": future(3), "Budget": "30", "Currency": "GBP", "Kind": "group", "For": p["Fay"].id}, 200)
	p["Ben"].c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	before := len(a.state.Outbox)
	a.remind(time.Now()) // the week-before reminder is already late, so it is skipped rather than sent
	if len(a.state.Outbox) != before {
		t.Fatal("a late reminder was sent for a new group gift")
	}
	a.state.Exchanges[e["id"].(string)].Date = future(1)
	a.remind(time.Now())
	if len(a.state.Outbox) != before+2 {
		t.Fatalf("expected a reminder for each member, got %d new", len(a.state.Outbox)-before)
	}
	for _, m := range a.state.Outbox[before:] {
		if m.To == "fay@example.com" || strings.Contains(m.Body, "Fay") || strings.Contains(m.Subject, "Fay") {
			t.Fatalf("reminder reaches or names the person the gift is for: %q %q", m.To, m.Body)
		}
	}
}

func TestWhiteElephantEmails(t *testing.T) {
	a, _ := mailSetup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	for _, n := range []string{"Ana", "Ben", "Cy"} {
		a.state.Users[p[n].id].Verified = true
	}
	e := p["Ana"].c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": future(30), "Budget": "20", "Currency": "GBP", "Kind": "elephant"}, 200)
	for _, n := range []string{"Ben", "Cy"} {
		p[n].c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	p["Ana"].c.req("POST", "exchanges/"+e["id"].(string)+"/draw", nil, 200)
	m := lastMail(t, a, "ben@example.com")
	if !strings.Contains(m.Subject, "Draw a number") || strings.Contains(m.Body, "buying for") {
		t.Fatalf("draw mail = %q %q", m.Subject, m.Body)
	}
}
