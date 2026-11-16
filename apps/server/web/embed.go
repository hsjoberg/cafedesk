// Package web holds the built admin, which apps/admin's build writes into
// dist. The server embeds it, so one binary serves the API and the admin.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Admin returns the built admin's files.
func Admin() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
