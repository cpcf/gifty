package app

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type wish struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Note  string `json:"note"`
	Price string `json:"price"`
	// Exchanges limits who sees the idea to the people buying for this user in those exchanges; empty means every exchange.
	Exchanges []string `json:"exchanges"`
	// Friends shows the idea to the owner's friends. NoExchanges keeps it from whoever is buying for the owner,
	// whatever Exchanges says, so an idea can be for friends only.
	Friends     bool `json:"friends,omitempty"`
	NoExchanges bool `json:"noExchanges,omitempty"`
	// Photos are the idea's photos in display order, stored as files beside the data file (see photos.go). Clients
	// are never sent them, only same-origin URLs to fetch the bytes from /image/.
	Photos []photo `json:"photos,omitempty"`
	// Images held photos as data URIs in data files from before the photos directory. openApp moves them to Photos.
	Images []string `json:"images,omitempty"`
	// Status is set by the owner when the idea no longer needs a gift: "got" (they have it) or "dropped" (they
	// don't want it any more). Sorted ideas are hidden from the people buying for them.
	Status string `json:"status,omitempty"`
	// ClaimedBy is the giver who said they are getting this, and ClaimedIn the exchange they are giving in. The
	// owner is never told; other givers see only that someone has it.
	ClaimedBy string    `json:"claimedBy,omitempty"`
	ClaimedIn string    `json:"claimedIn,omitempty"` // an exchange, or friendClaim for a friend's claim
	ClaimedAt time.Time `json:"claimedAt,omitzero"`
}

// friendClaim is ClaimedIn for a claim made as a friend rather than as someone's giver in an exchange.
const friendClaim = "friends"

// forExchange is whether the idea is meant for whoever buys for its owner in exchange id.
func (w wish) forExchange(id string) bool {
	return !w.NoExchanges && (len(w.Exchanges) == 0 || slices.Contains(w.Exchanges, id))
}

// wishOut is an idea as clients see it: photos are URLs, never the stored data.
type wishOut struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Note      string   `json:"note"`
	Price     string   `json:"price"`
	Exchanges []string `json:"exchanges"`
	// Friends and NoExchanges are only sent to the owner.
	Friends     bool     `json:"friends,omitempty"`
	NoExchanges bool     `json:"noExchanges,omitempty"`
	Images      []string `json:"images,omitempty"`
	Status      string   `json:"status,omitempty"`
	// Claim is only sent to a giver: "mine" or "other".
	Claim string `json:"claim,omitempty"`
}

func publicWish(w wish) wishOut {
	out := wishOut{ID: w.ID, Title: w.Title, URL: w.URL, Note: w.Note, Price: w.Price, Exchanges: w.Exchanges, Friends: w.Friends, NoExchanges: w.NoExchanges, Status: w.Status}
	for i := range w.Photos {
		out.Images = append(out.Images, imageURL(w.ID, i))
	}
	return out
}

// wishesFor is what giver, who is buying for u in exchange e, can see. Sorted ideas are hidden, except from a
// giver who had already said they were getting one, who is told it has changed.
func wishesFor(u *user, e *exchange, giver string) []wishOut {
	out := []wishOut{}
	for _, w := range u.Wishes {
		if !w.forExchange(e.ID) {
			continue
		}
		mine := w.ClaimedBy != "" && w.ClaimedBy == giver
		if w.Status != "" && !mine {
			continue
		}
		o := publicWish(w)
		o.Exchanges, o.Friends, o.NoExchanges = nil, false, false
		switch {
		case mine:
			o.Claim = "mine"
		case w.ClaimedBy != "":
			o.Claim = "other"
		}
		out = append(out, o)
	}
	return out
}

