package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestBirthdayCalendarFileWithoutSomeBirthdays(t *testing.T) {
	a := setup(t)
	a.base = "https://gifty.example"
	p := people(t, a, "Ana", "Ben", "Cy", "Dee")
	ana, ben, cy, dee := p["Ana"], p["Ben"], p["Cy"], p["Dee"]
	for _, f := range []*person{ana, cy, dee} {
		makeFriends(t, ben, f)
	}
	ana.c.req("POST", "birthday", map[string]any{"Month": 3, "Day": 14, "Show": true}, 200)
	cy.c.req("POST", "birthday", map[string]any{"Month": 7, "Day": 4, "Show": false}, 200) // hidden
	// Dee has no birthday at all.
	body := strings.ReplaceAll(ben.c.get("/calendar/birthdays", 200).Body.String(), "\r\n ", "") // lines are folded at 75 octets
	if n := strings.Count(body, "BEGIN:VEVENT"); n != 1 {
		t.Fatalf("calendar file has %d events, want 1:\n%s", n, body)
	}
	for _, want := range []string{"URL:https://gifty.example/#friends/" + ana.id, "TRANSP:TRANSPARENT", "RRULE:FREQ=YEARLY\r\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("calendar file lacks %q", want)
		}
	}
	if strings.Contains(body, "BYMONTH") {
		t.Fatal("an ordinary date got the leap-day rule")
	}
	if !strings.HasSuffix(body, "END:VCALENDAR\r\n") {
		t.Fatal("calendar file isn't terminated")
	}
	w := ben.c.get("/calendar/birthdays", 200)
	if w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "gifty-birthdays.ics") {
		t.Fatalf("headers = %v", w.Header())
	}
	// Someone with no friends gets an empty but valid file.
	if empty := people(t, a, "Eve")["Eve"].c.get("/calendar/birthdays", 200).Body.String(); strings.Contains(empty, "VEVENT") || !strings.Contains(empty, "BEGIN:VCALENDAR") {
		t.Fatalf("empty calendar = %q", empty)
	}
}

func TestBirthdayCalendarFile(t *testing.T) {
	a := setup(t)
	p := people(t, a, "Ana", "Ben", "Cy")
	ana, ben, cy := p["Ana"], p["Ben"], p["Cy"]
	makeFriends(t, ben, ana)
	makeFriends(t, cy, ana)
	ana.c.req("POST", "birthday", map[string]any{"Month": 2, "Day": 29, "Show": true}, 200)
	cy.c.req("POST", "birthday", map[string]any{"Month": 7, "Day": 4, "Show": false}, 200)
	body := ben.c.get("/calendar/birthdays", 200).Body.String()
	for _, want := range []string{"BEGIN:VCALENDAR", "SUMMARY:Ana’s birthday", "RRULE:FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=-1", "UID:birthday-" + ana.id + "@gifty"} {
		if !strings.Contains(body, want) {
			t.Errorf("calendar lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Cy") {
		t.Fatal("a hidden birthday is in a friend's calendar file")
	}
	r := httptest.NewRequest("GET", "/calendar/birthdays", nil)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("signed out calendar = %d", w.Code)
	}
}
