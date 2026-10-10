package app

import (
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
