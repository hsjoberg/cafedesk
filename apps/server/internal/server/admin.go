package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// adminFiles serves the built admin: its files as they are, and index.html
// for any other path, which the admin's router reads.
func (s *Server) adminFiles() http.Handler {
	files := s.cfg.Admin
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if files == nil {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(files, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("The admin isn't built into this server. Run `bun run build` in apps/admin, then build the server again.\n"))
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(files, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") { // named by their content: safe to keep for a year
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFileFS(w, r, files, name)
				return
			}
			if path.Ext(name) != "" { // a missing file, not a page of the admin
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, files, "index.html")
	})
}
