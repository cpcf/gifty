package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// party is an exchange of named people, each with a signed-in client.
type party struct {
	a   *App
	ids map[string]string
	c   map[string]*client
	ex  string
}

func newParty(t *testing.T, names ...string) *party {
	t.Helper()
	a := setup(t)
	p := &party{a: a, ids: map[string]string{}, c: map[string]*client{}}
	for _, n := range names {
		p.c[n] = &client{t: t, a: a}
		p.ids[n] = p.c[n].signup(n)
	}
	e := p.c[names[0]].req("POST", "exchanges", map[string]string{"Name": "Winter, gifts", "Date": "2099-12-20", "Budget": "25", "Currency": "GBP", "Note": "Bring a hat; wrap it"}, 200)
	p.ex = e["id"].(string)
	for _, n := range names[1:] {
		p.c[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	return p
}
func (p *party) path(action string) string {
	if action == "" {
		return "exchanges/" + p.ex
	}
	return "exchanges/" + p.ex + "/" + action
}
func (p *party) name(id string) string {
	for n, v := range p.ids {
		if v == id {
			return n
		}
	}
	return ""
}

// assign fixes who draws whom, so a test doesn't depend on the random draw.
func (p *party) assign(as map[string]string) {
	m := map[string]string{}
	for g, r := range as {
		m[p.ids[g]] = p.ids[r]
	}
	p.a.state.Exchanges[p.ex].Assignments = m
}

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

func TestAnonymousMessages(t *testing.T) {
	a, o := mailSetup(t)
	p := &party{a: a, ids: map[string]string{}, c: map[string]*client{}}
	names := []string{"Alex", "Bea", "Cara", "Dev"}
	for _, n := range names {
		p.c[n] = &client{t: t, a: a}
		p.ids[n] = p.c[n].signup(n)
		a.state.Users[p.ids[n]].Verified = true
	}
	e := p.c["Alex"].req("POST", "exchanges", map[string]string{"Name": "Gifts", "Date": "2099-12-20", "Budget": "25", "Currency": "GBP"}, 200)
	p.ex = e["id"].(string)
	for _, n := range names[1:] {
		p.c[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	p.c["Bea"].req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "hi"}, 400) // before the draw
	p.c["Alex"].req("POST", p.path("draw"), map[string]string{}, 200)
	p.assign(map[string]string{"Alex": "Bea", "Bea": "Cara", "Cara": "Dev", "Dev": "Alex"})
	a.state.Outbox = nil
	alex, bea, cara := p.c["Alex"], p.c["Bea"], p.c["Cara"]

	// Alex (who buys for Bea) asks; Bea answers without learning who asked.
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "  What size are you?  "}, 200)
	if len(a.state.Outbox) != 1 || a.state.Outbox[0].To != "bea@example.com" {
		t.Fatalf("outbox = %v", a.state.Outbox)
	}
	body := a.state.Outbox[0].Body
	if strings.Contains(body, "Alex") || strings.Contains(body, "What size") {
		t.Fatalf("the email gave away the sender or the text: %q", body)
	}
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "Or a colour?"}, 200)
	if len(a.state.Outbox) != 1 {
		t.Fatal("two messages in a row sent two emails")
	}
	got := bea.req("GET", p.path(""), nil, 200)
	from := got["fromGiver"].([]any)
	if len(from) != 2 || from[0].(map[string]any)["text"] != "What size are you?" || from[0].(map[string]any)["mine"] != false {
		t.Fatalf("Bea sees %v", from)
	}
	if got["toRecipient"] == nil || len(got["toRecipient"].([]any)) != 0 {
		t.Fatalf("Bea's own conversation with Cara = %v", got["toRecipient"])
	}
	bea.req("POST", p.path("message"), map[string]string{"To": "giver", "Text": "Medium, and green please"}, 200)
	if len(a.state.Outbox) != 2 || a.state.Outbox[1].To != "alex@example.com" {
		t.Fatalf("the reply did not email Alex: %v", a.state.Outbox)
	}
	if strings.Contains(a.state.Outbox[1].Body, "Bea") {
		t.Fatal("the reply email named the recipient")
	}
	mine := alex.req("GET", p.path(""), nil, 200)["toRecipient"].([]any)
	if len(mine) != 3 || mine[2].(map[string]any)["mine"] != false || mine[0].(map[string]any)["mine"] != true {
		t.Fatalf("Alex sees %v", mine)
	}
	// Nobody else sees any of it, and nothing in what Bea sees names the giver.
	for _, n := range []string{"Cara", "Dev"} {
		v := p.c[n].req("GET", p.path(""), nil, 200)
		for _, k := range []string{"toRecipient", "fromGiver"} {
			for _, m := range v[k].([]any) {
				if strings.Contains(fmt.Sprint(m), "size") {
					t.Fatalf("%s saw someone else's conversation", n)
				}
			}
		}
	}
	if s := fmt.Sprint(bea.req("GET", p.path(""), nil, 200)["fromGiver"]); strings.Contains(s, p.ids["Alex"]) || strings.Contains(s, "Alex") {
		t.Fatalf("the giver was identified: %s", s)
	}
	// Cara's thread with Dev is separate from Bea's with Alex.
	cara.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "Hello Dev"}, 200)
	if n := len(alex.req("GET", p.path(""), nil, 200)["toRecipient"].([]any)); n != 3 {
		t.Fatalf("conversations are mixed up: %d", n)
	}
	// Limits and bad input.
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "   "}, 400)
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": strings.Repeat("é", 501)}, 400)
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": strings.Repeat("é", 500)}, 200)
	alex.req("POST", p.path("message"), map[string]string{"To": "nobody", "Text": "x"}, 400)
	sent := func() int {
		n := 0
		for _, m := range a.state.Exchanges[p.ex].Threads[p.ids["Alex"]] {
			if m.FromGiver {
				n++
			}
		}
		return n
	}
	for sent() < maxMessages {
		alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "x"}, 200)
	}
	alex.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "one more"}, 400)
	// The other side can still answer.
	bea.req("POST", p.path("message"), map[string]string{"To": "giver", "Text": "still here"}, 200)
	stranger := &client{t: t, a: a}
	stranger.signup("Zed")
	stranger.req("POST", p.path("message"), map[string]string{"To": "recipient", "Text": "hi"}, 404)
	alex.req("POST", p.path("archive"), map[string]string{}, 200)
	bea.req("POST", p.path("message"), map[string]string{"To": "giver", "Text": "late"}, 400)
	_ = o
}

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

