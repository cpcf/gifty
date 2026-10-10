package app

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

type application struct {
	mu     sync.Mutex
	state  stateData
	path   string
	secure bool
	// trustProxy reads the client address from X-Forwarded-For on loopback requests, for running behind a local reverse proxy.
	trustProxy bool
	limits     map[string]limiter
	swept      time.Time
	// fails counts login attempts per account, and gateFails wrong access codes across the whole site.
	// Both are bounded (by the number of accounts, and by one) so flooding them with other keys can't reset them.
	fails     map[string]limiter
	gateFails limiter
	// key is the server secret in gifty.key, kept out of the data file and its backups. It keys the access-gate
	// cookie and the unsubscribe links.
	key []byte
	// mailer is nil when email is not configured.
	mailer func(to string, msg []byte) error
	base   string
	from   string
	wake   chan struct{}
	// access is the keyed hash of GIFTY_ACCESS_CODE (see gate); empty means anyone may sign up.
	access string
	// admins holds the lowercased emails from GIFTY_ADMINS.
	admins map[string]bool
	// hashSlots bounds concurrent password hashing, which takes about 300 ms of CPU each. Requests only reach it
	// once they are past the access gate and have a plausible target, so it can't be filled anonymously.
	hashSlots chan struct{}
	// hourStart and hourCount cap account emails across the whole site.
	hourStart time.Time
	hourCount int
	// watch counts security events for the emails sent to alertTo (see security.go).
	watch   watch
	alertTo string
	// photoBytes is the size of every stored photo, for maxPhotoTotal. memPhotos stands in for the photos
	// directory when there is no data file.
	photoBytes int
	memPhotos  map[string][]byte
	// dropped is set by a request that may have left photo files unused, so they are swept after its save.
	dropped bool
	// wishOwner maps each idea to the account it is on, so /image/ finds a photo without scanning every list.
	// Ideas never move between accounts; an entry for a deleted idea just fails the check in serveImage.
	wishOwner map[string]string
}

func openApp(path string) (*application, error) {
	a := &application{path: path, limits: map[string]limiter{}, fails: map[string]limiter{}, hashSlots: make(chan struct{}, 2), state: stateData{Users: map[string]*user{}, Exchanges: map[string]*exchange{}, Sessions: map[string]session{}}}
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &a.state)
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if a.state.Users == nil || a.state.Exchanges == nil || a.state.Sessions == nil {
		return nil, errors.New("invalid data file")
	}
	if err := a.loadKey(); err != nil {
		return nil, err
	}
	moved, err := a.migratePhotos()
	if err != nil {
		return nil, err
	}
	if moved {
		if err := a.save(); err != nil {
			return nil, err
		}
	}
	a.sweepPhotos()
	a.wishOwner = map[string]string{}
	for _, u := range a.state.Users {
		for _, w := range u.Wishes {
			a.wishOwner[w.ID] = u.ID
		}
	}
	return a, nil
}

// Site-wide ceilings, far above what friends and family need, so the data file and the work done on every
// save stay bounded however it is used.
const (
	maxUsers     = 2000
	maxExchanges = 5000
)
