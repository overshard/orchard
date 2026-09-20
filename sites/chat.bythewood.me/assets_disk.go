//go:build !embed

package main

import (
	"io/fs"
	"os"
)

// In development the templates and stylesheet are read from disk on every
// request, so editing CSS and reloading the browser is enough to see it. The
// release build embeds them, where a CSS edit does nothing until a rebuild.
func assets() fs.FS { return os.DirFS(".") }

// Reloaded reports whether assets come off disk, so the server can say so.
const Reloaded = true
