// Package webui embeds the compiled Svelte single-page application so the Go
// binary is self-contained.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
)

// dist is written by `bun run build` in the web/ directory. It is not
// committed, so the Go build fails until the frontend has been built.
//
//go:embed all:dist
var dist embed.FS

// FS returns the built web assets rooted at the dist directory.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(fmt.Sprintf("webui: embedded dist is invalid: %v", err))
	}
	return sub
}
