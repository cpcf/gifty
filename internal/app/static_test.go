package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAssetsAndRevalidation(t *testing.T) {
	h := setup(t).handler()
	for _, path := range []string{"/", "/app.js", "/core/api.js", "/features/friends.js", "/style.css", "/styles/style.css", "/favicon.svg", "/public-sans.woff2", "/public-sans-OFL.txt"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.Len() == 0 {
				t.Fatalf("GET %s: status %d, bytes %d", path, w.Code, w.Body.Len())
			}
			if strings.HasSuffix(path, ".js") && !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
				t.Fatalf("module MIME: %s", w.Header().Get("Content-Type"))
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing shared security headers")
			}
			if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") == "" {
				t.Fatal("missing revalidation headers")
			}
			conditional := httptest.NewRequest(http.MethodGet, path, nil)
			conditional.Header.Set("If-None-Match", w.Header().Get("ETag"))
			cached := httptest.NewRecorder()
			h.ServeHTTP(cached, conditional)
			if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
				t.Fatalf("revalidation: %d, bytes %d", cached.Code, cached.Body.Len())
			}
			head := httptest.NewRecorder()
			h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, path, nil))
			if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("ETag") != w.Header().Get("ETag") {
				t.Fatal("HEAD differs from GET metadata")
			}
		})
	}
	for _, path := range []string{"/index.html", "/embed.go", "/package.json", "/internal/app/config.go", "/internal/app/static_test.go", "/tests/unit/birthday-dates.mjs", "/public/index.html"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("private path %s: %d", path, w.Code)
		}
	}
	legacy, nested := httptest.NewRecorder(), httptest.NewRecorder()
	h.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/style.css", nil))
	h.ServeHTTP(nested, httptest.NewRequest(http.MethodGet, "/styles/style.css", nil))
	if !bytes.Equal(legacy.Body.Bytes(), nested.Body.Bytes()) || legacy.Header().Get("ETag") != nested.Header().Get("ETag") {
		t.Fatal("legacy asset URL has different content or cache identity")
	}
}
