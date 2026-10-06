package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Mail is a queued message. The outbox lives in the data file, so a message is
// committed in the same save as the change that caused it and survives restarts.
type Mail struct {
	ID       string
	To       string
	Subject  string
	Body     string
	Unsub    string
	Attempts int
	Next     time.Time
}

// Reminder is one reminder, N days, weeks or months before the exchange date.
type Reminder struct {
	N    int    `json:"n"`
	Unit string `json:"unit"`
}

// reminderLimits are the largest N for each unit; a day reminder may be 0, on the day itself.
var reminderLimits = map[string]int{"day": 365, "week": 52, "month": 12}

func (r Reminder) key() string { return fmt.Sprintf("%d%s", r.N, r.Unit) }

// on is the date the reminder is due. Months are calendar months, so one month
// before 18 December is 18 November.
func (r Reminder) on(date time.Time) time.Time {
	switch r.Unit {
	case "week":
		return date.AddDate(0, 0, -7*r.N)
	case "month":
		return date.AddDate(0, -r.N, 0)
	}
	return date.AddDate(0, 0, -r.N)
}

var defaultReminders = []Reminder{{1, "week"}, {1, "day"}}

// defaultReminders is the organiser's schedule. Exchanges saved before
// schedules existed have none stored and use a week and a day before.
func (e *Exchange) defaultReminders() []Reminder {
	if e.Reminders == nil {
		return defaultReminders
	}
	return e.Reminders
}

