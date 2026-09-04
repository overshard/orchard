//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// In development the templates and stylesheet are read from disk on every
// request, so editing CSS and reloading the browser is enough to see it.
//
// The release build uses assets_embed.go instead. This split exists because an
// embedded stylesheet is baked into the binary at compile time, so a CSS edit
// silently does nothing until a rebuild, which looks exactly like a CSS bug.
func assets() fs.FS { return os.DirFS(".") }

// Reloaded reports whether assets come off disk, so the server can say so.
const Reloaded = true
