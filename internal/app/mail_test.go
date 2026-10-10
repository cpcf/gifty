package app

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	u := a.byToken(func(u *user) bool { return u.Email == "ana@example.com" })
	u.Verified = true
	a.queue(u, true, "Test", "Body")
	m := lastMail(t, a, "ana@example.com")
	msg := string(a.message(m))
	if !strings.Contains(msg, "List-Unsubscribe: <https://gifty.example/api/unsubscribe/"+a.unsubToken(u)+">") || !strings.Contains(msg, "List-Unsubscribe-Post: List-Unsubscribe=One-Click") {
		t.Fatalf("missing unsubscribe headers:\n%s", msg)
	}
	// Mail providers post a form, not JSON, and send no Origin.
	r := httptest.NewRequest("POST", "/api/unsubscribe/"+a.unsubToken(u), strings.NewReader("List-Unsubscribe=One-Click"))
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
	msg := string(a.message(queuedMail{ID: "x", To: "ana@example.com", Subject: "Names drawn: Swap\r\nBcc: evil@example.com", Body: "Hi"}))
	head := strings.SplitN(msg, "\r\n\r\n", 2)[0]
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("header injected:\n%s", head)
		}
	}
}
