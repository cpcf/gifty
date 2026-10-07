package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type person struct {
	c  *client
	id string
}

func people(t *testing.T, a *App, names ...string) map[string]*person {
	t.Helper()
	out := map[string]*person{}
	for _, n := range names {
		c := &client{t: t, a: a}
		out[n] = &person{c, c.signup(n)}
	}
	return out
}

// makeFriends has x open y's friend link and y accept.
func makeFriends(t *testing.T, x, y *person) {
	t.Helper()
	code := y.c.req("GET", "friends", nil, 200)["code"].(string)
	x.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	y.c.req("POST", "friends/accept", map[string]string{"ID": x.id}, 200)
}

func idea(p *person, title string, extra map[string]any) string {
	body := map[string]any{"title": title, "exchanges": []string{}}
	for k, v := range extra {
		body[k] = v
	}
	p.c.req("POST", "wishes", body, 200)
	for _, w := range p.c.req("GET", "me", nil, 200)["wishes"].([]any) {
		if w.(map[string]any)["title"] == title {
			return w.(map[string]any)["id"].(string)
		}
	}
	p.c.t.Fatalf("idea %q not saved", title)
	return ""
}

func wishTitles(r map[string]any) map[string]string {
	out := map[string]string{}
	for _, w := range r["wishes"].([]any) {
		m := w.(map[string]any)
		claim, _ := m["claim"].(string)
		out[m["title"].(string)] = claim
	}
	return out
}

func TestFriendRequests(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben := p["Ana"], p["Ben"]
	code := ana.c.req("GET", "friends", nil, 200)["code"].(string)
	if code == "" {
		t.Fatal("a new account has no friend link")
	}
	ana.c.req("POST", "friends/request", map[string]string{"Code": code}, 400) // your own link
	ben.c.req("POST", "friends/request", map[string]string{"Code": "nonsense"}, 404)
	if r := ben.c.req("GET", "friend/"+code, nil, 200); r["name"] != "Ana" || r["own"] != false {
		t.Fatalf("preview = %v", r)
	}
	// The link says who sent it to anyone who holds it, signed in or not, and nothing else.
	out := &client{t: t, a: a}
	if r := out.req("GET", "friend-link/"+code, nil, 200); r["name"] != "Ana" || len(r) != 1 {
		t.Fatalf("public preview = %v", r)
	}
	out.req("GET", "friend-link/nonsense", nil, 404)
	ben.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	ben.c.req("POST", "friends/request", map[string]string{"Code": code}, 200) // asking twice changes nothing
	if r := ana.c.req("GET", "friends", nil, 200); len(r["requests"].([]any)) != 1 || len(r["friends"].([]any)) != 0 {
		t.Fatalf("ana sees %v", r)
	}
	if r := ben.c.req("GET", "friends", nil, 200); len(r["sent"].([]any)) != 1 || len(r["friends"].([]any)) != 0 {
		t.Fatalf("ben sees %v", r)
	}
	// Nothing is shared until she accepts: a request doesn't open a list.
	ben.c.req("GET", "friends/"+ana.id, nil, 404)
	// Only the person asked can accept.
	ben.c.req("POST", "friends/accept", map[string]string{"ID": ana.id}, 404)
	ana.c.req("POST", "friends/accept", map[string]string{"ID": ben.id}, 200)
	ben.c.req("GET", "friends/"+ana.id, nil, 200)
	ana.c.req("GET", "friends/"+ben.id, nil, 200)
	p["Cy"].c.req("GET", "friends/"+ana.id, nil, 404)

	// Both asking settles it without anyone accepting.
	cy := p["Cy"]
	cyCode := cy.c.req("GET", "friends", nil, 200)["code"].(string)
	ana.c.req("POST", "friends/request", map[string]string{"Code": cyCode}, 200)
	cy.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	ana.c.req("GET", "friends/"+cy.id, nil, 200)

	// Declining and cancelling leave no trace, and replacing a link stops the old one.
	dee := people(t, a, "Dee")["Dee"]
	dee.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	ana.c.req("POST", "friends/decline", map[string]string{"ID": dee.id}, 200)
	if r := dee.c.req("GET", "friends", nil, 200); len(r["sent"].([]any)) != 0 {
		t.Fatal("declined request still shows as sent")
	}
	ana.c.req("POST", "friends/rotate", nil, 200)
	people(t, a, "Eve")["Eve"].c.req("POST", "friends/request", map[string]string{"Code": code}, 404)

	// Ending it works from either side.
	ben.c.req("POST", "friends/remove", map[string]string{"ID": ana.id}, 200)
	ana.c.req("GET", "friends/"+ben.id, nil, 404)
	ben.c.req("GET", "friends/"+ana.id, nil, 404)
}

