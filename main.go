package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cpcf/gifty/internal/app"
)

func main() {
	path := os.Getenv("GIFTY_DATA")
	if path == "" {
		path = "data/gifty.json"
	}
	addr := os.Getenv("GIFTY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	// Cookies are Secure whenever the public address is https; the setting overrides that either way.
	secure := strings.HasPrefix(os.Getenv("GIFTY_BASE_URL"), "https://")
	if v := os.Getenv("GIFTY_SECURE_COOKIES"); v != "" {
		secure = v == "true"
	}
	a, err := app.New(app.Config{
		DataPath: path, Addr: addr, SecureCookies: secure,
		TrustProxy:  os.Getenv("GIFTY_TRUST_PROXY") == "true",
		AccessCode:  os.Getenv("GIFTY_ACCESS_CODE"),
		AdminEmails: strings.Split(os.Getenv("GIFTY_ADMINS"), ","),
		BaseURL:     os.Getenv("GIFTY_BASE_URL"), MailFrom: os.Getenv("GIFTY_MAIL_FROM"),
		SMTPHost: os.Getenv("GIFTY_SMTP_HOST"), SMTPPort: os.Getenv("GIFTY_SMTP_PORT"),
		SMTPUser: os.Getenv("GIFTY_SMTP_USER"), SMTPPassword: os.Getenv("GIFTY_SMTP_PASSWORD"),
		AlertEmail: os.Getenv("GIFTY_ALERT_EMAIL"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if !secure {
		log.Print("warning: cookies are not marked Secure; use this only on a trusted network or behind HTTPS")
	}
	a.StartBackground()
	s := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()
	fmt.Printf("Gifty is running at http://%s\n", addr)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
