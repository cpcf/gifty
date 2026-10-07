package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// shuffle puts s in a uniformly random order.
func shuffle[T any](s []T) {
	for i := len(s) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(err)
		}
		j := int(n.Int64())
		s[i], s[j] = s[j], s[i]
	}
}

// draw gives each of ids one recipient, never themselves and never someone they are kept apart from.
// It reports false when no such arrangement exists.
func draw(ids []string, apart [][2]string) (map[string]string, bool) {
	banned := map[[2]string]bool{}
	for _, p := range apart {
		banned[p] = true
		banned[[2]string{p[1], p[0]}] = true
	}
	n := len(ids)
	allowed := func(g, r int) bool { return g != r && !banned[[2]string{ids[g], ids[r]}] }
	// First make sure some arrangement exists, by matching givers to recipients with augmenting paths from a
	// random order. That arrangement is the fallback if shuffling is unlucky.
	adj := make([][]int, n)
	for g := range ids {
		for r := range ids {
			if allowed(g, r) {
				adj[g] = append(adj[g], r)
			}
		}
		shuffle(adj[g])
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	shuffle(order)
	giverOf := slices.Repeat([]int{-1}, n) // recipient index -> giver index
	var augment func(g int, seen []bool) bool
	augment = func(g int, seen []bool) bool {
		for _, r := range adj[g] {
			if seen[r] {
				continue
			}
			seen[r] = true
			if giverOf[r] < 0 || augment(giverOf[r], seen) {
				giverOf[r] = g
				return true
			}
		}
		return false
	}
	for _, g := range order {
		if !augment(g, make([]bool, n)) {
			return nil, false
		}
	}
	pick := make([]int, n) // giver index -> recipient index
	for r, g := range giverOf {
		pick[g] = r
	}
	// Rejection sampling yields a uniform random draw when the rules are loose, which they nearly always
	// are. Tight rules make it unlikely to succeed, so after a while the matching above stands.
	recipients := make([]int, n)
	for i := range recipients {
		recipients[i] = i
	}
	for try := 0; try < 400; try++ {
		shuffle(recipients)
		valid := true
		for g, r := range recipients {
			if !allowed(g, r) {
				valid = false
				break
			}
		}
		if valid {
			pick = recipients
			break
		}
	}
	out := make(map[string]string, n)
	for g, r := range pick {
		out[ids[g]] = ids[r]
	}
	return out, true
}

// dropMember takes someone out of an exchange that hasn't drawn yet, along with any pair they were in.
func (e *Exchange) dropMember(id string) {
	e.Members = slices.DeleteFunc(e.Members, func(m string) bool { return m == id })
	e.Apart = slices.DeleteFunc(e.Apart, func(p [2]string) bool { return p[0] == id || p[1] == id })
}

// setApart replaces the organiser's list of pairs who shouldn't draw each other.
func (a *App) setApart(e *Exchange, r *http.Request) error {
	var in struct{ Pairs [][2]string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if len(in.Pairs) > 200 {
		return bad("You can keep up to 200 pairs apart.")
	}
	out := [][2]string{}
	for _, p := range in.Pairs {
		if p[0] == p[1] {
			return bad("Choose two different people for each pair.")
		}
		if !slices.Contains(e.Members, p[0]) || !slices.Contains(e.Members, p[1]) {
			return bad("Choose people who have joined this exchange.")
		}
		if p[0] > p[1] {
			p[0], p[1] = p[1], p[0]
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	// Every pair narrows who someone can draw or be drawn by. With several pairs the organiser could work out, or
	// force, who buys for whom (their own giver included), so each person can be in only one.
	inPair := map[string]bool{}
	for _, p := range out {
		for _, id := range p {
			if inPair[id] {
				return bad("Each person can be kept apart from only one other person.")
			}
			inPair[id] = true
		}
	}
	if len(e.Members) >= 3 {
		if _, ok := draw(e.Members, out); !ok {
			return bad("With those pairs kept apart, there is no way to give everyone someone to buy for. Keep fewer pairs apart.")
		}
	}
	e.Apart = out
	return nil
}

// revealable is whether the organiser may show who bought for whom: from the day before the exchange (a day of
// leeway for time zones).
func (e *Exchange) revealable(now time.Time) bool {
	return len(e.Assignments) > 0 && !e.Revealed && now.UTC().AddDate(0, 0, 1).Format("2006-01-02") >= e.Date
}

func (a *App) reveal(e *Exchange) error {
	if len(e.Assignments) == 0 {
		return bad("Entries aren’t closed yet.")
	}
	if e.Revealed {
		return bad("Names are already revealed.")
	}
	if !e.revealable(time.Now()) {
		return bad("Names can be revealed from the day before the exchange.")
	}
	e.Revealed = true
	return nil
}

const (
	maxMessage  = 500 // characters
	maxMessages = 40  // from each side of a conversation
)

func threadView(thread []Message, asGiver bool) []map[string]any {
	out := []map[string]any{}
	for _, m := range thread {
		out = append(out, map[string]any{"mine": m.FromGiver == asGiver, "text": m.Text, "at": m.At})
	}
	return out
}

// exchangeExtras adds what depends on the draw, the conversations and the reveal. A person sees only the two
// conversations they are in, and the pairs kept apart only if they organise the exchange.
func (a *App) exchangeExtras(v map[string]any, e *Exchange, u *User) {
	a.kindExtras(v, e, u)
	drawn := e.locked()
	if u.ID == e.Owner && !drawn && e.Kind == "" {
		apart := [][2]string{}
		v["apart"] = append(apart, e.Apart...)
	}
	if drawn {
		if e.Assignments[u.ID] != "" {
			v["toRecipient"] = threadView(e.Threads[u.ID], true)
		}
		for giver, to := range e.Assignments {
			if to == u.ID {
				v["fromGiver"] = threadView(e.Threads[giver], false)
			}
		}
	}
	if u.ID == e.Owner && e.revealable(time.Now()) {
		v["canReveal"] = true
	}
	if e.Revealed {
		v["revealed"] = true
		pairs := []map[string]string{}
		for _, id := range e.Members {
			if to := e.Assignments[id]; to != "" {
				pairs = append(pairs, map[string]string{"giver": id, "recipient": to})
			}
		}
		v["pairs"] = pairs
	}
}

// say adds a line to the conversation between u and the person on the other side of the draw. To is
// "recipient" (u is the giver, writing to who they buy for) or "giver" (u is the recipient, answering).
// Neither side learns who the other is from this.
func (a *App) say(e *Exchange, u *User, r *http.Request) error {
	if e.Kind != "" {
		return bad("This kind of exchange has no messages.")
	}
	if len(e.Assignments) == 0 {
		return bad("Entries aren’t closed yet.")
	}
	if e.Archived {
		return bad("This exchange is archived.")
	}
	var in struct{ To, Text string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || utf8.RuneCountInString(text) > maxMessage {
		return bad(fmt.Sprintf("Write a message of up to %d characters.", maxMessage))
	}
	var giver, other string
	switch in.To {
	case "recipient":
		giver, other = u.ID, e.Assignments[u.ID]
	case "giver":
		other = ""
		for g, to := range e.Assignments {
			if to == u.ID {
				giver, other = g, g
			}
		}
	default:
		return bad("Choose who the message is for.")
	}
	if giver == "" || other == "" {
		return bad("There is nobody to send that to.")
	}
	thread := e.Threads[giver]
	fromGiver := in.To == "recipient"
	// The limit is per side, so one person can't use up the other's replies.
	if sent := len(slices.DeleteFunc(slices.Clone(thread), func(m Message) bool { return m.FromGiver != fromGiver })); sent >= maxMessages {
		return bad("You’ve sent as many messages as this conversation allows.")
	}
	// Email only when the conversation changes hands, so two messages in a row are one email.
	notify := len(thread) == 0 || thread[len(thread)-1].FromGiver != fromGiver
	if e.Threads == nil {
		e.Threads = map[string][]Message{}
	}
	e.Threads[giver] = append(thread, Message{FromGiver: fromGiver, Text: text, At: time.Now().UTC()})
	if notify {
		who := "The person you’re buying for in"
		if fromGiver {
			who = "Someone who is buying for you in"
		}
		a.queue(a.state.Users[other], true, "A message about "+e.Name, fmt.Sprintf("%s %s has sent you a message. Gifty keeps both of you anonymous to each other.\n\nSign in to read it and reply:\n%s/#exchange/%s", who, e.Name, a.base, e.ID))
	}
	return nil
}

// claim records that u, who is buying for the owner of an idea, is getting it, so nobody else buying for the
// same person in another exchange gets it too. The owner is never told.
func (a *App) claim(e *Exchange, u *User, r *http.Request) error {
	if e.Kind != "" {
		return bad("Mark what you’re getting from your friend’s page instead.")
	}
	to := a.state.Users[e.Assignments[u.ID]]
	if to == nil {
		return bad("Entries aren’t closed yet.")
	}
	var in struct {
		Wish  string
		Claim bool
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	i := slices.IndexFunc(to.Wishes, func(w Wish) bool { return w.ID == in.Wish })
	if i < 0 {
		return problem{404, "Gift idea not found."}
	}
	w := &to.Wishes[i]
	mine := w.ClaimedBy == u.ID
	// Letting go of your own claim always works, whatever has happened to the idea or the exchange since.
	if !in.Claim {
		if mine {
			w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
		}
		return nil
	}
	if e.Archived {
		return bad("This exchange is archived.")
	}
	if !w.forExchange(e.ID) || (w.Status != "" && !mine) {
		return problem{404, "Gift idea not found."}
	}
	switch {
	case w.ClaimedBy != "" && !mine:
		return problem{409, "Someone else is already getting this one."}
	case w.Status != "":
		return bad("They’ve sorted this one out already.")
	default:
		w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = u.ID, e.ID, time.Now().UTC()
	}
	return nil
}

// clearClaims releases every idea claim that match selects.
func (a *App) clearClaims(match func(*Wish) bool) {
	for _, v := range a.state.Users {
		for i := range v.Wishes {
			if w := &v.Wishes[i]; w.ClaimedBy != "" && match(w) {
				w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
			}
		}
	}
}

// setStatus marks one of u's ideas as sorted ("got", "dropped") or puts it back on the list (""). Sorted ideas
// are hidden from the people buying for u.
func (a *App) setStatus(u *User, id string, r *http.Request) error {
	var in struct{ Status string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !slices.Contains([]string{"", "got", "dropped"}, in.Status) {
		return bad("Choose whether you’ve got it or don’t want it.")
	}
	i := slices.IndexFunc(u.Wishes, func(w Wish) bool { return w.ID == id })
	if i < 0 {
		return problem{404, "Gift idea not found."}
	}
	u.Wishes[i].Status = in.Status
	return nil
}

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
func (a *App) serveCalendar(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
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
