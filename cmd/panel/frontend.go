package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func frontendHandler(webRoot string) http.Handler {
	fs := http.FileServer(http.Dir(webRoot))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API paths belong to explicit API handlers, never to the SPA fallback.
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}

		// Rooting at "/" collapses traversal segments before the filesystem is
		// touched, so a crafted URL cannot escape the built frontend directory.
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		filePath := filepath.Join(webRoot, cleanPath)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			// Vite fingerprints /assets files. A new release changes each name,
			// while index.html must be revalidated to discover those new names.
			if strings.HasPrefix(cleanPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fs.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
	})
}
