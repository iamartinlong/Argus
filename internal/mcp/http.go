package mcp

import (
	"context"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPConfig MCP over Streamable HTTP 的配置。
type HTTPConfig struct {
	// AllowKeyDownload 是否注册私钥下载工具（T13）。
	// HTTP 模式下 MCP Server 即 Argus 本体，能直接读到服务端开关——
	// 不像 M1 远程模式（REST 代理）无法确认而一律不注册。
	AllowKeyDownload bool
}

// scopesContextKey 从 HTTP 请求上下文取当前凭证的 scopes（由 delivery 层注入）。
type scopesContextKey struct{}

// WithScopes 把当前凭证的 scope 列表注入上下文，供 HTTP 传输按请求收敛工具集。
func WithScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesContextKey{}, scopes)
}

// scopesFromContext 取出注入的 scopes（无则 nil = 不过滤）。
func scopesFromContext(ctx context.Context) []string {
	s, _ := ctx.Value(scopesContextKey{}).([]string)
	return s
}

// NewHTTPHandler 装配 MCP over Streamable HTTP 的 http.Handler。
//
// 设计要点：
//   - Stateless：每个 POST 自包含，无会话亲和性，反向代理后可直接水平扩容；
//     代价是不支持 SSE 服务端推流（工具调用只需 JSON 响应，用不到）。
//   - JSONResponse：响应走 application/json 而非 SSE 帧，兼容性最好。
//   - getServer 按请求构造 Server：从上下文取当前凭证的 scopes 过滤工具集，
//     实现"每个 token 只看到自己有权调用的工具"（与本地 --scopes 同机制）。
//   - DNS rebinding / 跨源防护由调用方包一层 http.NewCrossOriginProtection()
//     （Go 1.25+ 标准库）；SDK 自身的 localhost 防护保持默认开启。
//
// 用法（delivery 层）：
//
//	protected := http.NewCrossOriginProtection().Handler(mcp.NewHTTPHandler(ops, version, cfg))
//	r.Mount("/mcp", protected)
func NewHTTPHandler(ops CertOps, version string, cfg HTTPConfig) http.Handler {
	return sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return NewServer(ops, version, Config{
			AllowKeyDownload: cfg.AllowKeyDownload,
			Scopes:           scopesFromContext(r.Context()),
		}).SDKServer()
	}, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}
