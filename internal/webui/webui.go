// Package webui serves the React single-page app embedded in the binary.
// The build copies web/dist into internal/webui/dist (see the Makefile and Dockerfile).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const csp = "default-src 'self'; img-src 'self' data: blob: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; " +
	"connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	root, _ := fs.Sub(dist, "dist")
	files := http.FileServer(http.FS(root))
	_, err := fs.Stat(root, "index.html")
	built := err == nil
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		if !built {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>Workflow</title><p>The web UI is not built into this binary. Run <code>make web</code> or use the Vite dev server.</p>"))
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if f, err := fs.Stat(root, p); err == nil && !f.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}
