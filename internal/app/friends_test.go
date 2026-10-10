package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	o.state.Sessions[digest("legacy")] = session{"u1", time.Now().Add(time.Hour)}
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
	leap := &birthday{2, 29, 0}
	for year, want := range map[int]string{2028: "2028-02-29", 2027: "2027-02-28", 2100: "2100-02-28"} {
		if got := leap.on(year).Format("2006-01-02"); got != want {
			t.Errorf("29 Feb in %d = %s, want %s", year, got, want)
		}
	}
	b := &birthday{10, 25, 1990}
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
	var none *birthday
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
		a.state.Users[ana.id].Birthday = &birthday{int(now.AddDate(0, 0, -days).Month()), now.AddDate(0, 0, -days).Day(), 0}
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
