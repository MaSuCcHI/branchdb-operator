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
	data, err := k8sDistFS.ReadFile("k8s-dist/index.html")
	if err != nil {
		http.Error(w, "console not built (run make web-build)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
