package mcp

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPConfig MCP over Streamable HTTP 的配置。
type HTTPConfig struct {
	// AllowKeyDownload 是否注册私钥下载工具（T13）。
	// HTTP 模式下 MCP Server 即 Argus 本体，能直接读到服务端开关——
	// 不像 M1 远程模式（REST 代理）无法确认而一律不注册。
	AllowKeyDownload bool
	// AuditKeyDownload 私钥下载审计钩子（见 Config.AuditKeyDownload）。
	// HTTP 模式下由 delivery 层注入；stdio 模式为 nil。
	AuditKeyDownload func(ctx context.Context, certID, format string)
}

// scopeDenyAll 占位 scope：不匹配任何工具所需 scope，也不等于 all/read。
// 用于 fail-closed——已认证但无法确定身份时不给任何工具，而不是放行全部。
const scopeDenyAll = "deny:all"

// scopesContextKey 从 HTTP 请求上下文取当前凭证的 scopes（由 delivery 层注入）。
type scopesContextKey struct{}

// WithScopes 把当前凭证的 scope 列表注入上下文，供 HTTP 传输按请求收敛工具集。
func WithScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesContextKey{}, scopes)
}

// scopesFromContext 取出注入的 scopes（无则 nil = 不过滤，仅本地 stdio 模式）。
func scopesFromContext(ctx context.Context) []string {
	s, _ := ctx.Value(scopesContextKey{}).([]string)
	return s
}

// serverCache 按「scope 组合 + 开关」缓存已装配的 Server。
//
// 动机：构造一个含 13 工具的 Server 要做 13 次 JSON Schema 推断，
// 实测约 1.7ms / 764KB 分配。HTTP 模式下每个 POST 都会走 getServer，
// 不缓存则等于每请求付出这笔开销（高并发下是持续的 GC 压力）。
// 而 scope 组合在实际部署中只有寥寥数个（all / read / 各资源的读写子集），
// 缓存命中率极高。条目数设上限，超出即整体重建，防止被异常多的 scope
// 组合撑爆内存。
type serverCache struct {
	mu      sync.Mutex
	entries map[string]*sdkmcp.Server
}

const serverCacheMaxEntries = 64

func (c *serverCache) get(key string, build func() *sdkmcp.Server) *sdkmcp.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*sdkmcp.Server, 8)
	}
	if s, ok := c.entries[key]; ok {
		return s
	}
	if len(c.entries) >= serverCacheMaxEntries {
		c.entries = make(map[string]*sdkmcp.Server, 8)
	}
	s := build()
	c.entries[key] = s
	return s
}

// cacheKey 由 scope 集合与开关生成稳定键（排序去重，保证同集合同键）。
func cacheKey(scopes []string, allowKeyDownload bool) string {
	cp := append([]string(nil), scopes...)
	sort.Strings(cp)
	var b strings.Builder
	for _, s := range cp {
		b.WriteString(s)
		b.WriteByte(',')
	}
	if allowKeyDownload {
		b.WriteString("|key=1")
	} else {
		b.WriteString("|key=0")
	}
	return b.String()
}

// NewHTTPHandler 装配 MCP over Streamable HTTP 的 http.Handler。
//
// 设计要点：
//   - Stateless：每个 POST 自包含，无会话亲和性，反向代理后可直接水平扩容；
//     代价是不支持 SSE 服务端推流（工具调用只需 JSON 响应，用不到）。
//   - JSONResponse：响应走 application/json 而非 SSE 帧，兼容性最好。
//   - getServer 按请求构造/取用 Server：从上下文取当前凭证的 scopes 过滤
//     工具集，实现"每个 token 只看到自己有权调用的工具"（与本地 --scopes
//     同机制）。scopes 为 nil（无主体）时按 deny:all 处理——fail-closed。
//   - Server 按 scope 组合缓存（见 serverCache），避免每请求重复做 schema 推断。
//   - DNS rebinding / 跨源防护由调用方包一层 http.NewCrossOriginProtection()
//     （Go 1.25+ 标准库）；SDK 自身的 localhost 防护保持默认开启。
func NewHTTPHandler(ops CertOps, version string, cfg HTTPConfig) http.Handler {
	cache := &serverCache{}
	return sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		scopes := scopesFromContext(r.Context())
		if scopes == nil {
			// fail-closed：拿不到主体就不给工具，而不是放行全部
			scopes = []string{scopeDenyAll}
		}
		key := cacheKey(scopes, cfg.AllowKeyDownload)
		return cache.get(key, func() *sdkmcp.Server {
			return NewServer(ops, version, Config{
				AllowKeyDownload: cfg.AllowKeyDownload,
				Scopes:           scopes,
				AuditKeyDownload: cfg.AuditKeyDownload,
			}).SDKServer()
		})
	}, &sdkmcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}
