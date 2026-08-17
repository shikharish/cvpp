// Package editor exposes the browser application to the Go server. Keeping
// the embed declaration next to the static files lets release binaries run
// without a checkout of the repository.
package editor

import "embed"

// Files contains the complete browser application.
//
//go:embed index.html app.js core.js styles.css
var Files embed.FS
