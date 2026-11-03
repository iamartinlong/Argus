package mcp

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iamartinlong/Argus/internal/domain"
)

// ---------- Server 缓存（HTTP 模式每请求都会走 getServer） ----------

func TestServerCacheReuse(t *testing.T) {
	c := &serverCache{}
	var builds int
	build := func() *sdkmcp.Server {
		builds++
		return NewServer(stubOps{}, "1.4.0", Config{}).SDKServer()
	}

	s1 := c.get("a", build)
	s2 := c.get("a", build)
	if s1 != s2 {
		t.Fatal("同一 key 应返回缓存实例")
	}
	if builds != 1 {
		t.Fatalf("同 key 只应构造一次，实际 %d 次", builds)
	}

	s3 := c.get("b", build)
	if s3 == s1 {
		t.Fatal("不同 key 应返回不同实例")
	}
	if builds != 2 {
		t.Fatalf("不同 key 应重新构造，实际 %d 次", builds)
	}
}

func TestCacheKeyStable(t *testing.T) {
	// 同集合不同顺序 → 同键（保证缓存命中）
	if cacheKey([]string{"b", "a"}, false) != cacheKey([]string{"a", "b"}, false) {
		t.Error("scope 顺序不应影响缓存键")
	}
	// 开关不同 → 不同键（T13 开/关是两套工具集）
	if cacheKey([]string{"a"}, true) == cacheKey([]string{"a"}, false) {
		t.Error("allow_key_download 应参与缓存键")
	}
	// 空集合有稳定键（非空串）
	if cacheKey(nil, false) == "" {
		t.Error("空 scope 集合也应有稳定键")
	}
}

// 超出条目上限后应整体重建而非无限增长（防异常多的 scope 组合撑爆内存）
func TestServerCacheBounded(t *testing.T) {
	c := &serverCache{}
	build := func() *sdkmcp.Server { return NewServer(stubOps{}, "1.4.0", Config{}).SDKServer() }
	for i := 0; i < serverCacheMaxEntries+10; i++ {
		c.get(string(rune('a'+i%26))+string(rune('a'+i/26)), build)
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > serverCacheMaxEntries {
		t.Fatalf("缓存条目 %d 超过上限 %d", n, serverCacheMaxEntries)
	}
}

// 并发取同一 key 应安全（sync.Mutex 保护，无数据竞争）
func TestServerCacheConcurrent(t *testing.T) {
	c := &serverCache{}
	build := func() *sdkmcp.Server { return NewServer(stubOps{}, "1.4.0", Config{}).SDKServer() }
	var wg sync.WaitGroup
	got := make([]*sdkmcp.Server, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = c.get("same", build)
		}(i)
	}
	wg.Wait()
	for i := 1; i < len(got); i++ {
		if got[i] != got[0] {
			t.Fatal("并发同 key 应得到同一实例")
		}
	}
}

// ---------- fail-closed：无主体时不放行全部工具 ----------

// scopes 为 nil 时 HTTP 层会替换为 deny:all，此时一个工具都不该注册
func TestHTTPScopesNilFailsClosed(t *testing.T) {
	server := NewServer(stubOps{}, "1.4.0", Config{Scopes: []string{scopeDenyAll}})
	if server == nil {
		t.Fatal("nil server")
	}
	// deny:all 不匹配任何工具所需 scope，也不等于 all/read
	if hasScope([]string{scopeDenyAll}, domain.ScopeCertRead) {
		t.Fatal("deny:all 不应放行只读工具")
	}
	if hasScope([]string{scopeDenyAll}, domain.ScopeAdminWrite) {
		t.Fatal("deny:all 不应放行管理写工具")
	}
}

// ---------- T13 审计钩子 ----------

// downloadAuditOps 记录 DownloadCertificate 调用，并返回成功结果
type downloadAuditOps struct {
	stubOps
	gotID     string
	gotFormat string
}

func (o *downloadAuditOps) DownloadCertificate(ctx context.Context, id, format string) ([]byte, string, error) {
	o.gotID, o.gotFormat = id, format
	return []byte("zipbytes"), "a.example.com-nginx.zip", nil
}

// 审计钩子必须在 T13 成功路径被调用，并带上 certID 与 format
func TestAuditKeyDownloadHookFires(t *testing.T) {
	ops := &downloadAuditOps{}
	var hookCertID, hookFormat string
	var hookCalled bool

	server := NewServer(ops, "1.4.0", Config{
		AllowKeyDownload: true,
		AuditKeyDownload: func(ctx context.Context, certID, format string) {
			hookCalled = true
			hookCertID, hookFormat = certID, format
		},
	})

	// 内存管道建会话，走真实 tools/call
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.sdk.Run(ctx, &sdkmcp.IOTransport{Reader: s2cR, Writer: c2sW}) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: c2sR, Writer: s2cW}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	res, err := cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "download_certificate",
		Arguments: map[string]any{"id": "cert-123", "format": "nginx"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("download 不应报错: %+v", res.Content)
	}
	if ops.gotID != "cert-123" || ops.gotFormat != "nginx" {
		t.Fatalf("ops 未收到正确参数: %q/%q", ops.gotID, ops.gotFormat)
	}
	if !hookCalled {
		t.Fatal("审计钩子未被调用——私钥下载必须留痕")
	}
	if hookCertID != "cert-123" || hookFormat != "nginx" {
		t.Fatalf("钩子参数不符: %q/%q", hookCertID, hookFormat)
	}
}

// T13 失败时不应写审计（避免把失败尝试也记成"已下载"）
func TestAuditKeyDownloadHookNotFiredOnError(t *testing.T) {
	var called bool
	server := NewServer(stubOps{}, "1.4.0", Config{
		AllowKeyDownload: true,
		AuditKeyDownload: func(context.Context, string, string) { called = true },
	})
	// stubOps.DownloadCertificate 返回 ErrForbidden → 工具报错 → 钩子不触发
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.sdk.Run(ctx, &sdkmcp.IOTransport{Reader: s2cR, Writer: c2sW}) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: c2sR, Writer: s2cW}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	res, err := cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "download_certificate",
		Arguments: map[string]any{"id": "x", "format": "nginx"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatal("stub 的 DownloadCertificate 应报错")
	}
	if called {
		t.Fatal("失败路径不应写审计")
	}
}

// 未开 AllowKeyDownload 时钩子根本不应被接线（工具未注册）
func TestAuditHookAbsentWhenToolNotRegistered(t *testing.T) {
	var called bool
	server := NewServer(stubOps{}, "1.4.0", Config{
		AllowKeyDownload: false, // T13 不注册
		AuditKeyDownload: func(context.Context, string, string) { called = true },
	})
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.sdk.Run(ctx, &sdkmcp.IOTransport{Reader: s2cR, Writer: c2sW}) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: c2sR, Writer: s2cW}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	_, err = cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "download_certificate",
		Arguments: map[string]any{"id": "x", "format": "nginx"},
	})
	if err == nil {
		t.Fatal("未注册的工具应报错")
	}
	if called {
		t.Fatal("工具未注册时钩子不应被调用")
	}
}
