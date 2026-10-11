package app

import (
	"slices"
	"testing"
)

func TestReviewFixes(t *testing.T) {
	// A claim can always be let go of, after the exchange is archived or the idea's scope narrows.
	p := newParty(t, "Alex", "Bea", "Cara")
	alex, bea := p.c["Alex"], p.c["Bea"]
	alex.req("POST", p.path("draw"), map[string]string{}, 200)
	p.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Alex"})
	bea.req("POST", "wishes", map[string]string{"title": "Teapot", "note": "n"}, 200)
	wish := p.a.state.Users[p.ids["Bea"]].Wishes[0]
	alex.req("POST", p.path("claim"), map[string]any{"Wish": wish.ID, "Claim": true}, 200)
	alex.req("POST", p.path("archive"), map[string]string{}, 200)
	alex.req("POST", p.path("claim"), map[string]any{"Wish": wish.ID, "Claim": true}, 400) // no new claims once archived
	alex.req("POST", p.path("claim"), map[string]any{"Wish": wish.ID, "Claim": false}, 200)
	if p.a.state.Users[p.ids["Bea"]].Wishes[0].ClaimedBy != "" {
		t.Fatal("claim not released after archiving")
	}
	// Narrowing the scope away from the claiming exchange clears the claim.
	q := newParty(t, "Alex", "Bea", "Cara")
	q.c["Alex"].req("POST", q.path("draw"), map[string]string{}, 200)
	q.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Alex"})
	q.c["Bea"].req("POST", "wishes", map[string]string{"title": "Scarf"}, 200)
	w := q.a.state.Users[q.ids["Bea"]].Wishes[0]
	q.c["Alex"].req("POST", q.path("claim"), map[string]any{"Wish": w.ID, "Claim": true}, 200)
	other := q.c["Cara"].req("POST", "exchanges", map[string]string{"Name": "Other", "Date": "2099-01-01", "Budget": "5", "Currency": "GBP"}, 200)
	q.c["Bea"].req("POST", "join", map[string]string{"Code": other["invite"].(string)}, 200)
	q.c["Bea"].req("POST", "wishes", map[string]any{"id": w.ID, "title": "Scarf", "exchanges": []string{other["id"].(string)}}, 200)
	if q.a.state.Users[q.ids["Bea"]].Wishes[0].ClaimedBy != "" {
		t.Fatal("a claim survived the idea leaving its exchange")
	}
	// Deleting an exchange deletes ideas meant only for it instead of showing them everywhere.
	q.c["Bea"].req("POST", "wishes", map[string]any{"title": "Only here", "exchanges": []string{other["id"].(string)}}, 200)
	q.c["Cara"].req("POST", "exchanges/"+other["id"].(string)+"/archive", map[string]string{}, 200)
	q.a.removeExchange(other["id"].(string))
	titles := []string{}
	for _, x := range q.a.state.Users[q.ids["Bea"]].Wishes {
		titles = append(titles, x.Title)
		if len(x.Exchanges) != 0 {
			t.Fatalf("%s gained a scope: %v", x.Title, x.Exchanges)
		}
	}
	if len(titles) != 0 {
		t.Fatalf("ideas meant for the deleted exchange survive: %v", titles)
	}
	// A sorted idea's photo is no longer served to the giver.
	r := newParty(t, "Alex", "Bea", "Cara")
	r.c["Alex"].req("POST", r.path("draw"), map[string]string{}, 200)
	r.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Alex"})
	img := dataURI("image/png", []byte("\x89PNG\r\n\x1a\n0000"))
	r.c["Bea"].req("POST", "wishes", map[string]any{"title": "Mug", "photos": []string{img}}, 200)
	id := r.a.state.Users[r.ids["Bea"]].Wishes[0].ID
	r.c["Alex"].get("/image/"+id+"/0", 200)
	r.c["Bea"].req("POST", "wishes/"+id+"/status", map[string]string{"Status": "got"}, 200)
	r.c["Alex"].get("/image/"+id+"/0", 404)
	r.c["Bea"].get("/image/"+id+"/0", 200)
	// A calendar value has no control characters.
	if got := icsText("a\x00b\x0bc\td,e"); got != "abc"+"d\\,e" {
		t.Fatalf("icsText = %q", got)
	}
}

