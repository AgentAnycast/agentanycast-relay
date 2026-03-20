package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web
var webFS embed.FS

// WebHandler returns an http.Handler that serves the embedded SPA files.
// It falls back to index.html for SPA client-side routing.
func WebHandler() http.Handler {
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		panic("embedded web filesystem not found: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try serving the exact file first.
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists in embedded FS.
		if f, err := subFS.Open(path[1:]); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for SPA routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
