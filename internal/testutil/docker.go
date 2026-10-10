package testutil

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// DockerAvailable 检测 Docker 是否可用；SKIP_DOCKER 环境变量可强制跳过。
func DockerAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SKIP_DOCKER") != "" {
		return false
	}
	out, err := exec.Command("docker", "info").CombinedOutput()
	if err != nil {
		t.Logf("docker 不可用，跳过：%s", strings.TrimSpace(string(out)))
		return false
	}
	return true
}

// RunDocker 执行 docker 命令，失败即终止测试。
func RunDocker(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v 失败: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// WaitPort 轮询等待端口就绪。
func WaitPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("端口 %s 未在 %v 内就绪", addr, timeout)
}
