package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedSaveRollsBack(t *testing.T) {
	a := setup(t)
	c := &client{t: t, a: a}
	c.signup("Rollback")
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a.path = filepath.Join(block, "data.json")
	c.req("POST", "wishes", map[string]string{"title": "Should not save"}, 500)
	v := c.req("GET", "me", nil, 200)
	if len(v["wishes"].([]any)) != 0 {
		t.Fatal("failed transaction changed memory")
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gifty.json")
	a, err := openApp(path)
	if err != nil {
		t.Fatal(err)
	}
	// The secret lives beside the data file, never in it, and is private to the service user.
	if fi, err := os.Stat(filepath.Join(dir, "gifty.key")); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("key file %v %v", fi, err)
	}
	a.state.Users["x"] = &user{ID: "x", Name: "x"}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), string(a.key)) {
		t.Fatal("the key is in the data file")
	}
	// It survives a restart, so access cookies and unsubscribe links keep working.
	b, err := openApp(path)
	if err != nil || string(b.key) != string(a.key) {
		t.Fatalf("reopen: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "gifty.key"), []byte("short\n"), 0600)
	if _, err := openApp(path); err == nil {
		t.Fatal("a damaged key file was accepted")
	}
}
