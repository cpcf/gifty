package app

import (
	"testing"
)

func TestAccountDeletion(t *testing.T) {
	a := setup(t)
	owner, one, two, loner := &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}, &client{t: t, a: a}
	owner.signup("Alex")
	oneID := one.signup("Bea")
	two.signup("Charlie")
	loner.signup("Dana")
	e := owner.req("POST", "exchanges", map[string]string{"Name": "Winter", "Date": "2099-12-20", "Budget": "20", "Currency": "GBP"}, 200)
	id, code := e["id"].(string), e["invite"].(string)
	one.req("POST", "join", map[string]string{"Code": code}, 200)
	two.req("POST", "join", map[string]string{"Code": code}, 200)
	// Wrong password changes nothing.
	one.req("POST", "account/delete", map[string]string{"Password": "not the password"}, 403)
	// The organiser must archive an exchange that has other people in it.
	owner.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 400)
	// Before the draw a member just disappears.
	one.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	if a.state.Users[oneID] != nil || len(a.state.Exchanges[id].Members) != 2 {
		t.Fatal("member was not removed")
	}
	one.req("GET", "me", nil, 401)
	// Someone alone in an exchange takes it with them.
	solo := loner.req("POST", "exchanges", map[string]string{"Name": "Solo", "Date": "2099-12-20", "Budget": "5", "Currency": "GBP"}, 200)["id"].(string)
	loner.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	if a.state.Exchanges[solo] != nil {
		t.Fatal("solo exchange survived its only member")
	}
	// After the draw a deleted member stays on as an empty account.
	three := &client{t: t, a: a}
	threeID := three.signup("Eve")
	three.req("POST", "join", map[string]string{"Code": code}, 200)
	owner.req("POST", "exchanges/"+id+"/draw", map[string]string{}, 200)
	three.req("POST", "account/delete", map[string]string{"Password": "correct horse battery staple"}, 200)
	u := a.state.Users[threeID]
	if u == nil || u.Name != deletedName || u.Email != "" || u.Hash != "" || len(a.state.Exchanges[id].Members) != 3 {
		t.Fatalf("expected an emptied account, got %+v", u)
	}
	owner.req("GET", "exchanges/"+id, nil, 200)
	(&client{t: t, a: a}).req("POST", "login", map[string]string{"Email": "", "Password": "correct horse battery staple"}, 401)
	// The address can be used again.
	(&client{t: t, a: a}).signup("Eve")
}
