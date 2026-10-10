package app

import (
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"
)

// Reminder is one reminder, N days, weeks or months before the exchange date.
type reminder struct {
	N    int    `json:"n"`
	Unit string `json:"unit"`
}

// reminderLimits are the largest N for each unit; a day reminder may be 0, on the day itself.
var reminderLimits = map[string]int{"day": 365, "week": 52, "month": 12}

func (r reminder) key() string { return fmt.Sprintf("%d%s", r.N, r.Unit) }

// on is the date the reminder is due. Months are calendar months, so one month
// before 18 December is 18 November.
func (r reminder) on(date time.Time) time.Time {
	switch r.Unit {
	case "week":
		return date.AddDate(0, 0, -7*r.N)
	case "month":
		return date.AddDate(0, -r.N, 0)
	}
	return date.AddDate(0, 0, -r.N)
}

var defaultReminders = []reminder{{1, "week"}, {1, "day"}}

// defaultReminders is the organiser's schedule. Exchanges saved before
// schedules existed have none stored and use a week and a day before.
func (e *exchange) defaultReminders() []reminder {
	if e.Reminders == nil {
		return defaultReminders
	}
	return e.Reminders
}

// reminders is the schedule a member gets and where it comes from: their
// choice for this exchange, else their account default, else the organiser's.
func (a *application) reminders(e *exchange, id string) ([]reminder, string) {
	if rs, ok := e.MyReminders[id]; ok {
		return rs, "exchange"
	}
	if u := a.state.Users[id]; u != nil && u.OwnReminders {
		return u.Reminders, "account"
	}
	return e.defaultReminders(), "organiser"
}

// parseReminders checks each reminder, drops duplicates and orders them from
// furthest ahead to nearest.
func parseReminders(in []reminder) ([]reminder, error) {
	if len(in) > 8 {
		return nil, bad("Add up to 8 reminders.")
	}
	out := []reminder{}
	for _, r := range in {
		limit, ok := reminderLimits[r.Unit]
		if !ok || r.N < 0 || r.N > limit || (r.N == 0 && r.Unit != "day") {
			return nil, bad("Use up to 365 days, 52 weeks or 12 months before the exchange.")
		}
		if !slices.ContainsFunc(out, func(o reminder) bool { return o == r }) {
			out = append(out, r)
		}
	}
	far := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	slices.SortStableFunc(out, func(x, y reminder) int { return x.on(far).Compare(y.on(far)) })
	return out, nil
}

// dueReminders marks a member's reminders whose date has arrived as sent and
// reports whether any were new. Calling it after a draw or a schedule change
// skips reminders that are already late instead of sending a burst of them.
func (a *application) dueReminders(e *exchange, id string, now time.Time) bool {
	date, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		return false
	}
	today, due := now.UTC().Truncate(24*time.Hour), false
	schedule, _ := a.reminders(e, id)
	for _, r := range schedule {
		if !today.Before(r.on(date)) && !slices.Contains(e.Reminded[id], r.key()) {
			if e.Reminded == nil {
				e.Reminded = map[string][]string{}
			}
			e.Reminded[id] = append(e.Reminded[id], r.key())
			due = true
		}
	}
	return due
}

func (a *application) mailOn() bool { return a.mailer != nil }

