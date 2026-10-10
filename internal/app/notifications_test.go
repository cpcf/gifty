package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBirthdayEmails(t *testing.T) {
	a, _ := mailSetup(t)
	p := people(t, a, "Ana", "Ben")
	ana, ben := p["Ana"], p["Ben"]
	makeFriends(t, ben, ana)
	for _, id := range []string{ana.id, ben.id} {
		a.state.Users[id].Verified = true
	}
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	ana.c.req("POST", "birthday", map[string]any{"Month": 10, "Day": 15, "Show": true}, 200)
	count := func() int {
		n := 0
		for _, m := range a.state.Outbox {
			if m.To == "ben@example.com" && strings.Contains(m.Subject, "birthday") {
				n++
			}
		}
		return n
	}
	a.birthdays(now.AddDate(0, 0, -1)) // six days before: nothing yet
	if count() != 0 {
		t.Fatal("emailed before the week")
	}
	a.birthdays(now)
	a.birthdays(now.Add(time.Hour)) // a second pass the same day sends nothing more
	if count() != 1 {
		t.Fatalf("week-before emails = %d", count())
	}
	m := lastMail(t, a, "ben@example.com")
	if !strings.Contains(m.Subject, "Ana’s birthday is in a week") || !strings.Contains(m.Body, "Thursday 15 October") || !strings.Contains(m.Body, "/#friends/"+ana.id) {
		t.Fatalf("mail = %q %q", m.Subject, m.Body)
	}
	// An idea marked by a friend must not be hinted at in anything sent to anyone.
	if body := strings.ToLower(m.Body); strings.Contains(body, "marked") || strings.Contains(body, "claim") || strings.Contains(body, "someone") {
		t.Fatalf("birthday email talks about gifts: %q", m.Body)
	}
	a.birthdays(now.AddDate(0, 0, 7))
	if count() != 2 || !strings.Contains(lastMail(t, a, "ben@example.com").Subject, "today") {
		t.Fatal("no email on the day")
	}
	ben.c.req("POST", "account/birthday-mail", map[string]bool{"Notify": false}, 200)
	a.birthdays(now.AddDate(0, 0, 7+365))
	if count() != 2 {
		t.Fatal("emailed after opting out")
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 10, "Day": 15, "Show": false}, 200)
	ben.c.req("POST", "account/birthday-mail", map[string]bool{"Notify": true}, 200)
	a.birthdays(now.AddDate(1, 0, 0))
	if count() != 2 {
		t.Fatal("a hidden birthday was emailed")
	}
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
	ana := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
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
	if a.state.Exchanges[id].Reminders[0] != (reminder{2, "month"}) {
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
	a.dueReminders(a.state.Exchanges[id], a.byToken(func(u *user) bool { return u.Email == "ben@example.com" }).ID, time.Now().AddDate(0, 0, 13))
	if got := sent(17); len(got) != 0 {
		t.Fatalf("3 days out: %v", got)
	}
	if got := sent(19); len(got) != 2 || !got["ana"] || !got["ben"] {
		t.Fatalf("1 day out: %v", got)
	}
}

// One caller can't spend the site's account-email allowance, however many accounts they hold, so resets keep
// working for everyone else.
func TestAccountEmailsAreLimitedPerCaller(t *testing.T) {
	a, _ := mailSetup(t)
	from := func(ip string) *http.Request {
		r := httptest.NewRequest("POST", "/api/reset/request", nil)
		r.RemoteAddr = ip + ":1234"
		return r
	}
	sent := 0
	for i := 0; i < 20; i++ {
		u := &user{ID: fmt.Sprint("u", i), Email: fmt.Sprintf("u%d@example.com", i)}
		for j := 0; j < 5; j++ {
			if a.accountMailAllowed(u, from("203.0.113.1"), time.Now()) {
				sent++
			}
		}
	}
	if sent != maxAccountMailPerIP {
		t.Fatalf("one caller sent %d account emails, want %d", sent, maxAccountMailPerIP)
	}
	if !a.accountMailAllowed(&user{ID: "v", Email: "v@example.com"}, from("198.51.100.2"), time.Now()) {
		t.Fatal("another caller's reset was refused")
	}
	// The site-wide ceiling still holds, and reaching it is a security event.
	a.hourCount = maxAccountMailPerHour
	before := a.watch.events["mail_site_limit"]
	if a.accountMailAllowed(&user{ID: "w", Email: "w@example.com"}, from("198.51.100.3"), time.Now()) {
		t.Fatal("site-wide ceiling ignored")
	}
	if a.watch.events["mail_site_limit"] != before+1 {
		t.Fatal("reaching the ceiling wasn't reported")
	}
}

func TestNotificationCapAndExistingEmailNotice(t *testing.T) {
	a := setup(t)
	o := &outbox{}
	a.mailer, a.base, a.from = o.send, "https://gifty.example", "Gifty <noreply@gifty.example>"
	(&client{t: t, a: a}).signup("ana")
	u := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
	u.Verified = true
	start := len(a.state.Outbox)
	for i := 0; i < 25; i++ {
		a.queue(u, true, "Reminder", "body")
	}
	if len(a.state.Outbox)-start != 20 {
		t.Fatalf("queued %d exchange emails, want 20", len(a.state.Outbox))
	}
	// Signing up again with a confirmed address is refused and tells the owner.
	before := len(a.state.Outbox)
	(&client{t: t, a: a}).req("POST", "signup", map[string]string{"Name": "x", "Email": "ana@example.com", "Password": "another long password"}, 400)
	if len(a.state.Outbox) != before+1 || !strings.Contains(a.state.Outbox[before].Subject, "tried to sign up") {
		t.Fatal("the account owner was not told")
	}
}
