package app

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestDrawKeepsPairsApart(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f"}
	apart := [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}}
	for k := 0; k < 200; k++ {
		d, ok := draw(ids, apart)
		if !ok {
			t.Fatal("a draw exists but none was found")
		}
		seen := map[string]bool{}
		for g, r := range d {
			if g == r || seen[r] {
				t.Fatalf("invalid draw %v", d)
			}
			seen[r] = true
			for _, p := range apart {
				if (g == p[0] && r == p[1]) || (g == p[1] && r == p[0]) {
					t.Fatalf("%s drew %s: %v", g, r, d)
				}
			}
		}
		if len(d) != len(ids) {
			t.Fatalf("draw is incomplete: %v", d)
		}
	}
	// Three people, a kept apart from both others: a can't draw anyone.
	if _, ok := draw([]string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"a", "c"}}); ok {
		t.Fatal("found a draw where there is none")
	}
	// Tight rules still find the one arrangement (a>b>c>d>a in some direction).
	tight := [][2]string{{"a", "c"}, {"b", "d"}}
	for k := 0; k < 50; k++ {
		d, ok := draw([]string{"a", "b", "c", "d"}, tight)
		if !ok || d["a"] == "c" || d["c"] == "a" || d["b"] == "d" || d["d"] == "b" {
			t.Fatalf("tight draw = %v, %v", d, ok)
		}
	}
	// Large groups with many pairs stay quick and valid.
	big := []string{}
	for i := 0; i < 100; i++ {
		big = append(big, fmt.Sprint(i))
	}
	var pairs [][2]string
	for i := 0; i < 100; i += 2 {
		pairs = append(pairs, [2]string{big[i], big[i+1]})
	}
	if d, ok := draw(big, pairs); !ok || len(d) != 100 {
		t.Fatal("no draw for 100 people in 50 couples")
	}
}

func TestKeepApartRules(t *testing.T) {
	p := newParty(t, "Alex", "Bea", "Cara", "Dev")
	o, bea := p.c["Alex"], p.c["Bea"]
	pair := func(x, y string) [][2]string { return [][2]string{{p.ids[x], p.ids[y]}} }
	if v := o.req("GET", p.path(""), nil, 200); len(v["apart"].([]any)) != 0 {
		t.Fatal("expected an empty list")
	}
	bea.req("POST", p.path("apart"), map[string]any{"Pairs": pair("Alex", "Cara")}, 403)
	o.req("POST", p.path("apart"), map[string]any{"Pairs": pair("Bea", "Bea")}, 400)
	o.req("POST", p.path("apart"), map[string]any{"Pairs": [][2]string{{p.ids["Bea"], "nobody"}}}, 400)
	v := o.req("POST", p.path("apart"), map[string]any{"Pairs": [][2]string{{p.ids["Cara"], p.ids["Bea"]}, {p.ids["Bea"], p.ids["Cara"]}}}, 200)
	if len(v["apart"].([]any)) != 1 {
		t.Fatalf("pairs not deduplicated: %v", v["apart"])
	}
	// Only the organiser ever sees the pairs.
	if _, ok := bea.req("GET", p.path(""), nil, 200)["apart"]; ok {
		t.Fatal("a member was shown the pairs")
	}
	// Alex kept apart from everyone else would force who Alex draws and is drawn by, so a person can be in one pair only.
	o.req("POST", p.path("apart"), map[string]any{"Pairs": [][2]string{{p.ids["Alex"], p.ids["Bea"]}, {p.ids["Alex"], p.ids["Cara"]}}}, 400)
	o.req("POST", p.path("apart"), map[string]any{"Pairs": [][2]string{{p.ids["Alex"], p.ids["Bea"]}, {p.ids["Cara"], p.ids["Dev"]}}}, 200)
	// A pair goes when one of them leaves.
	p.c["Cara"].req("POST", p.path("leave"), map[string]string{}, 200)
	if got := p.a.state.Exchanges[p.ex].Apart; len(got) != 1 || slices.Contains(got[0][:], p.ids["Cara"]) {
		t.Fatalf("pairs after Cara left = %v", got)
	}
	p.c["Cara"].req("POST", "join", map[string]string{"Code": p.a.state.Exchanges[p.ex].Invite}, 200)
	o.req("POST", p.path("apart"), map[string]any{"Pairs": pair("Alex", "Bea")}, 200)
	o.req("POST", p.path("remove"), map[string]string{"ID": p.ids["Bea"]}, 200)
	if len(p.a.state.Exchanges[p.ex].Apart) != 0 {
		t.Fatal("a pair survived one of its people being removed")
	}
	bea.req("POST", "join", map[string]string{"Code": p.a.state.Exchanges[p.ex].Invite}, 200)
	o.req("POST", p.path("apart"), map[string]any{"Pairs": pair("Alex", "Bea")}, 200)
	for k := 0; k < 30; k++ {
		e := p.a.state.Exchanges[p.ex]
		e.Assignments = nil
		o.req("POST", p.path("draw"), map[string]string{}, 200)
		if e.Assignments[p.ids["Alex"]] == p.ids["Bea"] || e.Assignments[p.ids["Bea"]] == p.ids["Alex"] {
			t.Fatal("kept-apart pair drew each other")
		}
	}
	o.req("POST", p.path("apart"), map[string]any{"Pairs": [][2]string{}}, 400) // locked after the draw
	if _, ok := o.req("GET", p.path(""), nil, 200)["apart"]; ok {
		t.Fatal("pairs still shown after the draw")
	}
	// A draw refused for lack of any arrangement leaves the exchange open.
	q := newParty(t, "Alex", "Bea", "Cara")
	q.a.state.Exchanges[q.ex].Apart = [][2]string{{q.ids["Alex"], q.ids["Bea"]}, {q.ids["Alex"], q.ids["Cara"]}}
	q.c["Alex"].req("POST", q.path("draw"), map[string]string{}, 400)
	if len(q.a.state.Exchanges[q.ex].Assignments) != 0 {
		t.Fatal("a refused draw still assigned names")
	}
}

