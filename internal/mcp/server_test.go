package mcp

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iamartinlong/Argus/internal/domain"
)

// stubOps 测试用 CertOps：全部返回空结果（工具装配/过滤测试不需要真实业务）。
type stubOps struct{}

func (stubOps) ListCertificates(context.Context, ListFilter) ([]*domain.Certificate, error) {
	return nil, nil
}
func (stubOps) GetCertificate(context.Context, string, string) (*domain.Certificate, error) {
	return nil, domain.ErrNotFound
}
func (stubOps) IssueCertificate(context.Context, IssueParams) (*domain.Certificate, error) {
	return nil, domain.ErrInvalidArgument
}
func (stubOps) RenewCertificate(context.Context, string, string) (*domain.Certificate, error) {
	return nil, domain.ErrInvalidArgument
}
func (stubOps) RevokeCertificate(context.Context, string, string) error {
	return domain.ErrInvalidArgument
}
func (stubOps) ImportCertificate(context.Context, ImportParams) (*domain.Certificate, error) {
	return nil, domain.ErrInvalidArgument
}
func (stubOps) ListTargets(context.Context) ([]*domain.DeployTarget, error) { return nil, nil }
func (stubOps) DeployCertificate(context.Context, string, string, []string) error {
	return domain.ErrInvalidArgument
}
func (stubOps) TestTarget(context.Context, string) error { return domain.ErrInvalidArgument }
func (stubOps) ListAccounts(context.Context, string) ([]AccountBrief, error) {
	return nil, nil
}
func (stubOps) ListAgents(context.Context) ([]*domain.Agent, error) { return nil, nil }
func (stubOps) TaskLogs(context.Context, int) ([]*domain.TaskLog, error) {
	return nil, nil
}
func (stubOps) DownloadCertificate(context.Context, string, string) ([]byte, string, error) {
	return nil, "", domain.ErrForbidden
}

// newSmokeSession 在内存管道上建立 client↔server 会话。
func newSmokeSession(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	server := NewServer(stubOps{}, "1.3.0-test", cfg)

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	clientTransport := &mcp.IOTransport{Reader: c2sR, Writer: s2cW}
	serverTransport := &mcp.IOTransport{Reader: s2cR, Writer: c2sW}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		if err := server.sdk.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "smoke-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// M1b：默认注册 12 个工具（T13 私钥下载默认不注册）。
func TestDefaultRegisters12Tools(t *testing.T) {
	names := toolNames(t, newSmokeSession(t, Config{}))
	if len(names) != 12 {
		t.Fatalf("expected 12 tools, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if n == "download_certificate" {
			t.Fatal("download_certificate must NOT be registered by default")
		}
	}
}

// T13 仅在 AllowKeyDownload 开启时注册。
func TestDownloadToolGatedByConfig(t *testing.T) {
	names := toolNames(t, newSmokeSession(t, Config{AllowKeyDownload: true}))
	if len(names) != 13 {
		t.Fatalf("expected 13 tools with allow_key_download, got %d: %v", len(names), names)
	}
	found := false
	for _, n := range names {
		if n == "download_certificate" {
			found = true
		}
	}
	if !found {
		t.Fatal("download_certificate should be registered when allow_key_download=true")
	}
}

// 本地模式 --scopes 收敛：只读 scope 下写工具不出现。
func TestScopeFilteringReadOnly(t *testing.T) {
	names := toolNames(t, newSmokeSession(t, Config{Scopes: []string{"certificates:read"}}))
	if len(names) == 0 {
		t.Fatal("expected some read tools")
	}
	for _, n := range names {
		switch n {
		case "issue_certificate", "renew_certificate", "revoke_certificate", "import_certificate":
			t.Fatalf("write tool %s must not appear with read-only scopes", n)
		}
	}
	// 证书只读工具应在
	if !contains(names, "list_certificates") {
		t.Fatalf("list_certificates should be present, got %v", names)
	}
}

// all scope 注册全部（含写操作）。
func TestScopeAllRegistersEverything(t *testing.T) {
	names := toolNames(t, newSmokeSession(t, Config{Scopes: []string{"all"}}))
	if len(names) != 12 {
		t.Fatalf("expected 12 tools with all scope, got %d: %v", len(names), names)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// NewServer 必须装配出可用的 SDK Server（含 stderr Logger 配置）。
func TestNewServerAssembles(t *testing.T) {
	server := NewServer(stubOps{}, "1.3.0-test", Config{AllowKeyDownload: true})
	if server == nil || server.sdk == nil {
		t.Fatal("server not assembled")
	}
	if !server.cfg.AllowKeyDownload {
		t.Fatal("config not carried")
	}
}

// 工具调用走 stub ops：list_certificates 应返回空列表而非报错。
func TestToolCallListCertificates(t *testing.T) {
	cs := newSmokeSession(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_certificates",
		Arguments: map[string]any{"status": "expiring"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
}

// 缺少必填参数应返回工具级错误（IsError），而非崩溃。
func TestToolCallMissingParams(t *testing.T) {
	cs := newSmokeSession(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_certificate",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for missing id/domain")
	}
}
