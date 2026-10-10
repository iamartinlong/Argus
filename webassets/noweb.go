//go:build noui

package webassets

import "net/http"

// Enabled 当前构建是否包含前端产物（noui 构建无 UI）
const Enabled = false

// Handler 无 UI 构建：仅透传 API handler
func Handler(apiHandler http.Handler) http.Handler {
	return apiHandler
}
