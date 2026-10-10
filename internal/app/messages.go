package app

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Message is one line of an anonymous conversation between a giver and their recipient.
type message struct {
	FromGiver bool
	Text      string
	At        time.Time
}

const (
	maxMessage  = 500 // characters
	maxMessages = 40  // from each side of a conversation
)

func threadView(thread []message, asGiver bool) []map[string]any {
	out := []map[string]any{}
	for _, m := range thread {
		out = append(out, map[string]any{"mine": m.FromGiver == asGiver, "text": m.Text, "at": m.At})
	}
	return out
}

// say adds a line to the conversation between u and the person on the other side of the draw. To is
// "recipient" (u is the giver, writing to who they buy for) or "giver" (u is the recipient, answering).
// Neither side learns who the other is from this.
func (a *application) say(e *exchange, u *user, r *http.Request) error {
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
	if sent := len(slices.DeleteFunc(slices.Clone(thread), func(m message) bool { return m.FromGiver != fromGiver })); sent >= maxMessages {
		return bad("You’ve sent as many messages as this conversation allows.")
	}
	// Email only when the conversation changes hands, so two messages in a row are one email.
	notify := len(thread) == 0 || thread[len(thread)-1].FromGiver != fromGiver
	if e.Threads == nil {
		e.Threads = map[string][]message{}
	}
	e.Threads[giver] = append(thread, message{FromGiver: fromGiver, Text: text, At: time.Now().UTC()})
	if notify {
		who := "The person you’re buying for in"
		if fromGiver {
			who = "Someone who is buying for you in"
		}
		a.queue(a.state.Users[other], true, "A message about "+e.Name, fmt.Sprintf("%s %s has sent you a message. Gifty keeps both of you anonymous to each other.\n\nSign in to read it and reply:\n%s/#exchange/%s", who, e.Name, a.base, e.ID))
	}
	return nil
}
