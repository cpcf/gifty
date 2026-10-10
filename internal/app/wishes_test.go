package app

import (
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestClaimingIdeas(t *testing.T) {
	// Bea is bought for by Alex in one exchange and by Cara in another.
	p := newParty(t, "Alex", "Bea", "Cara")
	alex, bea, cara := p.c["Alex"], p.c["Bea"], p.c["Cara"]
	p.c["Alex"].req("POST", p.path("draw"), map[string]string{}, 200)
	p.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Alex"})
	first := p.ex
	other := cara.req("POST", "exchanges", map[string]string{"Name": "Book club", "Date": "2099-11-20", "Budget": "10", "Currency": "GBP"}, 200)
	code := other["invite"].(string)
	bea.req("POST", "join", map[string]string{"Code": code}, 200)
	alex.req("POST", "join", map[string]string{"Code": code}, 200)
	second := other["id"].(string)
	cara.req("POST", "exchanges/"+second+"/draw", map[string]string{}, 200)
	p.a.state.Exchanges[second].Assignments = map[string]string{p.ids["Cara"]: p.ids["Bea"], p.ids["Bea"]: p.ids["Alex"], p.ids["Alex"]: p.ids["Cara"]}
	bea.req("POST", "wishes", map[string]string{"title": "Teapot"}, 200)
	bea.req("POST", "wishes", map[string]string{"title": "Scarf"}, 200)
	wishes := func(c *client, ex string) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, w := range c.req("GET", "exchanges/"+ex, nil, 200)["recipient"].(map[string]any)["wishes"].([]any) {
			out[w.(map[string]any)["title"].(string)] = w.(map[string]any)
		}
		return out
	}
	teapot := func() string { return wishes(alex, first)["Teapot"]["id"].(string) }
	if wishes(alex, first)["Teapot"]["claim"] != nil {
		t.Fatal("an unclaimed idea shows a claim")
	}
	alex.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": teapot(), "Claim": true}, 200)
	if wishes(alex, first)["Teapot"]["claim"] != "mine" || wishes(cara, second)["Teapot"]["claim"] != "other" {
		t.Fatal("claims are not shown to the right givers")
	}
	cara.req("POST", "exchanges/"+second+"/claim", map[string]any{"Wish": teapot(), "Claim": true}, 409)
	cara.req("POST", "exchanges/"+second+"/claim", map[string]any{"Wish": teapot(), "Claim": false}, 200) // not theirs to release
	if wishes(cara, second)["Teapot"]["claim"] != "other" {
		t.Fatal("someone else released the claim")
	}
	// The owner is told nothing.
	for _, v := range bea.req("GET", "me", nil, 200)["wishes"].([]any) {
		if _, ok := v.(map[string]any)["claim"]; ok {
			t.Fatal("the owner can see a claim")
		}
		if strings.Contains(fmt.Sprint(v), p.ids["Alex"]) {
			t.Fatal("the owner can see who claimed")
		}
	}
	// Ideas not shown in an exchange, and ideas of people you don't buy for, can't be claimed.
	alex.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": "missing", "Claim": true}, 404)
	cara.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": teapot(), "Claim": true}, 404)
	// Marking it sorted hides it from Cara but warns Alex, who had claimed it.
	id := teapot()
	bea.req("POST", "wishes/"+id+"/status", map[string]string{"Status": "got"}, 200)
	if _, ok := wishes(cara, second)["Teapot"]; ok {
		t.Fatal("a sorted idea is still shown to another giver")
	}
	if w := wishes(alex, first)["Teapot"]; w == nil || w["status"] != "got" || w["claim"] != "mine" {
		t.Fatalf("the claimer isn't warned: %v", w)
	}
	alex.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": id, "Claim": false}, 200)
	if _, ok := wishes(alex, first)["Teapot"]; ok {
		t.Fatal("a released sorted idea is still shown")
	}
	alex.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": id, "Claim": true}, 404)
	bea.req("POST", "wishes/"+id+"/status", map[string]string{"Status": ""}, 200)
	if wishes(alex, first)["Teapot"]["claim"] != nil {
		t.Fatal("claim came back")
	}
	// A claim goes with the account or exchange it came from.
	alex.req("POST", "exchanges/"+first+"/claim", map[string]any{"Wish": id, "Claim": true}, 200)
	cara.req("POST", "exchanges/"+second+"/delete", map[string]string{}, 400) // not archived
	p.a.removeExchange(first)
	if wishes(cara, second)["Teapot"]["claim"] != nil {
		t.Fatal("a claim outlived its exchange")
	}
	cara.req("POST", "exchanges/"+second+"/claim", map[string]any{"Wish": id, "Claim": true}, 200)
	cara.req("POST", "exchanges/"+second+"/archive", map[string]string{}, 200)
	if err := p.a.removeUser(p.a.state.Users[p.ids["Cara"]]); err != nil {
		t.Fatal(err)
	}
	for _, w := range p.a.state.Users[p.ids["Bea"]].Wishes {
		if w.ClaimedBy != "" {
			t.Fatal("a claim outlived its account")
		}
	}
}

