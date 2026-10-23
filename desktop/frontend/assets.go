package frontend

import "embed"

// Assets contains the static frontend. Everything is plain HTML/CSS/JS; there
// is no build step and no bundler.
//
//go:embed index.html css js
var Assets embed.FS