func TestFriendRequestsAreCapped(t *testing.T) {
	a := setup(t)
	ana := people(t, a, "Ana")["Ana"]
	for i := 0; i < maxRequests; i++ {
		o := people(t, a, "P"+string(rune('a'+i)))["P"+string(rune('a'+i))]
		code := o.c.req("GET", "friends", nil, 200)["code"].(string)
		ana.c.req("POST", "friends/request", map[string]string{"Code": code}, 200)
	}
	last := people(t, a, "Last")["Last"]
	ana.c.req("POST", "friends/request", map[string]string{"Code": last.c.req("GET", "friends", nil, 200)["code"].(string)}, 400)
}

func TestBirthdays(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben")
	ana, ben := p["Ana"], p["Ben"]
	makeFriends(t, ben, ana)
	for _, bad := range []map[string]any{{"Month": 13, "Day": 1}, {"Month": 2, "Day": 30}, {"Month": 4, "Day": 31}, {"Month": 5, "Day": 5, "Year": 1800}, {"Month": 5, "Day": 5, "Year": 3000}, {"Month": 5, "Day": 0}} {
		ana.c.req("POST", "birthday", bad, 400)
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 2, "Day": 29, "Show": true}, 200) // a leap day is a real birthday
	card := func() map[string]any {
		for _, f := range ben.c.req("GET", "friends", nil, 200)["friends"].([]any) {
			return f.(map[string]any)
		}
		return nil
	}
	if b := card()["birthday"].(map[string]any); b["month"].(float64) != 2 || b["day"].(float64) != 29 || b["year"] != nil {
		t.Fatalf("friend sees %v", b)
	}
	// Hidden means saved but not shown, anywhere a friend can look.
	ana.c.req("POST", "birthday", map[string]any{"Month": 2, "Day": 29, "Year": 1990, "Show": false}, 200)
	if card()["birthday"] != nil || ben.c.req("GET", "friends/"+ana.id, nil, 200)["birthday"] != nil {
		t.Fatal("a hidden birthday is visible to a friend")
	}
	if me := ana.c.req("GET", "me", nil, 200); me["birthday"] == nil || me["birthdayHidden"] != true {
		t.Fatalf("owner lost their own birthday: %v", me)
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 0, "Day": 0}, 200)
	if ana.c.req("GET", "me", nil, 200)["birthday"] != nil {
		t.Fatal("birthday not cleared")
	}
}

func TestBirthdayDates(t *testing.T) {
	leap := &Birthday{2, 29, 0}
	for year, want := range map[int]string{2028: "2028-02-29", 2027: "2027-02-28", 2100: "2100-02-28"} {
		if got := leap.on(year).Format("2006-01-02"); got != want {
			t.Errorf("29 Feb in %d = %s, want %s", year, got, want)
		}
	}
	b := &Birthday{10, 25, 1990}
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	if got := b.next(now).Format("2006-01-02"); got != "2026-10-25" {
		t.Errorf("next = %s", got)
	}
	if got := b.next(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)).Format("2006-01-02"); got != "2027-10-25" {
		t.Errorf("next after = %s", got)
	}
	if got := b.next(time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)).Format("2006-01-02"); got != "2026-10-25" {
		t.Errorf("next on the day = %s", got)
	}
	if got := b.last(now).Format("2006-01-02"); got != "2025-10-25" {
		t.Errorf("last = %s", got)
	}
	for _, c := range []struct {
		on   time.Time
		want bool
	}{{time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), false}, {time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), true}, {time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), true}, {time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), false}} {
		if got := b.recent(c.on); got != c.want {
			t.Errorf("recent on %s = %v, want %v", c.on.Format("2006-01-02"), got, c.want)
		}
	}
	var none *Birthday
	if none.recent(now) {
		t.Fatal("no birthday can't be recent")
	}
}

