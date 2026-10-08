package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// raw sends a body exactly as given, for the requests req can't make: broken JSON, unknown fields, other methods.
func raw(c *client, method, path, contentType, body string, status int) {
	c.t.Helper()
	r := httptest.NewRequest(method, "/api/"+path, bytes.NewBufferString(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.a.handler().ServeHTTP(w, r)
	if w.Code != status {
		c.t.Fatalf("%s %s %q: got %d want %d: %s", method, path, body, w.Code, status, w.Body.String())
	}
}

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

func TestFriendLimits(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	anaCode := ana.c.req("GET", "friends", nil, 200)["code"].(string)
	// Someone with a full friends list can't take another, however it is asked.
	for i := 0; i < maxFriends; i++ {
		a.state.Users[ana.id].Friends = append(a.state.Users[ana.id].Friends, "filler")
	}
	ben.c.req("POST", "friends/request", map[string]string{"Code": anaCode}, 400)
	a.state.Users[ana.id].Friends = nil
	ben.c.req("POST", "friends/request", map[string]string{"Code": anaCode}, 200)
	for i := 0; i < maxFriends; i++ {
		a.state.Users[ana.id].Friends = append(a.state.Users[ana.id].Friends, "filler")
	}
	ana.c.req("POST", "friends/accept", map[string]string{"ID": ben.id}, 400)
	a.state.Users[ana.id].Friends = nil
	ana.c.req("POST", "friends/accept", map[string]string{"ID": ben.id}, 200)
	// Too many people waiting on one person is refused, so a leaked link can't bury someone in requests.
	for i := 0; i < 2*maxRequests; i++ {
		a.state.Users[ana.id].FriendsIn = append(a.state.Users[ana.id].FriendsIn, "filler")
	}
	cy.c.req("POST", "friends/request", map[string]string{"Code": anaCode}, 400)
}

func TestClaimingASortedIdeaYouHold(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	pan := idea(ana, "Pan", map[string]any{"friends": true})
	claim := func(x *person, on bool, status int) {
		x.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": pan, "Claim": on}, status)
	}
	claim(ben, true, 200)
	ana.c.req("POST", "wishes/"+pan+"/status", map[string]string{"Status": "got"}, 200)
	claim(ben, true, 400)  // the holder is told it is sorted, not allowed to take it again
	claim(cy, true, 404)   // everyone else no longer sees it
	claim(ben, false, 200) // but letting go always works
	claim(ben, true, 404)
}

func TestBirthdayCalendarFileWithoutSomeBirthdays(t *testing.T) {
	a := setup(t)
	a.base = "https://gifty.example"
	p := people(t, a, "Ana", "Ben", "Cy", "Dee")
	ana, ben, cy, dee := p["Ana"], p["Ben"], p["Cy"], p["Dee"]
	for _, f := range []*person{ana, cy, dee} {
		makeFriends(t, ben, f)
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 3, "Day": 14, "Show": true}, 200)
	cy.c.req("POST", "birthday", map[string]any{"Month": 7, "Day": 4, "Show": false}, 200) // hidden
	// Dee has no birthday at all.
	body := strings.ReplaceAll(ben.c.get("/calendar/birthdays", 200).Body.String(), "\r\n ", "") // lines are folded at 75 octets
	if n := strings.Count(body, "BEGIN:VEVENT"); n != 1 {
		t.Fatalf("calendar file has %d events, want 1:\n%s", n, body)
	}
	for _, want := range []string{"URL:https://gifty.example/#friends/" + ana.id, "TRANSP:TRANSPARENT", "RRULE:FREQ=YEARLY\r\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("calendar file lacks %q", want)
		}
	}
	if strings.Contains(body, "BYMONTH") {
		t.Fatal("an ordinary date got the leap-day rule")
	}
	if !strings.HasSuffix(body, "END:VCALENDAR\r\n") {
		t.Fatal("calendar file isn't terminated")
	}
	w := ben.c.get("/calendar/birthdays", 200)
	if w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "gifty-birthdays.ics") {
		t.Fatalf("headers = %v", w.Header())
	}
	// Someone with no friends gets an empty but valid file.
	if empty := people(t, a, "Eve")["Eve"].c.get("/calendar/birthdays", 200).Body.String(); strings.Contains(empty, "VEVENT") || !strings.Contains(empty, "BEGIN:VCALENDAR") {
		t.Fatalf("empty calendar = %q", empty)
	}
}