// idOf is the signed-in user's ID.
func (c *client) idOf() string { return c.req("GET", "me", nil, 200)["id"].(string) }

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

func TestCalendarFile(t *testing.T) {
	p := newParty(t, "Alex", "Bea")
	p.a.base = "https://gifty.example"
	w := p.c["Bea"].get("/calendar/"+p.ex, 200)
	body := w.Body.String()
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/calendar") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("headers = %v", w.Header())
	}
	flat := strings.ReplaceAll(body, "\r\n ", "")
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "DTSTART;VALUE=DATE:20991220\r\n", "DTEND;VALUE=DATE:20991221\r\n", "SUMMARY:Winter\\, gifts\r\n", "UID:" + p.ex + "@gifty\r\n", "URL:https://gifty.example/#exchange/" + p.ex, "END:VCALENDAR\r\n"} {
		if !strings.Contains(flat, want) {
			t.Errorf("calendar lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(flat, "Bring a hat\\; wrap it") {
		t.Errorf("note missing or unescaped:\n%s", body)
	}
	for _, l := range strings.Split(body, "\r\n") {
		if len(l) > 75 {
			t.Errorf("line not folded: %q", l)
		}
	}
	(&client{t: t, a: p.a}).get("/calendar/"+p.ex, 401)
	out := &client{t: t, a: p.a}
	out.signup("Zed")
	out.get("/calendar/"+p.ex, 404)
	p.c["Bea"].get("/calendar/missing", 404)
}

func TestFoldingKeepsCharactersWhole(t *testing.T) {
	var b strings.Builder
	line := "DESCRIPTION:" + strings.Repeat("é", 100)
	icsLine(&b, line)
	if got := strings.ReplaceAll(b.String(), "\r\n ", ""); got != line+"\r\n" {
		t.Fatal("folding changed the text")
	}
	for _, l := range strings.Split(strings.TrimSuffix(b.String(), "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Fatalf("line of %d octets", len(l))
		}
	}
}

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
