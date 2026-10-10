//go:build !noui

package webassets

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

//go:embed openapi.yaml
var openapiSpec []byte

// OpenAPISpec 返回 OpenAPI 3 规范内容
func OpenAPISpec() []byte { return openapiSpec }

// Enabled 当前构建是否包含前端产物
const Enabled = true

// Handler 返回托管前端 SPA 的 http.Handler（配合 API handler 使用）
func Handler(apiHandler http.Handler) http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API、健康检查、指标与 OpenAPI 规范走后端
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" ||
			r.URL.Path == "/metrics" || r.URL.Path == "/openapi.yaml" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		// SPA 回退：非文件路径返回 index.html（支持前端路由刷新）
		if r.URL.Path != "/" {
			p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/")
			if _, err := fs.Stat(sub, p); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
