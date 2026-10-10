package app

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
)

// Config contains startup settings. Environment parsing belongs to the executable.
type Config struct {
	DataPath      string
	Addr          string
	SecureCookies bool
	TrustProxy    bool
	AccessCode    string
	AdminEmails   []string
	BaseURL       string
	MailFrom      string
	SMTPHost      string
	SMTPPort      string
	SMTPUser      string
	SMTPPassword  string
	AlertEmail    string
}

// New loads persisted state and configures the application before it serves requests.
func New(c Config) (*application, error) {
	a, err := openApp(c.DataPath)
	if err != nil {
		return nil, err
	}
	a.secure, a.trustProxy = c.SecureCookies, c.TrustProxy
	if code := strings.TrimSpace(c.AccessCode); code != "" {
		a.access = a.gate(code)
	}
	a.admins = map[string]bool{}
	for _, v := range c.AdminEmails {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			a.admins[v] = true
		}
	}
	if c.SMTPHost != "" {
		a.base, a.from = strings.TrimSuffix(c.BaseURL, "/"), c.MailFrom
		if c.SMTPHost == "log" {
			if a.base == "" {
				a.base = "http://" + c.Addr
			}
			if a.from == "" {
				a.from = "Gifty <gifty@localhost>"
			}
			a.mailer = logSender
		} else {
			port := c.SMTPPort
			if port == "" {
				port = "587"
			}
			if a.base == "" || a.from == "" {
				return nil, fmt.Errorf("GIFTY_BASE_URL and GIFTY_MAIL_FROM are required when GIFTY_SMTP_HOST is set")
			}
			if _, err := mail.ParseAddress(a.from); err != nil {
				return nil, fmt.Errorf("GIFTY_MAIL_FROM: %w", err)
			}
			a.mailer = smtpSender(c.SMTPHost, port, c.SMTPUser, c.SMTPPassword, a.from)
		}
		a.wake = make(chan struct{}, 1)
		if to := strings.TrimSpace(c.AlertEmail); to != "" {
			if addr, err := mail.ParseAddress(to); err != nil || addr.Address != to {
				return nil, fmt.Errorf("GIFTY_ALERT_EMAIL must be a plain email address")
			}
			a.alertTo = to
		}
	}
	return a, nil
}

// Handler is the complete HTTP surface, including the shared security headers.
func (a *application) Handler() http.Handler { return a.handler() }

// StartBackground starts the existing mail and security-monitoring workers after configuration.
func (a *application) StartBackground() {
	if !a.mailOn() {
		return
	}
	go a.mailLoop()
	if a.alertTo != "" {
		go a.alertLoop()
	}
	fmt.Printf("Email is on, sending from %s\n", a.from)
}