// reminders is the schedule a member gets and where it comes from: their
// choice for this exchange, else their account default, else the organiser's.
func (a *App) reminders(e *Exchange, id string) ([]Reminder, string) {
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
func parseReminders(in []Reminder) ([]Reminder, error) {
	if len(in) > 8 {
		return nil, bad("Add up to 8 reminders.")
	}
	out := []Reminder{}
	for _, r := range in {
		limit, ok := reminderLimits[r.Unit]
		if !ok || r.N < 0 || r.N > limit || (r.N == 0 && r.Unit != "day") {
			return nil, bad("Use up to 365 days, 52 weeks or 12 months before the exchange.")
		}
		if !slices.ContainsFunc(out, func(o Reminder) bool { return o == r }) {
			out = append(out, r)
		}
	}
	far := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	slices.SortStableFunc(out, func(x, y Reminder) int { return x.on(far).Compare(y.on(far)) })
	return out, nil
}

// dueReminders marks a member's reminders whose date has arrived as sent and
// reports whether any were new. Calling it after a draw or a schedule change
// skips reminders that are already late instead of sending a burst of them.
func (a *App) dueReminders(e *Exchange, id string, now time.Time) bool {
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

func (a *App) mailOn() bool { return a.mailer != nil }

// queue adds a message to the outbox. Notifications go only to confirmed
// addresses that haven't opted out; account emails (confirm, reset) always go.
func (a *App) queue(u *User, notification bool, subject, body string) {
	if !a.mailOn() || (notification && (!u.Verified || u.NoEmail)) {
		return
	}
	m := Mail{ID: token(), To: u.Email, Subject: subject, Next: time.Now()}
	if notification {
		if u.Unsub == "" {
			u.Unsub = token()
		}
		m.Unsub = u.Unsub
		m.Body = body + "\n\n--\nYou’re getting this because you’re taking part in a gift exchange on Gifty.\nStop these emails: " + a.base + "/#unsubscribe/" + u.Unsub + "\n"
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

// accountMailAllowed caps confirmation and reset emails at 5 per person per
// day and 100 an hour site-wide, so Gifty can't be used to flood an inbox.
func (a *App) accountMailAllowed(u *User, now time.Time) bool {
	if !a.mailOn() {
		return false
	}
	if day := now.UTC().Format("2006-01-02"); u.MailDay != day {
		u.MailDay, u.MailCount = day, 0
	}
	if now.Sub(a.hourStart) > time.Hour {
		a.hourStart, a.hourCount = now, 0
	}
	if u.MailCount >= 5 || a.hourCount >= 100 {
		log.Printf("account email to %s not sent: limit reached", u.Email)
		return false
	}
	u.MailCount++
	a.hourCount++
	return true
}
func (a *App) sendVerify(u *User) bool {
	if !a.accountMailAllowed(u, time.Now()) {
		return false
	}
	t := token()
	u.VerifyHash, u.VerifyExpires = digest(t), time.Now().Add(48*time.Hour)
	a.queue(u, false, "Confirm your email address for Gifty", fmt.Sprintf("Hello %s,\n\nConfirm that this is your email address by opening this link:\n\n%s/#verify/%s\n\nThe link works for 48 hours. Gifty only sends exchange emails to confirmed addresses.\n\nIf you didn’t create a Gifty account, ignore this email.", u.Name, a.base, t))
	return true
}
func (a *App) sendReset(u *User) bool {
	if !a.accountMailAllowed(u, time.Now()) {
		return false
	}
	t := token()
	u.ResetHash, u.ResetExpires = digest(t), time.Now().Add(time.Hour)
	a.queue(u, false, "Reset your Gifty password", fmt.Sprintf("Hello %s,\n\nSomeone asked to reset the password for this Gifty account. To choose a new password, open this link:\n\n%s/#reset/%s\n\nThe link works for one hour. If you didn’t ask for this, ignore this email and your password stays the same.", u.Name, a.base, t))
	return true
}

func money(e *Exchange) string {
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

// drawn emails every member. The recipient's name is never put in an email:
// inboxes are shared, previewed and forwarded, so it stays behind sign-in.
func (a *App) drawn(e *Exchange) {
	organiser := a.state.Users[e.Owner].Name
	for _, id := range e.Members {
		a.queue(a.state.Users[id], true, "Names have been drawn: "+e.Name, fmt.Sprintf("%s has drawn names for %s.\n\nSign in to see who you’re buying for and their wish list:\n%s/#exchange/%s\n\nDate: %s\nSpending limit: %s", organiser, e.Name, a.base, e.ID, longDate(e.Date), money(e)))
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
func (a *App) remind(now time.Time) bool {
	changed := false
	for _, e := range a.state.Exchanges {
		if e.Archived || len(e.Assignments) == 0 {
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
			if slices.Contains(e.Done, id) || !a.dueReminders(e, id, now) {
				continue
			}
			changed = true
			a.queue(a.state.Users[id], true, fmt.Sprintf("Reminder: %s is %s", e.Name, when), fmt.Sprintf("%s is %s, on %s.\n\nSpending limit: %s\n\nSign in to see who you’re buying for and their wish list:\n%s/#exchange/%s\n\nAlready got your gift? Tick “I’ve got my gift” on the exchange page and you won’t get any more reminders for it. You can also change when you get reminders there.", e.Name, when, longDate(e.Date), money(e), a.base, e.ID))
		}
	}
	return changed
}

// tick queues reminders, then sends what is due outside the lock so a slow
// mail server never blocks requests. Failed messages back off and retry.
func (a *App) tick(now time.Time) {
	a.mu.Lock()
	if a.remind(now) {
		if err := a.save(); err != nil {
			log.Printf("saving reminders: %v", err)
		}
	}
	var due []Mail
	for _, m := range a.state.Outbox {
		if !now.Before(m.Next) && len(due) < 20 {
			due = append(due, m)
		}
	}
	a.mu.Unlock()
	if len(due) == 0 {
		return
	}
	results := map[string]error{}
	for _, m := range due {
		results[m.ID] = a.mailer(m.To, a.message(m))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var keep []Mail
	for _, m := range a.state.Outbox {
		err, tried := results[m.ID]
		if tried && err == nil {
			continue
		}
		if tried {
			m.Attempts++
			if m.Attempts >= 8 {
				log.Printf("giving up on email to %s: %v", m.To, err)
				continue
			}
			m.Next = now.Add(time.Minute << m.Attempts)
			log.Printf("email to %s failed (attempt %d): %v", m.To, m.Attempts, err)
		}
		keep = append(keep, m)
	}
	a.state.Outbox = keep
	if err := a.save(); err != nil {
		log.Printf("saving outbox: %v", err)
	}
}
func (a *App) mailLoop() {
	for {
		a.tick(time.Now())
		select {
		case <-a.wake:
		case <-time.After(30 * time.Second):
		}
	}
}

func (a *App) message(m Mail) []byte {
	host := "gifty"
	if u, err := url.Parse(a.base); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	clean := strings.NewReplacer("\r", " ", "\n", " ")
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n", a.from, m.To, mime.QEncoding.Encode("utf-8", clean.Replace(m.Subject)), time.Now().Format(time.RFC1123Z), m.ID, host)
	if m.Unsub != "" {
		fmt.Fprintf(&b, "List-Unsubscribe: <%s/api/unsubscribe/%s>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n", a.base, m.Unsub)
	}
	b.WriteString("\r\n")
	q := quotedprintable.NewWriter(&b)
	q.Write([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n")))
	q.Close()
	return b.Bytes()
}

// smtpSender sends through an SMTP server that supports STARTTLS, such as
// Amazon SES on port 587. It refuses to send credentials without TLS.
func smtpSender(host, port, user, pass, from string) func(string, []byte) error {
	sender, _ := mail.ParseAddress(from)
	return func(to string, msg []byte) error {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 20*time.Second)
		if err != nil {
			return err
		}
		conn.SetDeadline(time.Now().Add(time.Minute))
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return err
		}
		defer c.Close()
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("mail server does not offer STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
		if user != "" {
			if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
				return err
			}
		}
		if err := c.Mail(sender.Address); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}
}
func logSender(to string, msg []byte) error {
	log.Printf("email to %s:\n%s", to, msg)
	return nil
}
