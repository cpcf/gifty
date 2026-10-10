package app

import (
	"fmt"
	"strings"
	"testing"
)

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
