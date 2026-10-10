package app

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
	"strings"
	"time"
)

// Mail is a queued message. The outbox lives in the data file, so a message is
// committed in the same save as the change that caused it and survives restarts.
type queuedMail struct {
	ID       string
	To       string
	Subject  string
	Body     string
	Unsub    string
	Attempts int
	Next     time.Time
}

// tick queues reminders, then sends what is due outside the lock so a slow
// mail server never blocks requests. Failed messages back off and retry.
func (a *application) tick(now time.Time) {
	a.mu.Lock()
	changed := a.remind(now)
	if a.birthdays(now) {
		changed = true
	}
	if changed {
		if err := a.save(); err != nil {
			log.Printf("saving reminders: %v", err)
		}
	}
	var due []queuedMail
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
	var keep []queuedMail
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

func (a *application) mailLoop() {
	for {
		a.tick(time.Now())
		select {
		case <-a.wake:
		case <-time.After(30 * time.Second):
		}
	}
}

func (a *application) message(m queuedMail) []byte {
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
