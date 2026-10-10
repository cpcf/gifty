package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type stateData struct {
	Users     map[string]*user
	Exchanges map[string]*exchange
	Sessions  map[string]session
	Outbox    []queuedMail `json:",omitempty"`
}

// loadKey reads the server secret from gifty.key beside the data file, creating it on first run.
func (a *application) loadKey() error {
	if a.path == "" {
		a.key = []byte(token())
		return nil
	}
	dir := filepath.Dir(a.path)
	kp := filepath.Join(dir, "gifty.key")
	b, err := os.ReadFile(kp)
	if err == nil {
		if a.key = bytes.TrimSpace(b); len(a.key) < 32 {
			return errors.New("gifty.key is damaged")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	k := token()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gifty-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(k + "\n"); err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), kp)
	}
	if err != nil {
		return err
	}
	a.key = []byte(k)
	return nil
}

func (a *application) save() error {
	if a.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.path), ".gifty-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), a.path)
}
