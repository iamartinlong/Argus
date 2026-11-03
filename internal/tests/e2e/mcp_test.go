//go:build integration

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPStdioEndToEnd spawn 真实 argus mcp 二进制，走 stdio 协议：
// initialize → tools/list（12 个，T13 缺席）→ tools/call（list/缺参错误）。
func TestMCPStdioEndToEnd(t *testing.T) {
	exe := os.Getenv("ARGUS_E2E_BIN")
	if exe == "" {
		t.Skip("ARGUS_E2E_BIN 未设置（交叉编译场景跳过）")
	}
	work := t.TempDir()
	cfg := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(cfg, []byte("server:\n  listen: \"127.0.0.1:0\"\nstorage:\n  driver: sqlite\n  dsn: "+filepath.Join(work, "argus.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(exe, "mcp", "--config", cfg)
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	// initialize 往返已完成（Connect 内含）；tools/list 断言 12 个且 T13 缺席
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(tools.Tools) != 12 {
		t.Fatalf("expected 12 tools, got %d", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Name == "download_certificate" {
			t.Fatal("download_certificate must not be registered by default")
		}
		if tool.Description == "" {
			t.Fatalf("tool %s missing description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %s missing input schema", tool.Name)
		}
	}

	// tools/call：只读工具应成功返回（空库为空列表）
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_certificates", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call list_certificates: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_certificates unexpected error: %+v", res.Content)
	}

	// 缺少必填参数 → 工具级错误（不崩溃、不产生副作用）
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "issue_certificate", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call issue_certificate: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for missing required params")
	}
}