// claim records that u, who is buying for the owner of an idea, is getting it, so nobody else buying for the
// same person in another exchange gets it too. The owner is never told.
func (a *application) claim(e *exchange, u *user, r *http.Request) error {
	if e.Kind != "" {
		return bad("Mark what you’re getting from your friend’s page instead.")
	}
	to := a.state.Users[e.Assignments[u.ID]]
	if to == nil {
		return bad("Entries aren’t closed yet.")
	}
	var in struct {
		Wish  string
		Claim bool
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	i := slices.IndexFunc(to.Wishes, func(w wish) bool { return w.ID == in.Wish })
	if i < 0 {
		return problem{404, "Gift idea not found."}
	}
	w := &to.Wishes[i]
	mine := w.ClaimedBy == u.ID
	// Letting go of your own claim always works, whatever has happened to the idea or the exchange since.
	if !in.Claim {
		if mine {
			w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
		}
		return nil
	}
	if e.Archived {
		return bad("This exchange is archived.")
	}
	if !w.forExchange(e.ID) || (w.Status != "" && !mine) {
		return problem{404, "Gift idea not found."}
	}
	switch {
	case w.ClaimedBy != "" && !mine:
		return problem{409, "Someone else is already getting this one."}
	case w.Status != "":
		return bad("They’ve sorted this one out already.")
	default:
		w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = u.ID, e.ID, time.Now().UTC()
	}
	return nil
}

// clearClaims releases every idea claim that match selects.
func (a *application) clearClaims(match func(*wish) bool) {
	for _, v := range a.state.Users {
		for i := range v.Wishes {
			if w := &v.Wishes[i]; w.ClaimedBy != "" && match(w) {
				w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
			}
		}
	}
}

// setStatus marks one of u's ideas as sorted ("got", "dropped") or puts it back on the list (""). Sorted ideas
// are hidden from the people buying for u.
func (a *application) setStatus(u *user, id string, r *http.Request) error {
	var in struct{ Status string }
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !slices.Contains([]string{"", "got", "dropped"}, in.Status) {
		return bad("Choose whether you’ve got it or don’t want it.")
	}
	i := slices.IndexFunc(u.Wishes, func(w wish) bool { return w.ID == id })
	if i < 0 {
		return problem{404, "Gift idea not found."}
	}
	u.Wishes[i].Status = in.Status
	return nil
}

func (a *application) handleSaveWish(u *user, r *http.Request) (any, error) {
	var in struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		URL       string   `json:"url"`
		Note      string   `json:"note"`
		Price     string   `json:"price"`
		Exchanges []string `json:"exchanges"`
		// Friends shows the idea to friends; NoExchanges keeps it from whoever buys for you.
		Friends     bool `json:"friends"`
		NoExchanges bool `json:"noExchanges"`
		// Photos, when present, is the idea's whole photo list in order: each entry is either a
		// photo already on the idea (its /image/ URL) or a new one as a data URI. Absent means
		// leave the photos alone.
		Photos *[]string `json:"photos"`
	}
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.URL = strings.TrimSpace(in.URL)
	in.Price = strings.TrimSpace(in.Price)
	if in.Title == "" || len(in.Title) > 120 || len(in.Note) > 1000 || len(in.URL) > 2000 || len(in.Price) > 60 {
		return nil, bad("Add a title up to 120 characters, a note up to 1,000 characters and a price up to 60 characters.")
	}
	if in.URL != "" {
		p, err := url.Parse(in.URL)
		if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.User != nil {
			return nil, bad("Use a full link starting with https:// or http://.")
		}
	}
	scope := []string{}
	for _, id := range in.Exchanges {
		if e := a.state.Exchanges[id]; e == nil || !slices.Contains(e.Members, u.ID) {
			return nil, bad("Choose exchanges you’re taking part in.")
		}
		if !slices.Contains(scope, id) {
			scope = append(scope, id)
		}
	}
	index := -1
	var w wish
	if in.ID == "" {
		if len(u.Wishes) >= 100 {
			return nil, bad("Your list can hold up to 100 ideas.")
		}
		w.ID = token()
	} else {
		index = slices.IndexFunc(u.Wishes, func(v wish) bool { return v.ID == in.ID })
		if index < 0 {
			return nil, problem{404, "Gift idea not found."}
		}
		w = u.Wishes[index] // keeps the stored photos unless in.Photos replaces them
	}
	if in.NoExchanges {
		scope = []string{}
	}
	w.Title, w.URL, w.Note, w.Price, w.Exchanges, w.Friends, w.NoExchanges = in.Title, in.URL, in.Note, in.Price, scope, in.Friends, in.NoExchanges
	// A claim ends when its holder is no longer meant to see the idea.
	if w.ClaimedBy != "" {
		if w.ClaimedIn == friendClaim && !w.Friends || w.ClaimedIn != friendClaim && (w.NoExchanges || len(scope) > 0 && !slices.Contains(scope, w.ClaimedIn)) {
			w.ClaimedBy, w.ClaimedIn, w.ClaimedAt = "", "", time.Time{}
		}
	}
	if in.Photos != nil {
		if len(*in.Photos) > maxImages {
			return nil, bad(fmt.Sprintf("An idea can have up to %d photos.", maxImages))
		}
		// Each entry is a photo already on the idea, or new bytes to store once everything has been checked.
		type entry struct {
			kept photo
			data []byte
			mime string
		}
		entries, size, uploaded := []entry{}, 0, 0
		for _, p := range *in.Photos {
			if num, ok := strings.CutPrefix(p, "/image/"+w.ID+"/"); ok {
				n, err := strconv.Atoi(num)
				if err != nil || n < 0 || n >= len(w.Photos) {
					return nil, bad("That photo is no longer on this idea.")
				}
				entries = append(entries, entry{kept: w.Photos[n]})
				size += w.Photos[n].Size
				continue
			}
			data, mime, err := decodeImage(p)
			if err != nil {
				return nil, err
			}
			size += len(data)
			uploaded += len(data)
			entries = append(entries, entry{data: data, mime: mime})
		}
		if err := a.imageBudget(u, w.ID, size, uploaded); err != nil {
			return nil, err
		}
		photos := []photo{}
		for _, e := range entries {
			if e.data == nil {
				photos = append(photos, e.kept)
				continue
			}
			p, err := a.storePhoto(e.data, e.mime)
			if err != nil {
				return nil, err
			}
			photos = append(photos, p)
		}
		w.Photos = photos
		a.dropped = true
	}
	if index < 0 {
		u.Wishes = append(u.Wishes, w)
		a.wishOwner[w.ID] = u.ID
	} else {
		u.Wishes[index] = w
	}
	return a.publicUser(u), nil
}

func (a *application) handleWishStatus(u *user, id string, r *http.Request) (any, error) {
	if err := a.setStatus(u, id, r); err != nil {
		return nil, err
	}
	return a.publicUser(u), nil
}

func (a *application) handleDeleteWish(path string, u *user) (any, error) {
	id := strings.TrimPrefix(path, "wishes/")
	index := slices.IndexFunc(u.Wishes, func(v wish) bool { return v.ID == id })
	if index < 0 {
		return nil, problem{404, "Gift idea not found."}
	}
	u.Wishes = slices.Delete(u.Wishes, index, index+1)
	a.dropped = true
	return a.publicUser(u), nil
}
