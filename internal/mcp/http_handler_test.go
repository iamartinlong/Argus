package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------- WithScopes / scopesFromContext ----------

func TestWithScopesRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := scopesFromContext(ctx); got != nil {
		t.Fatalf("未注入时应为 nil，got %v", got)
	}
	want := []string{"certificates:read", "deploy:write"}
	got := scopesFromContext(WithScopes(ctx, want))
	if len(got) != len(want) {
		t.Fatalf("round-trip 长度不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round-trip 内容不符: %v", got)
		}
	}
	// 注入 nil 与"未注入"可区分：nil 表示未设置（HTTP 层据此 fail-closed）
	if scopesFromContext(WithScopes(ctx, nil)) != nil {
		t.Fatal("显式注入 nil 应读回 nil")
	}
}

// ---------- NewHTTPHandler：httptest 直连（无需起整个服务端） ----------

// startMCPHTTP 起一个仅含 MCP handler 的 httptest 服务端。
// scopes 为 nil 表示不注入（走 fail-closed 路径）。
func startMCPHTTP(t *testing.T, cfg HTTPConfig, scopes []string) (*httptest.Server, *mcpClientTrace) {
	t.Helper()
	h := NewHTTPHandler(stubOps{}, "1.4.0", cfg)
	trace := &mcpClientTrace{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if scopes != nil {
			ctx = WithScopes(ctx, scopes)
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	_ = trace
	return srv, trace
}

type mcpClientTrace struct{}

// connectAndList 连接并返回工具名集合
func connectAndList(t *testing.T, url string, scopes []string) (map[string]bool, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             url,
		DisableStandaloneSSE: true,
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "ut", Version: "0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	return names, nil
}

// 注入全量 scope → 12 工具
func TestNewHTTPHandlerAllScopes(t *testing.T) {
	srv, _ := startMCPHTTP(t, HTTPConfig{}, []string{"all"})
	names, err := connectAndList(t, srv.URL, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(names) != 12 {
		t.Fatalf("期望 12 工具，got %d: %v", len(names), names)
	}
	if !names["issue_certificate"] || !names["list_certificates"] {
		t.Fatal("全量 scope 应同时含读与写工具")
	}
}

// 注入只读 scope → 只剩只读工具
func TestNewHTTPHandlerReadOnlyScopes(t *testing.T) {
	srv, _ := startMCPHTTP(t, HTTPConfig{}, []string{"certificates:read", "deploy:read"})
	names, err := connectAndList(t, srv.URL, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("只读 scope 应有工具")
	}
	for banned := range names {
		switch banned {
		case "issue_certificate", "renew_certificate", "revoke_certificate",
			"import_certificate", "deploy_certificate", "test_deploy_target":
			t.Fatalf("只读 scope 不应出现写工具 %s", banned)
		}
	}
}

// 不注入 scopes（nil）→ fail-closed，一个工具都没有
func TestNewHTTPHandlerNilScopesFailsClosed(t *testing.T) {
	srv, _ := startMCPHTTP(t, HTTPConfig{}, nil)
	names, err := connectAndList(t, srv.URL, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("无主体时应零工具（fail-closed），got %d: %v", len(names), names)
	}
}

// allow_key_download 开关经 HTTPConfig 贯通到工具注册
func TestNewHTTPHandlerKeyDownloadToggle(t *testing.T) {
	on, _ := startMCPHTTP(t, HTTPConfig{AllowKeyDownload: true}, []string{"all"})
	namesOn, err := connectAndList(t, on.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !namesOn["download_certificate"] {
		t.Fatal("AllowKeyDownload=true 应注册 download_certificate")
	}

	off, _ := startMCPHTTP(t, HTTPConfig{AllowKeyDownload: false}, []string{"all"})
	namesOff, err := connectAndList(t, off.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if namesOff["download_certificate"] {
		t.Fatal("AllowKeyDownload=false 不应注册 download_certificate")
	}
}

// 同 scope 组合的并发请求应复用同一 Server（缓存生效且无竞争）
func TestNewHTTPHandlerConcurrentSameScopes(t *testing.T) {
	srv, _ := startMCPHTTP(t, HTTPConfig{}, []string{"all"})
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := connectAndList(t, srv.URL, nil)
			done <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发请求失败: %v", err)
		}
	}
}

// 工具真能调通（HTTP 全链路）
func TestNewHTTPHandlerToolCall(t *testing.T) {
	srv, _ := startMCPHTTP(t, HTTPConfig{}, []string{"all"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		DisableStandaloneSSE: true,
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "ut", Version: "0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "list_certificates",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_certificates 不应报错: %+v", res.Content)
	}
}
