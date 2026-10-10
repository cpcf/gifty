// Package web contains only the application's embedded public assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed public
var files embed.FS

// Public returns the assets relative to their public root. Go source and tests are outside it.
func Public() fs.FS {
	public, err := fs.Sub(files, "public")
	if err != nil {
		panic(err)
	}
	return public
}
