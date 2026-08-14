package adminui

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func Handler() http.Handler {
	sub, err := fs.Sub(assets, assetRoot)
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assetPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if assetPath == "." || assetPath == "" {
			assetPath = "index.html"
		}
		if _, err := fs.Stat(sub, assetPath); err != nil {
			assetPath = "index.html"
		}
		if extension := path.Ext(assetPath); extension != "" {
			if contentType := mime.TypeByExtension(extension); contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
		}
		if assetPath == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		content, err := fs.ReadFile(sub, assetPath)
		if err != nil {
			http.Error(w, "admin asset unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(content)
		}
	})
}
