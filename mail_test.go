package main

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

type outbox struct{ sent []string }

func (o *outbox) send(to string, msg []byte) error {
	o.sent = append(o.sent, string(msg))
	return nil
}
func mailSetup(t *testing.T) (*App, *outbox) {
	t.Helper()
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	return a, o
}
func linkToken(t *testing.T, body, route string) string {
	t.Helper()
	m := regexp.MustCompile(`#` + route + `/([A-Za-z0-9_-]+)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s link in %q", route, body)
	}
	return m[1]
}
func lastMail(t *testing.T, a *App, to string) Mail {
	t.Helper()
	for i := len(a.state.Outbox) - 1; i >= 0; i-- {
		if a.state.Outbox[i].To == to {
			return a.state.Outbox[i]
		}
	}
	t.Fatalf("no email to %s", to)
	return Mail{}
}
func future(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02") }

func TestEmailOffByDefault(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	if c.req("GET", "config", nil, 200)["mail"] != false {
		t.Fatal("mail reported on without configuration")
	}
	c.signup("ana")
	if len(a.state.Outbox) != 0 {
		t.Fatal("queued email with mail off")
	}
	c.req("POST", "reset/request", map[string]string{"Email": "ana@example.com"}, 200)
	if len(a.state.Outbox) != 0 {
		t.Fatal("queued reset with mail off")
	}
}

func TestVerificationAndReset(t *testing.T) {
	a, _ := mailSetup(t)
	c := &client{t: t, a: a}
	c.signup("ana")
	if c.req("GET", "me", nil, 200)["verified"] != false {
		t.Fatal("new account verified without confirmation")
	}
	tok := linkToken(t, lastMail(t, a, "ana@example.com").Body, "verify")
	c.req("POST", "verify/resend", nil, 429)
	(&client{t: t, a: a}).req("POST", "verify", map[string]string{"Token": tok}, 200)
	if c.req("GET", "me", nil, 200)["verified"] != true {
		t.Fatal("confirmation link did not verify")
	}
	c.req("POST", "verify", map[string]string{"Token": tok}, 404)
	c.req("POST", "verify/resend", nil, 400)

	anon := &client{t: t, a: a}
	before := len(a.state.Outbox)
	anon.req("POST", "reset/request", map[string]string{"Email": "nobody@example.com"}, 200)
	if len(a.state.Outbox) != before {
		t.Fatal("reset email queued for unknown address")
	}
	anon.req("POST", "reset/request", map[string]string{"Email": "ANA@example.com "}, 200)
	reset := linkToken(t, lastMail(t, a, "ana@example.com").Body, "reset")
	before = len(a.state.Outbox)
	anon.req("POST", "reset/request", map[string]string{"Email": "ana@example.com"}, 200)
	if len(a.state.Outbox) != before {
		t.Fatal("repeat reset request was not rate limited")
	}
	anon.req("POST", "reset", map[string]string{"Token": reset, "Password": "short"}, 400)
	anon.req("POST", "reset", map[string]string{"Token": "wrong", "Password": "a brand new long password"}, 404)
	anon.req("POST", "reset", map[string]string{"Token": reset, "Password": "a brand new long password"}, 200)
	anon.req("GET", "me", nil, 200)
	c.req("GET", "me", nil, 401)
	anon.req("POST", "reset", map[string]string{"Token": reset, "Password": "another long password"}, 404)
	fresh := &client{t: t, a: a}
	fresh.req("POST", "login", map[string]string{"Email": "ana@example.com", "Password": "a brand new long password"}, 200)
}

func TestDrawEmailsArePrivateAndRespectOptOut(t *testing.T) {
	a, _ := mailSetup(t)
	names := []string{"ana", "ben", "cat", "dev"}
	clients := map[string]*client{}
	for _, n := range names {
		clients[n] = &client{t: t, a: a}
		clients[n].signup(n)
	}
	for _, u := range a.state.Users {
		u.Verified = u.Email != "dev@example.com"
	}
	clients["cat"].req("POST", "account", map[string]bool{"Notify": false}, 200)
	e := clients["ana"].req("POST", "exchanges", map[string]string{"Name": "Office swap", "Date": future(30), "Budget": "20", "Currency": "GBP"}, 200)
	for _, n := range names[1:] {
		clients[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	a.state.Outbox = nil
	clients["ana"].req("POST", "exchanges/"+e["id"].(string)+"/draw", nil, 200)
	ana := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
	ana.LastMail = time.Time{}
	a.queue(ana, true, "Test", "Body")
	if !ana.LastMail.IsZero() {
		t.Fatal("a notification started the reset cooldown")
	}
	a.state.Outbox = a.state.Outbox[:len(a.state.Outbox)-1]
	got := map[string]bool{}
	bodies := map[string]bool{}
	for _, m := range a.state.Outbox {
		got[m.To] = true
		if m.Unsub == "" || !strings.Contains(m.Subject, "Office swap") {
			t.Fatalf("draw email missing unsubscribe or exchange name: %+v", m)
		}
		bodies[strings.Split(m.Body, "\n--\n")[0]] = true
	}
	if len(got) != 2 || !got["ana@example.com"] || !got["ben@example.com"] {
		t.Fatalf("draw emails went to %v; want only confirmed, opted-in members", got)
	}
	if len(bodies) != 1 {
		t.Fatal("draw emails differ per person, so they could reveal a recipient")
	}
}

func TestOutboxRetriesAndSends(t *testing.T) {
	a, o := mailSetup(t)
	c := &client{t: t, a: a}
	c.signup("ana")
	fail := true
	a.mailer = func(to string, msg []byte) error {
		if fail {
			return errors.New("connection refused")
		}
		return o.send(to, msg)
	}
	now := time.Now()
	a.tick(now)
	if len(a.state.Outbox) != 1 || a.state.Outbox[0].Attempts != 1 || !a.state.Outbox[0].Next.After(now) {
		t.Fatalf("failed send not kept for retry: %+v", a.state.Outbox)
	}
	fail = false
	a.tick(now)
	if len(o.sent) != 0 {
		t.Fatal("retried before backoff")
	}
	a.tick(now.Add(time.Hour))
	if len(a.state.Outbox) != 0 || len(o.sent) != 1 {
		t.Fatal("message not sent after backoff")
	}
	reloaded, err := openApp(a.path)
	if err != nil || len(reloaded.state.Outbox) != 0 {
		t.Fatal("sent message still in saved outbox")
	}
}

func TestOneClickUnsubscribe(t *testing.T) {
	a, _ := mailSetup(t)
	c := &client{t: t, a: a}
	c.signup("ana")
	u := a.byToken(func(u *User) bool { return u.Email == "ana@example.com" })
	u.Verified = true
	a.queue(u, true, "Test", "Body")
	m := lastMail(t, a, "ana@example.com")
	msg := string(a.message(m))
	if !strings.Contains(msg, "List-Unsubscribe: <https://gifty.example/api/unsubscribe/"+u.Unsub+">") || !strings.Contains(msg, "List-Unsubscribe-Post: List-Unsubscribe=One-Click") {
		t.Fatalf("missing unsubscribe headers:\n%s", msg)
	}
	// Mail providers post a form, not JSON, and send no Origin.
	r := httptest.NewRequest("POST", "/api/unsubscribe/"+u.Unsub, strings.NewReader("List-Unsubscribe=One-Click"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 200 || !u.NoEmail {
		t.Fatalf("one-click unsubscribe failed: %d %s", w.Code, w.Body.String())
	}
	(&client{t: t, a: a}).req("POST", "unsubscribe/wrong", nil, 404)
	before := len(a.state.Outbox)
	a.queue(u, true, "Test", "Body")
	if len(a.state.Outbox) != before {
		t.Fatal("notification queued after opting out")
	}
	c.req("POST", "account", map[string]bool{"Notify": true}, 200)
	if u.NoEmail {
		t.Fatal("notifications not turned back on")
	}
}

func TestMessageHeadersCannotBeInjected(t *testing.T) {
	a, _ := mailSetup(t)
	msg := string(a.message(Mail{ID: "x", To: "ana@example.com", Subject: "Names drawn: Swap\r\nBcc: evil@example.com", Body: "Hi"}))
	head := strings.SplitN(msg, "\r\n\r\n", 2)[0]
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("header injected:\n%s", head)
		}
	}
}

func rem(spec ...any) []map[string]any {
	out := []map[string]any{}
	for i := 0; i < len(spec); i += 2 {
		out = append(out, map[string]any{"n": spec[i], "unit": spec[i+1]})
	}
	return out
}

// reminderSetup makes a three-person exchange 40 days out and returns clients by name and its ID.
func reminderSetup(t *testing.T) (*App, map[string]*client, string) {
	a, _ := mailSetup(t)
	cs := map[string]*client{}
	for _, n := range []string{"ana", "ben", "cat"} {
		cs[n] = &client{t: t, a: a}
		cs[n].signup(n)
	}
	for _, u := range a.state.Users {
		u.Verified = true
	}
	e := cs["ana"].req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": future(40), "Budget": "20", "Currency": "GBP"}, 200)
	for _, n := range []string{"ben", "cat"} {
		cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	return a, cs, e["id"].(string)
}
func sentOn(a *App, at time.Time) map[string]bool {
	a.state.Outbox = nil
	a.remind(at)
	out := map[string]bool{}
	for _, m := range a.state.Outbox {
		out[strings.TrimSuffix(m.To, "@example.com")] = true
	}
	return out
}

func TestReminderValidation(t *testing.T) {
	a, cs, id := reminderSetup(t)
	path := "exchanges/" + id + "/reminders"
	cs["ben"].req("POST", path, map[string]any{"Reminders": rem(1, "day")}, 403)
	for _, bad := range [][]map[string]any{rem(0, "week"), rem(13, "month"), rem(53, "week"), rem(366, "day"), rem(-1, "day"), rem(1, "year"), rem(1, "day", 2, "day", 3, "day", 4, "day", 5, "day", 6, "day", 7, "day", 8, "day", 9, "day")} {
		cs["ana"].req("POST", path, map[string]any{"Reminders": bad}, 400)
	}
	got := cs["ana"].req("POST", path, map[string]any{"Reminders": rem(1, "day", 2, "month", 0, "day", 1, "day", 10, "day", 1, "week")}, 200)["reminders"].([]any)
	var keys []string
	for _, r := range got {
		m := r.(map[string]any)
		keys = append(keys, fmt.Sprint(m["n"], m["unit"]))
	}
	if want := []string{"2month", "10day", "1week", "1day", "0day"}; !slices.Equal(keys, want) {
		t.Fatalf("got %v, want duplicates dropped and ordered furthest first %v", keys, want)
	}
	if a.state.Exchanges[id].Reminders[0] != (Reminder{2, "month"}) {
		t.Fatal("schedule not stored")
	}
}

func TestReminders(t *testing.T) {
	a, cs, id := reminderSetup(t)
	ex := a.state.Exchanges[id]
	date, _ := time.Parse("2006-01-02", ex.Date)
	// Organiser default: 1 month and on the day. Ben: 10 days. Cat: none. Ana keeps the default.
	cs["ana"].req("POST", "exchanges/"+id+"/reminders", map[string]any{"Reminders": rem(1, "month", 0, "day")}, 200)
	cs["ben"].req("POST", "exchanges/"+id+"/my-reminders", map[string]any{"Reminders": rem(10, "day")}, 200)
	cs["cat"].req("POST", "exchanges/"+id+"/my-reminders", map[string]any{"Reminders": rem()}, 200)
	m := cs["ben"].req("GET", "exchanges/"+id, nil, 200)["mine"].(map[string]any)
	if m["source"] != "exchange" {
		t.Fatalf("source %v", m["source"])
	}
	cs["ana"].req("POST", "exchanges/"+id+"/ready", map[string]bool{"Ready": true}, 400)
	cs["ana"].req("POST", "exchanges/"+id+"/draw", nil, 200)

	month := date.AddDate(0, -1, 0)
	for _, step := range []struct {
		at   time.Time
		want []string
	}{
		{month.AddDate(0, 0, -1), nil},
		{month, []string{"ana"}},
		{month.AddDate(0, 0, 1), nil},
		{date.AddDate(0, 0, -10), []string{"ben"}},
		{date, []string{"ana"}},
		{date.AddDate(0, 0, 1), nil},
	} {
		got := sentOn(a, step.at)
		if len(got) != len(step.want) {
			t.Fatalf("%s: reminded %v, want %v", step.at.Format("2006-01-02"), got, step.want)
		}
		for _, n := range step.want {
			if !got[n] {
				t.Fatalf("%s: reminded %v, want %v", step.at.Format("2006-01-02"), got, step.want)
			}
		}
	}

	// Marking your gift as ready stops reminders; going back to the default picks it up again.
	e2 := cs["ana"].req("POST", "exchanges", map[string]string{"Name": "Late", "Date": future(3), "Budget": "20", "Currency": "GBP"}, 200)
	ex2 := a.state.Exchanges[e2["id"].(string)]
	ex2.Members = slices.Clone(ex.Members)
	cs["ana"].req("POST", "exchanges/"+ex2.ID+"/draw", nil, 200)
	cs["ben"].req("POST", "exchanges/"+ex2.ID+"/ready", map[string]bool{"Ready": true}, 200)
	// Everyone can see who has their gift, but nothing about who it is for.
	view := cs["cat"].req("GET", "exchanges/"+ex2.ID, nil, 200)
	for _, raw := range view["members"].([]any) {
		m := raw.(map[string]any)
		if want := m["id"] == a.state.Exchanges[ex2.ID].Members[1]; m["ready"] != want || len(m) != 3 {
			t.Fatalf("member view = %v", m)
		}
	}
	// A reminder added after its date has passed isn't sent late.
	cs["cat"].req("POST", "exchanges/"+ex2.ID+"/my-reminders", map[string]any{"Reminders": rem(2, "week", 1, "day")}, 200)
	if got := sentOn(a, time.Now()); len(got) != 0 {
		t.Fatalf("late reminders sent: %v", got)
	}
	if got := sentOn(a, time.Now().AddDate(0, 0, 2)); len(got) != 2 || got["ben"] {
		t.Fatalf("day-before reminder for the second exchange: %v", got)
	}
}

func TestAccountReminderDefault(t *testing.T) {
	a, _ := mailSetup(t)
	cs := map[string]*client{}
	for _, n := range []string{"ana", "ben", "cat"} {
		cs[n] = &client{t: t, a: a}
		cs[n].signup(n)
	}
	for _, u := range a.state.Users {
		u.Verified = true
	}
	e := cs["ana"].req("POST", "exchanges", map[string]string{"Name": "Swap", "Date": future(20), "Budget": "20", "Currency": "GBP"}, 200)
	id := e["id"].(string)
	for _, n := range []string{"ben", "cat"} {
		cs[n].req("POST", "join", map[string]string{"Code": e["invite"].(string)}, 200)
	}
	mine := func(c *client) (string, []any) {
		m := c.req("GET", "exchanges/"+id, nil, 200)["mine"].(map[string]any)
		var keys []any
		for _, r := range m["reminders"].([]any) {
			keys = append(keys, fmt.Sprint(r.(map[string]any)["n"], r.(map[string]any)["unit"]))
		}
		return m["source"].(string), keys
	}
	if src, days := mine(cs["ben"]); src != "organiser" || !slices.Equal(days, []any{"1week", "1day"}) {
		t.Fatalf("got %s %v, want the organiser's default", src, days)
	}
	cs["ben"].req("POST", "account/reminders", map[string]any{"Reminders": rem(14, "day", 3, "day")}, 200)
	cs["ben"].req("POST", "account/reminders", map[string]any{"Reminders": rem(0, "month")}, 400)
	if src, days := mine(cs["ben"]); src != "account" || !slices.Equal(days, []any{"14day", "3day"}) {
		t.Fatalf("got %s %v, want the account default", src, days)
	}
	// A choice made on the exchange wins over the account default.
	cs["ben"].req("POST", "exchanges/"+id+"/my-reminders", map[string]any{"Reminders": rem(1, "day")}, 200)
	if src, _ := mine(cs["ben"]); src != "exchange" {
		t.Fatalf("got %s, want the exchange choice", src)
	}
	cs["ben"].req("POST", "exchanges/"+id+"/my-reminders", map[string]any{"Default": true}, 200)
	cs["cat"].req("POST", "account/reminders", map[string]any{"Reminders": rem()}, 200)
	me := cs["cat"].req("GET", "me", nil, 200)
	if me["ownReminders"] != true || len(me["reminders"].([]any)) != 0 {
		t.Fatalf("account default not reported: %v", me)
	}
	cs["ana"].req("POST", "exchanges/"+id+"/draw", nil, 200)

	sent := func(at int) map[string]bool {
		a.state.Outbox = nil
		a.remind(time.Now().AddDate(0, 0, at))
		out := map[string]bool{}
		for _, m := range a.state.Outbox {
			out[strings.TrimSuffix(m.To, "@example.com")] = true
		}
		return out
	}
	// 20 days out. Ana: organiser default (7, 1). Ben: account (14, 3). Cat: none.
	if got := sent(6); len(got) != 1 || !got["ben"] {
		t.Fatalf("14 days out: %v", got)
	}
	if got := sent(13); len(got) != 1 || !got["ana"] {
		t.Fatalf("7 days out: %v", got)
	}
	// Switching back to the organiser's default once the week reminder has passed doesn't send it late.
	cs["ben"].req("POST", "account/reminders", map[string]any{"Default": true}, 200)
	a.dueReminders(a.state.Exchanges[id], a.byToken(func(u *User) bool { return u.Email == "ben@example.com" }).ID, time.Now().AddDate(0, 0, 13))
	if got := sent(17); len(got) != 0 {
		t.Fatalf("3 days out: %v", got)
	}
	if got := sent(19); len(got) != 2 || !got["ana"] || !got["ben"] {
		t.Fatalf("1 day out: %v", got)
	}
}
