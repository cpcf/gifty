package app

import (
	"bytes"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSecurityEventsAreLoggedAndCounted(t *testing.T) {
	a := setup(t)
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	(&client{t: t, a: a}).signup("ana")
	c := &client{t: t, a: a}
	c.req("POST", "login", map[string]string{"Email": "ana@example.com", "Password": "not the password"}, 401)
	if !strings.Contains(out.String(), "security: login_failed ip=192.0.2.1") {
		t.Fatalf("a failed login was not logged: %q", out.String())
	}
	if strings.Contains(out.String(), "not the password") || strings.Contains(out.String(), "ana@example.com") {
		t.Fatal("the log names the attempt's credentials")
	}
	if a.watch.events["login_failed"] != 1 || a.watch.ips["192.0.2.1"] != 1 {
		t.Fatalf("not counted: %v %v", a.watch.events, a.watch.ips)
	}
	r := httptest.NewRequest("POST", "/api/logout", strings.NewReader("{}"))
	a.handler().ServeHTTP(httptest.NewRecorder(), r)
	if !strings.Contains(out.String(), "security: origin_refused ip=192.0.2.1") {
		t.Fatalf("a cross-site post was not logged: %q", out.String())
	}
}

func TestAlertsAndSummaries(t *testing.T) {
	a := setup(t)
	var sent []string
	a.mailer = func(to string, msg []byte) error { sent = append(sent, to+"\n"+string(msg)); return nil }
	a.alertTo, a.base, a.from = "me@example.com", "https://gifty.test", "Gifty <g@gifty.test>"
	day := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < spikeThreshold-1; i++ {
		a.watch.add(day, "login_failed", "198.51.100.7")
	}
	a.alertTick(day)
	if len(sent) != 0 {
		t.Fatal("alerted below the threshold")
	}
	a.watch.add(day, "login_failed", "198.51.100.7")
	a.alertTick(day)
	if len(sent) != 1 || !strings.HasPrefix(sent[0], "me@example.com\n") || !strings.Contains(sent[0], "198.51.100.7") {
		t.Fatalf("no alert for a burst: %q", sent)
	}
	a.alertTick(day.Add(time.Minute))
	if len(sent) != 1 {
		t.Fatal("alerted twice inside the cooldown")
	}
	// Old events fall out of the window, and the summary goes out once the date turns over, only if something happened.
	a.alertTick(day.Add(spikeWindow + time.Minute))
	if len(a.watch.recent) != 0 {
		t.Fatal("old events stayed in the window")
	}
	a.alertTick(day.Add(24 * time.Hour))
	if len(sent) != 2 || !strings.Contains(sent[1], "summary for 2026-01-05") || !strings.Contains(sent[1], "login_failed: 40") {
		t.Fatalf("no summary: %q", sent)
	}
	a.alertTick(day.Add(48 * time.Hour))
	if len(sent) != 2 {
		t.Fatal("sent a summary for a quiet day")
	}
}