func TestRefusedRequestsChangeNothing(t *testing.T) {
	a := setup(t)
	alice, bea, cara := &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}
	aliceID, beaID, caraID := alice.signup("alice"), bea.signup("bea"), cara.signup("cara")
	pa, pb, pc := &person{alice, aliceID}, &person{bea, beaID}, &person{cara, caraID}
	makeFriends(t, pa, pb)
	makeFriends(t, pa, pc)
	makeFriends(t, pb, pc)
	// A group gift for Alice, organised by Bea, with Cara in it.
	grp := bea.req("POST", "exchanges", map[string]string{"Name": "For Alice", "Date": "2099-12-20", "Budget": "30", "Currency": "GBP", "Kind": "group", "For": aliceID}, 200)
	grpID := grp["id"].(string)
	cara.req("POST", "join", map[string]string{"Code": grp["invite"].(string)}, 200)
	// Alice organises an exchange with the other two in it, so she can't delete her account yet.
	open := alice.req("POST", "exchanges", map[string]string{"Name": "Open one", "Date": "2099-12-20", "Budget": "25", "Currency": "GBP"}, 200)
	bea.req("POST", "join", map[string]string{"Code": open["invite"].(string)}, 200)
	cara.req("POST", "join", map[string]string{"Code": open["invite"].(string)}, 200)
	alice.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 400)
	if a.state.Users[aliceID] == nil {
		t.Fatal("refused deletion removed the account")
	}
	for _, id := range []string{beaID, caraID} {
		if !slices.Contains(a.state.Users[id].Friends, aliceID) || !slices.Contains(a.state.Users[aliceID].Friends, id) {
			t.Fatal("refused deletion ended a friendship")
		}
	}
	if a.state.Exchanges[grpID] == nil {
		t.Fatal("refused deletion removed the group gift")
	}
	// An administrator's delete is refused the same way.
	a.admins = map[string]bool{"bea@example.com": true}
	a.state.Users[beaID].Verified = true
	bea.req("POST", "admin/users/delete", map[string]string{"ID": aliceID}, 400)
	if !slices.Contains(a.state.Users[beaID].Friends, aliceID) || a.state.Exchanges[grpID] == nil {
		t.Fatal("refused admin deletion still changed things")
	}

	// An edit refused for its steals leaves the other details alone.
	e := alice.req("POST", "exchanges", map[string]string{"Name": "Elephant", "Date": "2099-12-20", "Budget": "25", "Currency": "GBP", "Kind": "elephant"}, 200)
	id := e["id"].(string)
	alice.req("POST", "exchanges/"+id+"/edit", map[string]string{"Name": "Renamed", "Date": "2099-12-21", "Budget": "40", "Currency": "USD", "Note": "changed", "Steals": "abc"}, 400)
	if got := a.state.Exchanges[id]; got.Name != "Elephant" || got.Date != "2099-12-20" || got.Budget != "25" || got.Currency != "GBP" || got.Note != "" {
		t.Fatalf("refused edit still applied: %+v", got)
	}
	alice.req("POST", "exchanges/"+id+"/edit", map[string]string{"Name": "Renamed", "Date": "2099-12-21", "Budget": "40", "Currency": "USD", "Note": "changed", "Steals": "2"}, 200)
	if got := a.state.Exchanges[id]; got.Name != "Renamed" || got.Steals != 2 {
		t.Fatalf("edit not applied: %+v", got)
	}
}

func TestUnsubscribeLinksRetireWhenEmailsComeBackOn(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	u := a.state.Users[c.signup("ana")]
	old := a.unsubToken(u)
	if old != a.mac("gifty-unsub:"+u.ID) {
		t.Fatal("links sent before this change must keep working")
	}
	c.req("POST", "unsubscribe/"+old, nil, 200)
	c.req("POST", "account", map[string]bool{"Notify": true}, 200)
	if u.NoEmail || a.unsubToken(u) == old {
		t.Fatal("turning emails back on should replace the unsubscribe link")
	}
	c.req("POST", "unsubscribe/"+old, nil, 404)
	if u.NoEmail {
		t.Fatal("a retired link still unsubscribed")
	}
	// Saving the setting again while it is already on keeps the current link.
	now := a.unsubToken(u)
	c.req("POST", "account", map[string]bool{"Notify": true}, 200)
	if a.unsubToken(u) != now {
		t.Fatal("link replaced without emails having been off")
	}
	c.req("POST", "unsubscribe/"+now, nil, 200)
	if !u.NoEmail {
		t.Fatal("current link did not unsubscribe")
	}
}
