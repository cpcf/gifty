package app

import (
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// icsText escapes text for an iCalendar value.
var icsText = func(s string) string {
	s = strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\r\n", "\\n", "\n", "\\n", "\r", "").Replace(s)
	// Other control characters have no place in a calendar value.
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// icsLine writes one content line, folded to 75 octets as the format requires.
func icsLine(b *strings.Builder, s string) {
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		b.WriteString(s[:cut] + "\r\n ")
		s, limit = s[cut:], 74
	}
	b.WriteString(s + "\r\n")
}

// serveCalendar sends a member an all-day calendar event for the exchange.
func (a *application) serveCalendar(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	// Built under the lock, sent after it, like the API.
	out := &buffered{header: w.Header()}
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.calendar(out, r)
	}()
	out.flush(w)
}

func (a *application) calendar(w http.ResponseWriter, r *http.Request) {
	if !a.allow("read|"+a.clientIP(r), maxReads) {
		a.sec(r, "read_rate_limited")
		http.Error(w, "Too many requests. Try again in a few minutes.", 429)
		return
	}
	u := a.user(r)
	if u == nil {
		http.Error(w, "Sign in to continue.", 401)
		return
	}
	if strings.TrimPrefix(r.URL.Path, "/calendar/") == "birthdays" {
		a.serveBirthdays(w, u)
		return
	}
	e := a.state.Exchanges[strings.TrimPrefix(r.URL.Path, "/calendar/")]
	if e == nil || !slices.Contains(e.Members, u.ID) {
		http.NotFound(w, r)
		return
	}
	day, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	desc := "Gift exchange. Spending limit: " + money(e) + "."
	if e.Note != "" {
		desc += "\n\n" + e.Note
	}
	var b strings.Builder
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Gifty//EN", "CALSCALE:GREGORIAN", "BEGIN:VEVENT",
		"UID:" + e.ID + "@gifty", "DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z"),
		"DTSTART;VALUE=DATE:" + day.Format("20060102"), "DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102"),
		"SUMMARY:" + icsText(e.Name), "DESCRIPTION:" + icsText(desc)} {
		icsLine(&b, l)
	}
	if a.base != "" {
		icsLine(&b, "URL:"+a.base+"/#exchange/"+e.ID)
	}
	icsLine(&b, "END:VEVENT")
	icsLine(&b, "END:VCALENDAR")
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, e.Name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "exchange"
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gifty-`+name[:min(len(name), 40)]+`.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write([]byte(b.String()))
}
