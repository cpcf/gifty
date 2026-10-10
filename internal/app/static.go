package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"time"

	"github.com/cpcf/gifty/web"
)

func (a *application) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", a.serveAPI)
	mux.HandleFunc("/image/", a.serveImage)
	mux.HandleFunc("/calendar/", a.serveCalendar)
	// Embedded files have no modification time, so content hashes let browsers revalidate instead of re-downloading.
	type file struct {
		body []byte
		etag string
	}
	static := map[string]file{}
	assets := web.Public()
	err := fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		static["/"+name] = file{b, `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Keep the original public URLs as assets move into subdirectories.
	for legacy, name := range map[string]string{
		"style.css": "styles/style.css", "favicon.svg": "assets/favicon.svg",
		"public-sans.woff2": "assets/public-sans.woff2", "public-sans-OFL.txt": "assets/public-sans-OFL.txt",
	} {
		if f, ok := static["/"+name]; ok {
			static["/"+legacy] = f
		}
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		} else if path == "/index.html" {
			path = ""
		}
		f, ok := static[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", f.etag)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(f.body))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}