// queue adds a message to the outbox. Notifications go only to confirmed
// addresses that haven't opted out; account emails (confirm, reset) always go.
func (a *application) queue(u *user, notification bool, subject, body string) {
	if !a.mailOn() || (notification && (!u.Verified || u.NoEmail)) {
		return
	}
	if notification {
		if day := time.Now().UTC().Format("2006-01-02"); u.NotifyDay != day {
			u.NotifyDay, u.NotifyCount = day, 0
		}
		if u.NotifyCount >= 20 {
			log.Printf("exchange email to %s not sent: daily limit reached", u.Email)
			return
		}
		u.NotifyCount++
	}
	m := queuedMail{ID: token(), To: u.Email, Subject: subject, Next: time.Now()}
	if notification {
		m.Unsub = a.unsubToken(u)
		m.Body = body + "\n\n--\nYou’re getting this because you have a Gifty account.\nStop these emails: " + a.base + "/#unsubscribe/" + m.Unsub + "\n"
	} else {
		m.Body = body + "\n\n--\nGifty, " + a.base + "\n"
		// Only account emails count towards the confirmation resend cooldown.
		u.LastMail = time.Now()
	}
	a.state.Outbox = append(a.state.Outbox, m)
	// The request holds the lock until its save finishes, so the woken loop sends only committed mail.
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Account emails (confirmations and resets) are capped at 5 per person per day, so Gifty can't be used to
// flood an inbox, and at 10 per caller per 15 minutes, so one person can't spend the site's sending allowance on
// throwaway accounts. The site-wide hourly ceiling is far above that and only there to protect the mail account;
// reaching it is reported as a security event, since it means somebody is working around the other two.
const (
	maxAccountMailPerIP   = 10
	maxAccountMailPerHour = 1000
)

func (a *application) accountMailAllowed(u *user, r *http.Request, now time.Time) bool {
	if !a.mailOn() {
		return false
	}
	if day := now.UTC().Format("2006-01-02"); u.MailDay != day {
		u.MailDay, u.MailCount = day, 0
	}
	if now.Sub(a.hourStart) > time.Hour {
		a.hourStart, a.hourCount = now, 0
	}
	if u.MailCount >= 5 {
		log.Printf("account email to %s not sent: daily limit reached", u.Email)
		return false
	}
	if !a.allow("mail|"+a.clientIP(r), maxAccountMailPerIP) {
		a.sec(r, "mail_rate_limited")
		return false
	}
	if a.hourCount >= maxAccountMailPerHour {
		a.sec(r, "mail_site_limit")
		return false
	}
	u.MailCount++
	a.hourCount++
	return true
}

func (a *application) sendVerify(u *user, r *http.Request) bool {
	if !a.accountMailAllowed(u, r, time.Now()) {
		return false
	}
	t := token()
	u.VerifyHash, u.VerifyExpires = digest(t), time.Now().Add(48*time.Hour)
	a.queue(u, false, "Confirm your email address for Gifty", fmt.Sprintf("Hello %s,\n\nConfirm that this is your email address by opening this link:\n\n%s/#verify/%s\n\nThe link works for 48 hours. Gifty only sends exchange emails to confirmed addresses.\n\nIf you didn’t create a Gifty account, ignore this email.", u.Name, a.base, t))
	return true
}

func (a *application) sendReset(u *user, r *http.Request) bool {
	if !a.accountMailAllowed(u, r, time.Now()) {
		return false
	}
	t := token()
	u.ResetHash, u.ResetExpires = digest(t), time.Now().Add(time.Hour)
	a.queue(u, false, "Reset your Gifty password", fmt.Sprintf("Hello %s,\n\nSomeone asked to reset the password for this Gifty account. To choose a new password, open this link:\n\n%s/#reset/%s\n\nThe link works for one hour. If you didn’t ask for this, ignore this email and your password stays the same.", u.Name, a.base, t))
	return true
}

func money(e *exchange) string {
	sym := map[string]string{"GBP": "£", "USD": "$", "EUR": "€", "CAD": "CA$", "AUD": "A$"}[e.Currency]
	return sym + e.Budget
}

func longDate(s string) string {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return d.Format("Monday 2 January 2006")
}

// drawn emails every member once the organiser has locked the exchange in. The recipient's name is never put in an email:
// inboxes are shared, previewed and forwarded, so it stays behind sign-in.
func (a *application) drawn(e *exchange) {
	organiser := a.state.Users[e.Owner].Name
	if e.Kind == kindElephant {
		for _, id := range e.Members {
			a.queue(a.state.Users[id], true, "Draw a number: "+e.Name, fmt.Sprintf("%s has closed entries for %s. Everyone is in, so it’s time to draw a number for the order of picking.\n\nSign in and draw yours:\n%s/#exchange/%s\n\nDate: %s\nSpending limit per gift: %s", organiser, e.Name, a.base, e.ID, longDate(e.Date), money(e)))
		}
		for _, id := range e.Members {
			a.dueReminders(e, id, time.Now())
		}
		return
	}
	for _, id := range e.Members {
		a.queue(a.state.Users[id], true, "Draw a name: "+e.Name, fmt.Sprintf("%s has closed entries for %s. Everyone is in, so it’s time to draw a name.\n\nSign in and draw a name to see who you’re buying for and their wish list:\n%s/#exchange/%s\n\nDate: %s\nSpending limit: %s", organiser, e.Name, a.base, e.ID, longDate(e.Date), money(e)))
	}
	// Reminders whose day has already arrived are covered by this email.
	for _, id := range e.Members {
		a.dueReminders(e, id, time.Now())
	}
}

func daysUntil(date string, now time.Time) int {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return -1
	}
	return int(d.Sub(now.UTC().Truncate(24*time.Hour)).Hours() / 24)
}

