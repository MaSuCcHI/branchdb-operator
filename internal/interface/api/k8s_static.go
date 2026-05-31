package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed k8s-dist
var k8sDistFS embed.FS

//go:embed openapi.yaml
var openapiSpec []byte

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

func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Write(openapiSpec)
}

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="UTF-8" />
  <title>BranchDB API Docs</title>
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; }
    #swagger-ui .topbar { background: #1a1d2e; }
    #swagger-ui .topbar .download-url-wrapper { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/openapi.yaml",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
      defaultModelsExpandDepth: 2,
      defaultModelExpandDepth: 2,
    });
  </script>
</body>
</html>`

func serveSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerUIHTML))
}
