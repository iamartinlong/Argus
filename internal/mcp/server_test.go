package mcp

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newSmokeSession 在内存管道上建立 client↔server 会话（等价于 stdio 往返，
// 但免去 spawn 子进程——真实 stdio e2e 见 internal/tests/e2e/mcp_test.go）。
func newSmokeSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := NewServer(nil, "1.3.0-test", Config{})

	// 客户端写 → 服务端读；服务端写 → 客户端读
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

// M1a 冒烟：握手（client.Connect 内含 initialize 往返）+ ping + 空工具列表。
func TestM1aHandshakeAndEmptyToolList(t *testing.T) {
	cs := newSmokeSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := cs.Ping(ctx, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// M1a 为零工具骨架：tools/list 应为空（M1b 后改为 12，T13 视开关）
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 0 {
		t.Fatalf("expected 0 tools in M1a, got %d: %+v", len(tools.Tools), tools.Tools)
	}
}

// NewServer 必须装配出可用的 SDK Server（含 stderr Logger 配置）。
func TestM1aNewServerAssembles(t *testing.T) {
	server := NewServer(nil, "1.3.0-test", Config{AllowKeyDownload: true})
	if server == nil || server.sdk == nil {
		t.Fatal("server not assembled")
	}
	if server.cfg.AllowKeyDownload != true {
		t.Fatal("config not carried")
	}
}
