package main

// Rendering and static assets, compiled into the binary so it stays a single
// file with no runtime dependency on the working directory.

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"net/http"

	"github.com/a-h/templ"
)

//go:generate go tool templ generate

//go:embed static
var staticFS embed.FS

// staticHandler serves the embedded CSS and scripts under /public/static/.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	files := http.StripPrefix("/public/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

var assetURLs = map[string]string{}

func assetURL(name string) string {
	if u, ok := assetURLs[name]; ok {
		return u
	}
	data, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(data)
	u := "/public/static/" + name + "?v=" + hex.EncodeToString(sum[:])[:12]
	assetURLs[name] = u
	return u
}

func init() {
	// Fill the cache before serving, so concurrent requests only read it.
	for _, name := range []string{"theme.js", "bootstrap.min.css", "app.css", "bootstrap.bundle.min.js", "htmx.min.js", "fields.js"} {
		assetURL(name)
	}
}

// render writes a page. Errors are logged rather than returned: by this point
// the response has usually started.
func render(w http.ResponseWriter, status int, page templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := page.Render(context.Background(), w); err != nil {
		log.Printf("render: %v", err)
	}
}