func TestSortingIdeas(t *testing.T) {
	a := setup(t)
	bea := &client{t: t, a: a}
	bea.signup("Bea")
	v := bea.req("POST", "wishes", map[string]string{"title": "Teapot"}, 200)
	id := v["wishes"].([]any)[0].(map[string]any)["id"].(string)
	bea.req("POST", "wishes/"+id+"/status", map[string]string{"Status": "later"}, 400)
	bea.req("POST", "wishes/nope/status", map[string]string{"Status": "got"}, 404)
	v = bea.req("POST", "wishes/"+id+"/status", map[string]string{"Status": "dropped"}, 200)
	if v["wishes"].([]any)[0].(map[string]any)["status"] != "dropped" {
		t.Fatal("status not kept for the owner")
	}
	other := &client{t: t, a: a}
	other.signup("Cara")
	other.req("POST", "wishes/"+id+"/status", map[string]string{"Status": "got"}, 404)
	// Editing keeps the status.
	bea.req("POST", "wishes", map[string]string{"id": id, "title": "Big teapot"}, 200)
	if a.state.Users[a.state.Users[bea.idOf()].ID].Wishes[0].Status != "dropped" {
		t.Fatal("editing cleared the status")
	}
	bea.req("POST", "wishes/"+id+"/status", map[string]string{"Status": ""}, 200)
	bea.req("POST", "wishes/"+id, map[string]string{}, 200) // still removes
	if len(a.state.Users[bea.idOf()].Wishes) != 0 {
		t.Fatal("removing an idea stopped working")
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

func TestExistingIdeasStayWhereTheyWere(t *testing.T) {
	// An idea saved before friends existed has neither flag, so it is for givers only.
	w := wish{ID: "x"}
	if !w.forExchange("any") || w.Friends {
		t.Fatal("an old idea changed audience")
	}
	w.NoExchanges = true
	if w.forExchange("any") {
		t.Fatal("friends-only idea is shown to givers")
	}
	w = wish{Exchanges: []string{"a"}}
	if !w.forExchange("a") || w.forExchange("b") {
		t.Fatal("exchange scope broke")
	}
}

func TestWishesScopedToExchanges(t *testing.T) {
	a := setup(t)
	cs := map[string]*client{}
	for _, n := range []string{"ana", "ben", "cat"} {
		cs[n] = &client{t: t, a: a}
		cs[n].signup(n)
	}
	mk := func(name string) string {
		e := cs["ana"].req("POST", "exchanges", map[string]string{"Name": name, "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
		for _, n := range []string{"ben", "cat"} {
			cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
		}
		return e["id"].(string)
	}
	work, family := mk("Work"), mk("Family")
	other := cs["cat"].req("POST", "exchanges", map[string]string{"Name": "Cat's", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)["id"].(string)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Not in that exchange", "Exchanges": []string{other}}, 400)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Anything", "Exchanges": []string{}}, 200)
	cs["ben"].req("POST", "wishes", map[string]any{"Title": "Mug", "Exchanges": []string{work, work}}, 200)
	me := cs["ben"].req("POST", "wishes", map[string]any{"Title": "Scarf", "Exchanges": []string{family}}, 200)
	if got := me["wishes"].([]any)[1].(map[string]any)["exchanges"].([]any); len(got) != 1 {
		t.Fatalf("duplicate exchange kept: %v", got)
	}
	ben := a.byToken(func(u *user) bool { return u.Email == "ben@example.com" }).ID
	titles := func(ex string) []string {
		var out []string
		for _, w := range wishesFor(a.state.Users[ben], a.state.Exchanges[ex], "") {
			out = append(out, w.Title)
			if w.Exchanges != nil {
				t.Fatal("recipient view reveals the giver's other exchanges")
			}
		}
		return out
	}
	if got := titles(work); !slices.Equal(got, []string{"Anything", "Mug"}) {
		t.Fatalf("work sees %v", got)
	}
	if got := titles(family); !slices.Equal(got, []string{"Anything", "Scarf"}) {
		t.Fatalf("family sees %v", got)
	}
	// The draw view uses the same filter.
	cs["ana"].req("POST", "exchanges/"+family+"/draw", nil, 200)
	for _, n := range []string{"ana", "cat"} {
		r := cs[n].req("GET", "exchanges/"+family, nil, 200)["recipient"].(map[string]any)
		if r["name"] == "ben" {
			for _, w := range r["wishes"].([]any) {
				if w.(map[string]any)["title"] == "Mug" {
					t.Fatal("work-only idea shown in the family exchange")
				}
			}
		}
	}
}
