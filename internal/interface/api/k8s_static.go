package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed k8s-dist
var k8sDistFS embed.FS

func k8sStaticHandler() http.Handler {
	sub, _ := fs.Sub(k8sDistFS, "k8s-dist")
	return http.FileServer(http.FS(sub))
}

func serveK8sSPA(w http.ResponseWriter, _ *http.Request) {
	// Try k8s.html first (Vite builds with the input filename as output).
	// Fall back to index.html for the placeholder or any custom build.
	for _, name := range []string{"k8s-dist/k8s.html", "k8s-dist/index.html"} {
		data, err := k8sDistFS.ReadFile(name)
		if err != nil {
			continue
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
		return
	}
	http.Error(w, "console not built (run make web-build-k8s)", http.StatusNotFound)
}
