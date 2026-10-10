package httpapi

import "net/http"

// securityHeaders 全局安全响应头
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// 防 MIME 嗅探（PEM/CSV 下载场景）
		h.Set("X-Content-Type-Options", "nosniff")
		// 防点击劫持（SPA 无第三方嵌入场景）
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// HSTS：浏览器仅在 HTTPS 连接中生效，HTTP 部署无副作用
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// CSP：同源资源；允许内联样式（UI 库）；禁止 frame 嵌入
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; font-src 'self' data:; frame-ancestors 'none'; "+
				"connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
