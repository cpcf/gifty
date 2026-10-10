package app

import (
	"testing"
)

func TestAdmin(t *testing.T) {
	a := setup(t)
	a.admins = map[string]bool{"boss@example.com": true}
	boss, member := &client{t: t, a: a}, &client{t: t, a: a}
	bossID := boss.signup("boss")
	memberID := member.signup("Fran")
	// Not confirmed yet: no admin powers.
	if boss.req("GET", "me", nil, 200)["admin"] != false {
		t.Fatal("unconfirmed address got admin")
	}
	boss.req("GET", "admin/overview", nil, 404)
	a.state.Users[bossID].Verified = true
	if boss.req("GET", "me", nil, 200)["admin"] != true {
		t.Fatal("admin not recognised")
	}
	member.req("GET", "admin/overview", nil, 404)
	member.req("POST", "admin/users/delete", map[string]string{"ID": bossID}, 404)
	o := boss.req("GET", "admin/overview", nil, 200)
	if len(o["users"].([]any)) != 2 {
		t.Fatal("overview should list both accounts")
	}
	boss.req("POST", "admin/users/delete", map[string]string{"ID": bossID}, 400)
	e := member.req("POST", "exchanges", map[string]string{"Name": "Fran’s", "Date": "2099-12-20", "Budget": "5", "Currency": "GBP"}, 200)
	id := e["id"].(string)
	boss.req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	boss.req("POST", "admin/users/delete", map[string]string{"ID": memberID}, 400)
	boss.req("POST", "admin/exchanges/delete", map[string]string{"ID": id}, 200)
	boss.req("POST", "admin/users/delete", map[string]string{"ID": memberID}, 200)
	if a.state.Users[memberID] != nil || len(a.state.Exchanges) != 0 {
		t.Fatal("admin deletion left data behind")
	}
}