// remind queues due reminders and reports whether anything changed.
func (a *application) remind(now time.Time) bool {
	changed := false
	for _, e := range a.state.Exchanges {
		if e.Archived || !e.live() {
			continue
		}
		days := daysUntil(e.Date, now)
		if days < 0 {
			continue
		}
		when := fmt.Sprintf("in %d days", days)
		if days == 1 {
			when = "tomorrow"
		} else if days == 0 {
			when = "today"
		}
		for _, id := range e.Members {
			if slices.Contains(e.Ready, id) || !a.dueReminders(e, id, now) {
				continue
			}
			changed = true
			if e.Kind != "" {
				a.queue(a.state.Users[id], true, fmt.Sprintf("Reminder: %s is %s", e.Name, when), a.kindReminder(e, when))
				continue
			}
			a.queue(a.state.Users[id], true, fmt.Sprintf("Reminder: %s is %s", e.Name, when), fmt.Sprintf("%s is %s, on %s.\n\nSpending limit: %s\n\nSign in to see who you’re buying for and their wish list:\n%s/#exchange/%s\n\nIs their gift already ready? Press “Mark gift as ready” on the exchange page to stop these reminders. You can also change when you get reminders there.", e.Name, when, longDate(e.Date), money(e), a.base, e.ID))
		}
	}
	return changed
}

// kindReminder is the reminder for an exchange that isn't a Secret Santa. Like every email it never names who a gift is for.
func (a *application) kindReminder(e *exchange, when string) string {
	if e.Kind == kindGroup {
		return fmt.Sprintf("%s is %s, on %s.\n\nSpending limit: %s\n\nSign in to see the list and what has been taken:\n%s/#exchange/%s\n\nYou can change when you get reminders on the exchange page.", e.Name, when, longDate(e.Date), money(e), a.base, e.ID)
	}
	return fmt.Sprintf("%s is %s, on %s.\n\nBring one wrapped gift, up to %s.\n\nSign in to see your place in the order:\n%s/#exchange/%s\n\nIs your gift already wrapped? Press “Mark gift as ready” on the exchange page to stop these reminders. You can also change when you get reminders there.", e.Name, when, longDate(e.Date), money(e), a.base, e.ID)
}

func (a *application) handleAccountReminders(u *user, r *http.Request) (any, error) {
	// Default true goes back to using each organiser's schedule.
	var in struct {
		Default   bool
		Reminders []reminder
	}
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	days, err := parseReminders(in.Reminders)
	if err != nil {
		return nil, err
	}
	u.OwnReminders, u.Reminders = !in.Default, days
	if in.Default {
		u.Reminders = nil
	}
	for _, e := range a.state.Exchanges {
		if e.live() && slices.Contains(e.Members, u.ID) {
			a.dueReminders(e, u.ID, time.Now())
		}
	}
	return a.publicUser(u), nil
}
