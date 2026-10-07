package main

import (
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Security events are logged as "security: <event> ip=<address>", a fixed format that deploy/fail2ban
// matches, and counted so that a burst or a day's activity can be emailed to GIFTY_ALERT_EMAIL.
// Nothing about the request beyond the event and the caller's address is recorded.

const (
	spikeWindow    = 10 * time.Minute
	spikeThreshold = 40 // events inside the window that count as an attack rather than a mistyped password
	spikeCooldown  = time.Hour
	maxWatchedIPs  = 1000
	maxRecent      = 10000
)

// watch holds the counters behind the alerts. It has its own lock because events are raised both with
// and without the state lock held. Counts are in memory, so a restart starts them afresh.
type watch struct {
	mu        sync.Mutex
	recent    []time.Time
	events    map[string]int
	ips       map[string]int
	day       string // UTC date the counts above belong to
	lastAlert time.Time
}

// sec records a security event for the caller of r.
func (a *App) sec(r *http.Request, event string) {
	ip := a.rawIP(r)
	log.Printf("security: %s ip=%s", event, ip)
	a.watch.add(time.Now(), event, ip)
}

func (w *watch) add(now time.Time, event, ip string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.events == nil {
		w.events, w.ips, w.day = map[string]int{}, map[string]int{}, now.UTC().Format("2006-01-02")
	}
	w.events[event]++
	if _, seen := w.ips[ip]; seen || len(w.ips) < maxWatchedIPs {
		w.ips[ip]++
	}
	w.recent = append(w.recent, now)
	if len(w.recent) > maxRecent {
		w.recent = w.recent[len(w.recent)-maxRecent:]
	}
}

// top lists the n largest entries of counts as "name: count" lines.
func top(counts map[string]int, n int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y string) int {
		if counts[x] != counts[y] {
			return counts[y] - counts[x]
		}
		return strings.Compare(x, y)
	})
	var b strings.Builder
	for _, k := range keys[:min(n, len(keys))] {
		fmt.Fprintf(&b, "  %s: %d\n", k, counts[k])
	}
	return b.String()
}

// report returns the subject and body of an email due at now, or an empty subject if none is.
// A burst is reported at most once an hour; the previous UTC day's totals go out once the date turns over.
func (w *watch) report(now time.Time) (subject, body string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.events == nil {
		return
	}
	if day := now.UTC().Format("2006-01-02"); day != w.day {
		if len(w.events) > 0 {
			subject = "Gifty security summary for " + w.day
			body = fmt.Sprintf("Events:\n%s\nBusiest addresses:\n%s", top(w.events, 20), top(w.ips, 10))
		}
		w.events, w.ips, w.day = map[string]int{}, map[string]int{}, day
		return
	}
	cut := now.Add(-spikeWindow)
	w.recent = slices.DeleteFunc(w.recent, func(t time.Time) bool { return t.Before(cut) })
	if len(w.recent) >= spikeThreshold && now.Sub(w.lastAlert) >= spikeCooldown {
		w.lastAlert = now
		subject = "Gifty: unusual activity"
		body = fmt.Sprintf("%d security events in the last %d minutes.\n\nToday so far:\n%s\nBusiest addresses today:\n%s\nfail2ban bans repeat offenders on its own; `sudo fail2ban-client status gifty` lists who is banned, and `journalctl -u gifty | grep security:` has the detail.\n", len(w.recent), int(spikeWindow.Minutes()), top(w.events, 20), top(w.ips, 10))
	}
	return
}

// alertTick sends an alert or summary if one is due.
func (a *App) alertTick(now time.Time) {
	subject, body := a.watch.report(now)
	if subject == "" {
		return
	}
	m := Mail{ID: token(), To: a.alertTo, Subject: subject, Body: body + "\n--\nGifty, " + a.base + "\n"}
	if err := a.mailer(a.alertTo, a.message(m)); err != nil {
		log.Printf("sending alert: %v", err)
	}
}

func (a *App) alertLoop() {
	for range time.Tick(time.Minute) {
		a.alertTick(time.Now())
	}
}
