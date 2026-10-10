package app

import (
	"testing"
)

func TestExchangeLifecycleAndPrivacy(t *testing.T) {
	a := setup(t)
	owner := &client{t: t, a: a}
	one := &client{t: t, a: a}
	two := &client{t: t, a: a}
	stranger := &client{t: t, a: a}
	ownerID := owner.signup("Alex")
	oneID := one.signup("Bea")
	two.signup("Charlie")
	stranger.signup("Dana")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Winter gifts", "Date": "2099-12-20", "Budget": "25.50", "Currency": "GBP", "Note": "At our place"}, 200)
	id := e["id"].(string)
	code := e["invite"].(string)
	path := "exchanges/" + id
	owner.req("POST", path+"/draw", map[string]string{}, 400)
	stranger.req("GET", path, nil, 404)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	two.req("POST", "join", map[string]string{"Code": code}, 200)
	private := one.req("GET", path, nil, 200)
	if _, ok := private["invite"]; ok {
		t.Fatal("non-owner received invite management token")
	}
	if len(private["members"].([]any)) != 3 {
		t.Fatal("join was not idempotent")
	}
	one.req("POST", path+"/draw", map[string]string{}, 403)
	one.req("POST", "wishes", map[string]string{"title": "Blue mug", "url": "https://example.com/mug", "note": "Large, please"}, 200)
	drawn := owner.req("POST", path+"/draw", map[string]string{}, 200)
	if drawn["drawn"] != true {
		t.Fatal("draw not recorded")
	}
	for _, c := range []*client{owner, one, two} {
		v := c.req("GET", path, nil, 200)
		if v["recipient"] == nil {
			t.Fatal("missing recipient")
		}
		for _, key := range []string{"assignments", "invite", "email", "hash", "salt"} {
			if _, ok := v[key]; ok {
				t.Fatalf("leaked %s", key)
			}
		}
	}
	for from, to := range a.state.Exchanges[id].Assignments {
		if from == to {
			t.Fatal("self draw")
		}
	}
	one.req("POST", path+"/leave", map[string]string{}, 400)
	owner.req("POST", path+"/remove", map[string]string{"ID": oneID}, 400)
	owner.req("POST", path+"/draw", map[string]string{}, 400)
	stranger.req("POST", "join", map[string]string{"Code": code}, 404)
	owner.req("POST", path+"/edit", map[string]string{"Name": "changed"}, 400)
	reopened, err := openApp(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Exchanges[id].Assignments[ownerID] != a.state.Exchanges[id].Assignments[ownerID] {
		t.Fatal("draw not durable")
	}
	owner.a = reopened
	owner.req("GET", "me", nil, 200)
	owner.req("POST", path+"/archive", map[string]string{}, 200)
	owner.req("POST", path+"/rotate", map[string]string{}, 400)
	owner.req("POST", "logout", map[string]string{}, 200)
	owner.req("GET", "me", nil, 401)
}

func TestMembershipAndInviteRotation(t *testing.T) {
	a := setup(t)
	o := &client{t: t, a: a}
	b := &client{t: t, a: a}
	o.signup("Owner")
	bid := b.signup("Guest")
	e := o.req("POST", "exchanges", map[string]string{"Name": "Friends", "Date": "2099-01-01", "Budget": "10", "Currency": "EUR"}, 200)
	path := "exchanges/" + e["id"].(string)
	code := e["invite"].(string)
	if inv := b.req("GET", "invite/"+code, nil, 200); inv["organiser"] != "Owner" || inv["people"] != float64(1) {
		t.Fatalf("invite = %v", inv)
	}
	b.req("POST", "join", map[string]string{"Code": code}, 200)
	b.req("POST", path+"/leave", map[string]string{}, 200)
	b.req("GET", path, nil, 404)
	b.req("POST", "join", map[string]string{"Code": code}, 200)
	o.req("POST", path+"/remove", map[string]string{"ID": bid}, 200)
	b.req("GET", path, nil, 404)
	o.req("POST", path+"/rotate", map[string]string{}, 200)
	b.req("GET", "invite/"+code, nil, 404)
	b.req("POST", "join", map[string]string{"Code": code}, 404)
}

func TestExchangeDeletion(t *testing.T) {
	a := setup(t)
	owner, member, stranger := &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}
	owner.signup("Alex")
	memberID := member.signup("Bea")
	stranger.signup("Cara")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Old gifts", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	id, code := e["id"].(string), e["invite"].(string)
	path := "exchanges/" + id
	member.req("POST", "join", map[string]string{"Code": code}, 200)
	// An idea scoped to the exchange loses that scope when the exchange goes.
	member.req("POST", "wishes", map[string]any{"title": "Blue mug", "exchanges": []string{id}}, 200)
	// Deleting is for organisers, and only once the exchange is archived.
	stranger.req("POST", path+"/delete", map[string]string{}, 404)
	member.req("POST", path+"/delete", map[string]string{}, 403)
	owner.req("POST", path+"/delete", map[string]string{}, 400)
	owner.req("POST", path+"/archive", map[string]string{}, 200)
	member.req("POST", path+"/delete", map[string]string{}, 403)
	owner.req("POST", path+"/delete", map[string]string{}, 200)
	owner.req("GET", path, nil, 404)
	member.req("GET", path, nil, 404)
	if a.state.Exchanges[id] != nil {
		t.Fatal("exchange not deleted")
	}
	for _, w := range a.state.Users[memberID].Wishes {
		if len(w.Exchanges) != 0 {
			t.Fatal("wish scope not cleared")
		}
	}
	// The deletion is durable.
	reopened, err := openApp(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Exchanges[id] != nil {
		t.Fatal("deleted exchange persisted")
	}
}
