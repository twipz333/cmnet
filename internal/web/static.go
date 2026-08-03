package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// assets is the whole front end, compiled into the binary. There is no build step
// and no CDN: `go build` stays the only command needed to produce a self-contained
// advisor.exe (§3).
//
//go:embed static
var assets embed.FS

// staticHandler serves the embedded front end from the URL root.
func staticHandler() http.Handler {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		// Impossible unless the embed directive above is broken at build time.
		panic("web: embedded assets missing: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The UI is a single page; anything unknown falls back to it.
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, r.URL.Path[1:]); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
}