func TestFriendsSeeOnlyIdeasShownToFriends(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	idea(ana, "Pan", map[string]any{"friends": true})
	idea(ana, "Socks", map[string]any{})                                   // exchanges only, as every idea was before
	idea(ana, "Hat", map[string]any{"friends": true, "noExchanges": true}) // friends only
	got := wishTitles(ben.c.req("GET", "friends/"+ana.id, nil, 200))
	if len(got) != 2 || got["Pan"] != "" || got["Hat"] != "" {
		t.Fatalf("friend sees %v", got)
	}
	if card := ben.c.req("GET", "friends", nil, 200)["friends"].([]any)[0].(map[string]any); card["ideas"].(float64) != 2 {
		t.Fatalf("idea count = %v", card["ideas"])
	}
	cy.c.req("GET", "friends/"+ana.id, nil, 404)
	// The owner's own page still shows how each idea is shared.
	mine := map[string]map[string]any{}
	for _, w := range ana.c.req("GET", "me", nil, 200)["wishes"].([]any) {
		mine[w.(map[string]any)["title"].(string)] = w.(map[string]any)
	}
	if mine["Hat"]["friends"] != true || mine["Hat"]["noExchanges"] != true || mine["Socks"]["friends"] != nil {
		t.Fatalf("owner sees %v", mine)
	}
}

