package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Photos live as files in a photos directory beside the data file, named by the SHA-256 of their bytes, so the
// data file holds only a short reference and stays small however many photos people add. A file is written before
// the save that refers to it; files nothing refers to any more are removed by sweepPhotos.

// Photo is a stored photo: its file, its content type (checked against the bytes on the way in and out) and its
// size, for the budgets.
type Photo struct {
	File string `json:"file"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

// maxPhotoTotal is the decoded size of all photos on the site, so the disk stays bounded however many accounts
// upload. A variable so tests can shrink it.
var maxPhotoTotal = 1 << 30

func (a *App) photoDir() string { return filepath.Join(filepath.Dir(a.path), "photos") }

// validPhotoName is a hex SHA-256, the only kind of name storePhoto gives a file.
func validPhotoName(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// storePhoto writes b to its file, unless an identical photo is already stored, and returns its reference.
func (a *App) storePhoto(b []byte, mime string) (Photo, error) {
	sum := sha256.Sum256(b)
	p := Photo{File: hex.EncodeToString(sum[:]), Type: mime, Size: len(b)}
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
		return Photo{}, err
	}
	f, err := os.CreateTemp(dir, ".photo-*")
	if err != nil {
		return Photo{}, err
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
		return Photo{}, err
	}
	a.photoBytes += len(b)
	return p, nil
}

// readPhoto returns a stored photo's bytes, refusing anything that isn't the image its reference says it is.
func (a *App) readPhoto(p Photo) ([]byte, error) {
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
func (a *App) migratePhotos() (bool, error) {
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
func (a *App) sweepPhotos() {
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