func TestIdeasForFriendsOnlyStayFromGivers(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	e := ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	id := e["id"].(string)
	for _, x := range []*person{ben, cy} {
		x.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	other := people(t, a, "Dee")["Dee"]
	other.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	a.state.Exchanges[id].Assignments = map[string]string{ben.id: ana.id, ana.id: cy.id, cy.id: other.id, other.id: ben.id}
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	pan := idea(ana, "Pan", map[string]any{"friends": true, "noExchanges": true, "photos": []string{png}})
	hat := idea(ana, "Hat", map[string]any{"photos": []string{png}})
	titles := func(r map[string]any) map[string]string { return wishTitles(r["recipient"].(map[string]any)) }
	// Ben is a friend and the giver; he sees both, and the giver Cy... is buying for Dee, not Ana. Ben buys for Ana.
	got := titles(ben.c.req("GET", "exchanges/"+id, nil, 200))
	if _, ok := got["Pan"]; ok || len(got) != 1 {
		t.Fatalf("a giver sees a friends-only idea: %v", got)
	}
	status := func(x *person, wish string) int {
		r := httptest.NewRequest("GET", "/image/"+wish+"/0", nil)
		r.AddCookie(x.c.cookie)
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		return w.Code
	}
	// Ben is both Ana's giver and her friend: the friends-only photo comes through the friendship, the givers' one through the draw.
	if status(ben, pan) != 200 || status(ben, hat) != 200 {
		t.Fatal("a friend who is also the giver can't see photos he may see")
	}
	if status(cy, pan) != 404 || status(cy, hat) != 404 {
		t.Fatal("a member who is neither friend nor giver can see a photo")
	}
	// Ending the friendship takes the friends-only photo with it, but not the giver's access to the other.
	ben.c.req("POST", "friends/remove", map[string]string{"ID": ana.id}, 200)
	if status(ben, pan) != 404 || status(ben, hat) != 200 {
		t.Fatal("photo access didn't follow the audience")
	}
	// A claim made as a giver ends if the idea is then kept from givers.
	ben.c.req("POST", "exchanges/"+id+"/claim", map[string]any{"Wish": hat, "Claim": true}, 200)
	ana.c.req("POST", "wishes", map[string]any{"id": hat, "title": "Hat", "exchanges": []string{}, "friends": true, "noExchanges": true}, 200)
	if _, ok := titles(ben.c.req("GET", "exchanges/"+id, nil, 200))["Hat"]; ok {
		t.Fatal("a giver still sees an idea now meant for friends only")
	}
	if a.state.Users[ana.id].Wishes[1].ClaimedBy != "" {
		t.Fatal("a giver's claim survived the idea being kept from givers")
	}
	// ...but a friend's claim survives the idea changing which exchanges see it.
	makeFriends(t, ben, ana)
	ben.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": hat, "Claim": true}, 200)
	ana.c.req("POST", "wishes", map[string]any{"id": hat, "title": "Hat", "exchanges": []string{id}, "friends": true}, 200)
	if a.state.Users[ana.id].Wishes[1].ClaimedBy != ben.id {
		t.Fatal("a friend's claim was dropped by an exchange scope change")
	}
}

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

func TestIdeaLimitsAndEdits(t *testing.T) {
	a := setup(t)
	ana := people(t, a, "Ana")["Ana"]
	ana.c.req("POST", "wishes", map[string]any{"id": "missing", "title": "x", "exchanges": []string{}}, 404)
	ana.c.req("POST", "wishes/missing", nil, 404)
	ana.c.req("POST", "wishes", map[string]any{"title": "  ", "exchanges": []string{}}, 400)
	ana.c.req("POST", "wishes", map[string]any{"title": strings.Repeat("x", 121), "exchanges": []string{}}, 400)
	ana.c.req("POST", "wishes", map[string]any{"title": "x", "url": "javascript:alert(1)", "exchanges": []string{}}, 400)
	ana.c.req("POST", "wishes", map[string]any{"title": "x", "exchanges": []string{"not-an-exchange"}}, 400)
	ana.c.req("POST", "wishes/missing/status", map[string]string{"Status": "got"}, 404)
	id := idea(ana, "Pan", map[string]any{"friends": true})
	ana.c.req("POST", "wishes/"+id+"/status", map[string]string{"Status": "nonsense"}, 400)
	// Editing keeps what wasn't sent: photos stay when the form doesn't mention them.
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	ana.c.req("POST", "wishes", map[string]any{"id": id, "title": "Pan", "exchanges": []string{}, "friends": true, "photos": []string{png}}, 200)
	ana.c.req("POST", "wishes", map[string]any{"id": id, "title": "Pan, 26cm", "exchanges": []string{}, "friends": true}, 200)
	if w := a.state.Users[ana.id].Wishes[0]; len(w.Photos) != 1 || w.Title != "Pan, 26cm" || !w.Friends {
		t.Fatalf("edit lost data: %+v", w)
	}
	ana.c.req("POST", "wishes", map[string]any{"id": id, "title": "Pan", "exchanges": []string{}, "friends": true, "photos": []string{}}, 200)
	if len(a.state.Users[ana.id].Wishes[0].Photos) != 0 {
		t.Fatal("photos weren't removed")
	}
	for i := 1; i < 100; i++ {
		idea(ana, "Idea "+strings.Repeat("x", i%5)+string(rune('A'+i%26))+string(rune('a'+i/26)), nil)
	}
	ana.c.req("POST", "wishes", map[string]any{"title": "one too many", "exchanges": []string{}}, 400)
	ana.c.req("POST", "wishes/"+id, nil, 200)
	ana.c.req("POST", "wishes", map[string]any{"title": "room again", "exchanges": []string{}}, 200)
}

func TestFriendsAndKindsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	a, err := openApp(path)
	if err != nil {
		t.Fatal(err)
	}
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	ana.c.req("POST", "birthday", map[string]any{"Month": 2, "Day": 29, "Year": 1992, "Show": false}, 200)
	pan := idea(ana, "Pan", map[string]any{"friends": true, "noExchanges": true})
	ben.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": pan, "Claim": true}, 200)
	ben.c.req("POST", "account/birthday-mail", map[string]bool{"Notify": false}, 200)
	g := ana.c.req("POST", "exchanges", map[string]string{"Name": "Gift", "Date": "2099-12-20", "Budget": "30", "Currency": "GBP", "Kind": "group", "For": cy.id}, 200)
	w := ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP", "Kind": "elephant", "Steals": "4"}, 200)
	for _, x := range []*person{ben, cy} {
		x.c.req("POST", "join", map[string]string{"Code": w["invite"].(string)}, 200)
	}
	ana.c.req("POST", "exchanges/"+w["id"].(string)+"/draw", nil, 200)
	order := a.state.Exchanges[w["id"].(string)].Order

	b, err := openApp(path) // a fresh process reading the same file
	if err != nil {
		t.Fatal(err)
	}
	b.state.Sessions = a.state.Sessions
	c := func(x *person) *client { return &client{t: t, a: b, cookie: x.c.cookie} }
	aa, bb := c(ana), c(ben)
	if me := aa.req("GET", "me", nil, 200); me["birthday"] == nil || me["birthdayHidden"] != true || me["birthdayMail"] != true {
		t.Fatalf("birthday after restart: %v", me)
	}
	if bb.req("GET", "me", nil, 200)["birthdayMail"] != false {
		t.Fatal("birthday email preference lost")
	}
	if got := wishTitles(bb.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; got != "mine" {
		t.Fatalf("claim after restart = %q", got)
	}
	if len(aa.req("GET", "friends", nil, 200)["friends"].([]any)) != 2 {
		t.Fatal("friends lost")
	}
	if e := aa.req("GET", "exchanges/"+g["id"].(string), nil, 200); e["kind"] != "group" || e["for"].(map[string]any)["id"] != cy.id {
		t.Fatalf("group gift after restart: %v", e)
	}
	e := aa.req("GET", "exchanges/"+w["id"].(string), nil, 200)
	if e["kind"] != "elephant" || e["steals"].(float64) != 4 || len(e["order"].([]any)) != 3 || e["order"].([]any)[0] != order[0] {
		t.Fatalf("white elephant after restart: %v", e)
	}
	// An older data file, from before any of this, still loads, and its people can use the new pages.
	old := filepath.Join(t.TempDir(), "old.json")
	legacy := `{"Users":{"u1":{"ID":"u1","Name":"Old","Email":"old@example.com","Wishes":[{"id":"w1","title":"Socks","url":"","note":"","price":"","exchanges":null}]}},"Exchanges":{"e1":{"ID":"e1","Name":"Old swap","Date":"2099-01-01","Budget":"10","Currency":"GBP","Owner":"u1","Invite":"x","Members":["u1"]}},"Sessions":{}}`
	if err := os.WriteFile(old, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := openApp(old)
	if err != nil {
		t.Fatal(err)
	}
	if x := o.state.Exchanges["e1"]; x.Kind != "" || x.locked() || len(x.Members) != 1 {
		t.Fatalf("legacy exchange = %+v", x)
	}
	o.state.Sessions[digest("legacy")] = Session{"u1", time.Now().Add(time.Hour)}
	lc := &client{t: t, a: o, cookie: &http.Cookie{Name: "gifty_session", Value: "legacy"}}
	if lc.req("GET", "friends", nil, 200)["code"] != "" {
		t.Fatal("an account from before friends already has a link")
	}
	if lc.req("POST", "friends/rotate", nil, 200)["code"] == "" {
		t.Fatal("an older account can't make a friend link")
	}
	if w := o.state.Users["u1"].Wishes[0]; w.Friends || !w.forExchange("e1") {
		t.Fatal("a legacy idea changed audience")
	}
}