func TestRevealDay(t *testing.T) {
	p := newParty(t, "Alex", "Bea", "Cara")
	alex, bea := p.c["Alex"], p.c["Bea"]
	alex.req("POST", p.path("reveal"), map[string]string{}, 400) // not drawn
	alex.req("POST", p.path("draw"), map[string]string{}, 200)
	p.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Alex"})
	if v := alex.req("GET", p.path(""), nil, 200); v["canReveal"] != nil || v["revealed"] != nil {
		t.Fatal("reveal offered months early")
	}
	alex.req("POST", p.path("reveal"), map[string]string{}, 400) // too early
	e := p.a.state.Exchanges[p.ex]
	e.Date = time.Now().UTC().Format("2006-01-02")
	if v := alex.req("GET", p.path(""), nil, 200); v["canReveal"] != true {
		t.Fatal("reveal not offered on the day")
	}
	if v := bea.req("GET", p.path(""), nil, 200); v["canReveal"] != nil {
		t.Fatal("reveal offered to a member")
	}
	bea.req("POST", p.path("reveal"), map[string]string{}, 403)
	if _, ok := bea.req("GET", p.path(""), nil, 200)["pairs"]; ok {
		t.Fatal("pairs shown before the reveal")
	}
	v := alex.req("POST", p.path("reveal"), map[string]string{}, 200)
	if v["revealed"] != true || v["canReveal"] != nil {
		t.Fatalf("reveal = %v", v)
	}
	alex.req("POST", p.path("reveal"), map[string]string{}, 400)
	pairs := bea.req("GET", p.path(""), nil, 200)["pairs"].([]any)
	got := []string{}
	for _, x := range pairs {
		m := x.(map[string]any)
		got = append(got, p.name(m["giver"].(string))+">"+p.name(m["recipient"].(string)))
	}
	if !slices.Equal(got, []string{"Alex>Bea", "Bea>Cara", "Cara>Alex"}) {
		t.Fatalf("pairs = %v", got)
	}
	// Still possible once archived: reveal is allowed on a finished exchange.
	q := newParty(t, "Alex", "Bea", "Cara")
	q.c["Alex"].req("POST", q.path("draw"), map[string]string{}, 200)
	q.a.state.Exchanges[q.ex].Date = time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	q.c["Alex"].req("POST", q.path("archive"), map[string]string{}, 200)
	q.c["Alex"].req("POST", q.path("reveal"), map[string]string{}, 200)
}

func TestDrawProperties(t *testing.T) {
	for n := 3; n <= 100; n++ {
		ids := []string{}
		for i := 0; i < n; i++ {
			ids = append(ids, fmt.Sprint(i))
		}
		for k := 0; k < 20; k++ {
			d, _ := draw(ids, nil)
			seen := map[string]bool{}
			for _, id := range ids {
				to := d[id]
				if to == id || seen[to] || to == "" {
					t.Fatalf("invalid draw for %d members", n)
				}
				seen[to] = true
			}
		}
	}
}
