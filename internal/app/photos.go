package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Photos live as files in a photos directory beside the data file, named by the SHA-256 of their bytes, so the
// data file holds only a short reference and stays small however many photos people add. A file is written before
// the save that refers to it; files nothing refers to any more are removed by sweepPhotos.

// Photo is a stored photo: its file, its content type (checked against the bytes on the way in and out) and its
// size, for the budgets.
type photo struct {
	File string `json:"file"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

// maxPhotoTotal is the decoded size of all photos on the site, so the disk stays bounded however many accounts
// upload. A variable so tests can shrink it.
var maxPhotoTotal = 1 << 30

func (a *application) photoDir() string { return filepath.Join(filepath.Dir(a.path), "photos") }

// validPhotoName is a hex SHA-256, the only kind of name storePhoto gives a file.
func validPhotoName(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// storePhoto writes b to its file, unless an identical photo is already stored, and returns its reference.
func (a *application) storePhoto(b []byte, mime string) (photo, error) {
	sum := sha256.Sum256(b)
	p := photo{File: hex.EncodeToString(sum[:]), Type: mime, Size: len(b)}
	if a.path == "" {
		if a.memPhotos == nil {
			a.memPhotos = map[string][]byte{}
		}
		if _, ok := a.memPhotos[p.File]; !ok {
			a.memPhotos[p.File] = b
			a.photoBytes += len(b)
		}
		return p, nil
	}
	dir := a.photoDir()
	dest := filepath.Join(dir, p.File)
	if _, err := os.Stat(dest); err == nil {
		return p, nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return photo{}, err
	}
	f, err := os.CreateTemp(dir, ".photo-*")
	if err != nil {
		return photo{}, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), dest)
	}
	if err != nil {
		return photo{}, err
	}
	a.photoBytes += len(b)
	return p, nil
}

// readPhoto returns a stored photo's bytes, refusing anything that isn't the image its reference says it is.
func (a *application) readPhoto(p photo) ([]byte, error) {
	if !validPhotoName(p.File) {
		return nil, errors.New("bad photo name")
	}
	var b []byte
	if a.path == "" {
		b = a.memPhotos[p.File]
	} else {
		f, err := os.Open(filepath.Join(a.photoDir(), p.File))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if b, err = io.ReadAll(io.LimitReader(f, int64(maxImageBytes)+1)); err != nil {
			return nil, err
		}
	}
	if len(b) == 0 || len(b) > maxImageBytes || sniffImage(b) != p.Type {
		return nil, errors.New("stored photo is damaged")
	}
	return b, nil
}

// migratePhotos moves photos from data files written before the photos directory into it, and reports whether
// anything moved. A photo that no longer decodes is dropped.
func (a *application) migratePhotos() (bool, error) {
	moved := false
	for _, u := range a.state.Users {
		for i := range u.Wishes {
			w := &u.Wishes[i]
			for _, s := range w.Images {
				moved = true
				b, mime, err := decodeImage(s)
				if err != nil {
					log.Printf("dropping an unreadable photo on idea %s", w.ID)
					continue
				}
				p, err := a.storePhoto(b, mime)
				if err != nil {
					return false, err
				}
				w.Photos = append(w.Photos, p)
			}
			w.Images = nil
		}
	}
	return moved, nil
}

// sweepPhotos removes photo files that no idea refers to, and recounts the bytes the rest take up. It runs with
// the state lock held, after a save that may have dropped photos, so a file is only removed once the data file no
// longer needs it.
func (a *application) sweepPhotos() {
	used, total := map[string]bool{}, 0
	for _, u := range a.state.Users {
		for _, w := range u.Wishes {
			for _, p := range w.Photos {
				if !used[p.File] {
					used[p.File] = true
					total += p.Size
				}
			}
		}
	}
	a.photoBytes = total
	if a.path == "" {
		for k := range a.memPhotos {
			if !used[k] {
				delete(a.memPhotos, k)
			}
		}
		return
	}
	entries, err := os.ReadDir(a.photoDir())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("sweeping photos: %v", err)
		}
		return
	}
	for _, e := range entries {
		if !used[e.Name()] {
			if err := os.Remove(filepath.Join(a.photoDir(), e.Name())); err != nil {
				log.Printf("sweeping photos: %v", err)
			}
		}
	}
}
func imageURL(wishID string, i int) string { return "/image/" + wishID + "/" + strconv.Itoa(i) }

// Photo limits, far above what a phone photo needs after the app resizes it (to about 150 KB). They are
// variables so tests can shrink them. maxPhotoTotal, in photos.go, bounds the whole site.
var (
	maxImageBytes = 512 * 1024      // decoded bytes per photo
	maxImages     = 5               // photos per idea
	maxImageTotal = 8 * 1024 * 1024 // decoded bytes of photos per person
)

// sniffImage returns the content type of image bytes by their magic numbers, or "".
func sniffImage(b []byte) string {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) > 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// decodeImage validates a photo data URI and returns its bytes and content type. Only real image
// bytes of the declared type are accepted, so a photo can never smuggle markup or script.
func decodeImage(s string) ([]byte, string, error) {
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return nil, "", bad("Send the photo as image data.")
	}
	comma := strings.Index(s, ",")
	if comma < 0 {
		return nil, "", bad("That photo data is malformed.")
	}
	meta := strings.Split(s[len(prefix):comma], ";")
	if len(meta) != 2 || meta[1] != "base64" {
		return nil, "", bad("Send the photo as base64 image data.")
	}
	b, err := base64.StdEncoding.DecodeString(s[comma+1:])
	if err != nil {
		return nil, "", bad("That photo data is malformed.")
	}
	if len(b) == 0 || len(b) > maxImageBytes {
		return nil, "", bad("A photo can be up to 512 KB. Try a smaller one.")
	}
	if sniffImage(b) != meta[0] {
		return nil, "", bad("That file isn’t a JPEG, PNG, GIF or WebP photo.")
	}
	return b, meta[0], nil
}

// imageBudget refuses photos that would push u past the per-person total, or the site past its own. wishBytes
// is the size of all of wishID's photos after the change, which replace its stored ones; uploaded is the size
// of the photos being added.
func (a *application) imageBudget(u *user, wishID string, wishBytes, uploaded int) error {
	total := wishBytes
	for _, w := range u.Wishes {
		if w.ID != wishID {
			for _, p := range w.Photos {
				total += p.Size
			}
		}
	}
	if total > maxImageTotal {
		return bad(fmt.Sprintf("Your photos add up to more than %d MB. Remove some from an older idea first.", maxImageTotal>>20))
	}
	if uploaded > 0 && a.photoBytes+uploaded > maxPhotoTotal {
		log.Print("photo refused: the site's photo storage is full")
		return problem{507, "Gifty has run out of room for photos. Save the idea without one for now."}
	}
	return nil
}

// serveImage sends an idea's photo to its owner, or to the person buying for them in an exchange the
// idea is shown in — the same rule as the wish list itself. The bytes are never cached, so removing
// or replacing a photo takes effect at once.
func (a *application) serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	// Only the lookup and the permission check hold the lock; the file is read and sent without it.
	photo, status := a.findPhoto(r)
	switch status {
	case 401:
		http.Error(w, "Sign in to continue.", 401)
		return
	case 429:
		http.Error(w, "Too many requests. Try again in a few minutes.", 429)
		return
	case 404:
		http.NotFound(w, r)
		return
	}
	data, err := a.readPhoto(photo)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", photo.Type)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// findPhoto returns the photo /image/<idea>/<n> names, if the caller may see it, or the status to answer with.
func (a *application) findPhoto(r *http.Request) (photo, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.allow("read|"+a.clientIP(r), maxReads) {
		a.sec(r, "read_rate_limited")
		return photo{}, 429
	}
	u := a.user(r)
	if u == nil {
		return photo{}, 401
	}
	id, num, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/image/"), "/")
	n, err := strconv.Atoi(num)
	owner := a.state.Users[a.wishOwner[id]]
	if err != nil || n < 0 || owner == nil {
		return photo{}, 404
	}
	i := slices.IndexFunc(owner.Wishes, func(w wish) bool { return w.ID == id })
	if i < 0 || n >= len(owner.Wishes[i].Photos) {
		return photo{}, 404
	}
	wish := owner.Wishes[i]
	if owner.ID != u.ID {
		allowed := wish.Friends && slices.Contains(owner.Friends, u.ID) && (wish.Status == "" || wish.ClaimedBy == u.ID)
		for _, e := range a.state.Exchanges {
			if e.Assignments[u.ID] == owner.ID && wish.forExchange(e.ID) && (wish.Status == "" || wish.ClaimedBy == u.ID) {
				allowed = true
				break
			}
		}
		if !allowed {
			return photo{}, 404
		}
	}
	return wish.Photos[n], 200
}
