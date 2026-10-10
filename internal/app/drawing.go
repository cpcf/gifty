package app

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"slices"
	"time"
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

// setApart replaces the organiser's list of pairs who shouldn't draw each other.
func (a *application) setApart(e *exchange, r *http.Request) error {
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
func (e *exchange) revealable(now time.Time) bool {
	return len(e.Assignments) > 0 && !e.Revealed && now.UTC().AddDate(0, 0, 1).Format("2006-01-02") >= e.Date
}

func (a *application) reveal(e *exchange) error {
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