func TestFriendClaimsAreHiddenFromTheOwner(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy", "Dee")
	ana, ben, cy, dee := p["Ana"], p["Ben"], p["Cy"], p["Dee"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	id := idea(ana, "Pan", map[string]any{"friends": true})
	own := idea(ana, "Socks", map[string]any{})
	claim := func(x *person, wish string, on bool, status int) {
		x.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": wish, "Claim": on}, status)
	}
	claim(ben, own, true, 404) // an idea not shown to friends can't be marked by them
	claim(ben, id, true, 200)
	claim(ben, id, true, 200) // marking your own again changes nothing
	claim(cy, id, true, 409)
	if got := wishTitles(ben.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; got != "mine" {
		t.Fatalf("claimer sees %q", got)
	}
	if got := wishTitles(cy.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; got != "other" {
		t.Fatalf("other friend sees %q", got)
	}
	// Nothing the owner can fetch says who, or whether anyone, has an idea.
	for _, path := range []string{"me", "friends"} {
		body := ana.c.get("/api/"+path, 200).Body.String()
		for _, leak := range []string{"claimedBy", "claimedIn", "claimedAt", `"claim"`} {
			if strings.Contains(body, leak) {
				t.Fatalf("GET %s leaks %q: %s", path, leak, body)
			}
		}
	}
	// Whoever is buying for Ana in an exchange sees that someone has it, too.
	e := ana.c.req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	for _, x := range []*person{ben, cy, dee} {
		x.c.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	scarf := idea(ana, "Scarf", map[string]any{"friends": true})
	a.state.Exchanges[e["id"].(string)].Assignments = map[string]string{dee.id: ana.id, ana.id: ben.id, ben.id: cy.id, cy.id: dee.id}
	got := wishTitles(dee.c.req("GET", "exchanges/"+e["id"].(string), nil, 200)["recipient"].(map[string]any))
	if got["Pan"] != "other" || got["Scarf"] != "" {
		t.Fatalf("giver sees %v", got)
	}
	// ...and the other way: a giver's claim is seen by friends.
	dee.c.req("POST", "exchanges/"+e["id"].(string)+"/claim", map[string]any{"Wish": scarf, "Claim": true}, 200)
	makeFriends(t, dee, ana)
	if got := wishTitles(dee.c.req("GET", "friends/"+ana.id, nil, 200))["Scarf"]; got != "mine" {
		t.Fatalf("giver who is also a friend sees %q", got)
	}
	if got := wishTitles(ben.c.req("GET", "friends/"+ana.id, nil, 200))["Scarf"]; got != "other" {
		t.Fatalf("friend sees a giver's claim as %q", got)
	}
	// Letting go works, and only for the person who holds it.
	claim(cy, id, false, 200)
	if got := wishTitles(ben.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; got != "mine" {
		t.Fatal("someone else could release a claim")
	}
	claim(ben, id, false, 200)
	if got := wishTitles(cy.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; got != "" {
		t.Fatalf("released idea shows %q", got)
	}
}

func TestClaimsEndWhenTheyShouldNot(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	pan := idea(ana, "Pan", map[string]any{"friends": true})
	mark := func(x *person) {
		x.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": pan, "Claim": true}, 200)
	}
	state := func(x *person) string {
		return wishTitles(x.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]
	}
	// Sorting hides the idea from everyone but the person who had it, who is told to check.
	mark(ben)
	ana.c.req("POST", "wishes/"+pan+"/status", map[string]string{"Status": "got"}, 200)
	cy.c.req("GET", "friends/"+ana.id, nil, 200)
	if _, ok := wishTitles(cy.c.req("GET", "friends/"+ana.id, nil, 200))["Pan"]; ok {
		t.Fatal("a sorted idea is still shown to other friends")
	}
	if state(ben) != "mine" {
		t.Fatal("the claimer lost sight of a sorted idea")
	}
	ana.c.req("POST", "wishes/"+pan+"/status", map[string]string{"Status": ""}, 200)
	// Taking the idea off friends' view ends the claim.
	idea := func(friends bool) {
		ana.c.req("POST", "wishes", map[string]any{"id": pan, "title": "Pan", "exchanges": []string{}, "friends": friends}, 200)
	}
	idea(false)
	idea(true)
	if state(cy) != "" {
		t.Fatal("a claim survived the idea being hidden from friends")
	}
	// Ending a friendship ends claims, in both directions.
	mark(ben)
	ben.c.req("POST", "friends/remove", map[string]string{"ID": ana.id}, 200)
	makeFriends(t, ben, ana)
	if state(ben) != "" {
		t.Fatal("a claim survived the friendship")
	}
	// Deleting an account releases its claims and removes it from every friends list.
	mark(ben)
	ben.c.req("POST", "account/delete", map[string]string{"password": "correct horse battery staple"}, 200)
	if state(cy) != "" {
		t.Fatal("a deleted account still holds a claim")
	}
	if len(ana.c.req("GET", "friends", nil, 200)["friends"].([]any)) != 1 {
		t.Fatal("a deleted account is still a friend")
	}
}

func TestClaimsLapseAfterTheBirthday(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben")
	ana, ben := p["Ana"], p["Ben"]
	makeFriends(t, ben, ana)
	pan := idea(ana, "Pan", map[string]any{"friends": true})
	ben.c.req("POST", "friends/"+ana.id+"/claim", map[string]any{"Wish": pan, "Claim": true}, 200)
	now := time.Now().UTC()
	// Birthday two days ago: still held. Four days ago: let go.
	for days, want := range map[int]bool{2: true, 4: false} {
		a.state.Users[ana.id].Birthday = &Birthday{int(now.AddDate(0, 0, -days).Month()), now.AddDate(0, 0, -days).Day(), 0}
		w := &a.state.Users[ana.id].Wishes[0]
		w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = ben.id, friendClaim, now.AddDate(0, 0, -30)
		a.birthdays(now)
		if held := w.ClaimedBy != ""; held != want {
			t.Fatalf("birthday %d days ago: held = %v", days, held)
		}
	}
	// A claim made after the birthday lapsed is for next year and is kept.
	w := &a.state.Users[ana.id].Wishes[0]
	w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = ben.id, friendClaim, now
	a.birthdays(now)
	if w.ClaimedBy == "" {
		t.Fatal("a fresh claim was released")
	}
	// Claims made as someone's giver are left to the exchange.
	w.ClaimedIn, w.ClaimedAt = "some-exchange", now.AddDate(0, 0, -30)
	a.birthdays(now)
	if w.ClaimedBy == "" {
		t.Fatal("an exchange claim was released by a birthday")
	}
}

func TestFriendPhotos(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	shown := idea(ana, "Pan", map[string]any{"friends": true, "photos": []string{png}})
	hidden := idea(ana, "Socks", map[string]any{"photos": []string{png}})
	status := func(x *person, id string) int {
		r := httptest.NewRequest("GET", "/image/"+id+"/0", nil)
		r.AddCookie(x.c.cookie)
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		return w.Code
	}
	if status(ben, shown) != 200 || status(ben, hidden) != 404 || status(cy, shown) != 404 || status(ana, hidden) != 200 {
		t.Fatal("photos aren't limited to the audience chosen for the idea")
	}
}

func TestBirthdayCalendarFile(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	ana.c.req("POST", "birthday", map[string]any{"Month": 2, "Day": 29, "Show": true}, 200)
	cy.c.req("POST", "birthday", map[string]any{"Month": 7, "Day": 4, "Show": false}, 200)
	body := ben.c.get("/calendar/birthdays", 200).Body.String()
	for _, want := range []string{"BEGIN:VCALENDAR", "SUMMARY:Ana’s birthday", "RRULE:FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=-1", "UID:birthday-" + ana.id + "@gifty"} {
		if !strings.Contains(body, want) {
			t.Errorf("calendar lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Cy") {
		t.Fatal("a hidden birthday is in a friend's calendar file")
	}
	r := httptest.NewRequest("GET", "/calendar/birthdays", nil)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("signed out calendar = %d", w.Code)
	}
}

func TestBirthdayEmails(t *testing.T) {
	a, _ := mailSetup(t)
	p := people(t, a, "Ana", "Ben")
	ana, ben := p["Ana"], p["Ben"]
	makeFriends(t, ben, ana)
	for _, id := range []string{ana.id, ben.id} {
		a.state.Users[id].Verified = true
	}
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	ana.c.req("POST", "birthday", map[string]any{"Month": 10, "Day": 15, "Show": true}, 200)
	count := func() int {
		n := 0
		for _, m := range a.state.Outbox {
			if m.To == "ben@example.com" && strings.Contains(m.Subject, "birthday") {
				n++
			}
		}
		return n
	}
	a.birthdays(now.AddDate(0, 0, -1)) // six days before: nothing yet
	if count() != 0 {
		t.Fatal("emailed before the week")
	}
	a.birthdays(now)
	a.birthdays(now.Add(time.Hour)) // a second pass the same day sends nothing more
	if count() != 1 {
		t.Fatalf("week-before emails = %d", count())
	}
	m := lastMail(t, a, "ben@example.com")
	if !strings.Contains(m.Subject, "Ana’s birthday is in a week") || !strings.Contains(m.Body, "Thursday 15 October") || !strings.Contains(m.Body, "/#friends/"+ana.id) {
		t.Fatalf("mail = %q %q", m.Subject, m.Body)
	}
	// An idea marked by a friend must not be hinted at in anything sent to anyone.
	if body := strings.ToLower(m.Body); strings.Contains(body, "marked") || strings.Contains(body, "claim") || strings.Contains(body, "someone") {
		t.Fatalf("birthday email talks about gifts: %q", m.Body)
	}
	a.birthdays(now.AddDate(0, 0, 7))
	if count() != 2 || !strings.Contains(lastMail(t, a, "ben@example.com").Subject, "today") {
		t.Fatal("no email on the day")
	}
	ben.c.req("POST", "account/birthday-mail", map[string]bool{"Notify": false}, 200)
	a.birthdays(now.AddDate(0, 0, 7+365))
	if count() != 2 {
		t.Fatal("emailed after opting out")
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 10, "Day": 15, "Show": false}, 200)
	ben.c.req("POST", "account/birthday-mail", map[string]bool{"Notify": true}, 200)
	a.birthdays(now.AddDate(1, 0, 0))
	if count() != 2 {
		t.Fatal("a hidden birthday was emailed")
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
		e := &Exchange{Members: []string{"a", "b", "c"}}
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

func TestExistingIdeasStayWhereTheyWere(t *testing.T) {
	// An idea saved before friends existed has neither flag, so it is for givers only.
	w := Wish{ID: "x"}
	if !w.forExchange("any") || w.Friends {
		t.Fatal("an old idea changed audience")
	}
	w.NoExchanges = true
	if w.forExchange("any") {
		t.Fatal("friends-only idea is shown to givers")
	}
	w = Wish{Exchanges: []string{"a"}}
	if !w.forExchange("a") || w.forExchange("b") {
		t.Fatal("exchange scope broke")
	}
}
