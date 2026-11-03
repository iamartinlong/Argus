package mcp

import (
	"testing"
	"time"
)

// BenchmarkNewServerAllTools 量化 HTTP 模式下每请求重建 Server 的成本
// （getServer 钩子每次 POST 都会走一遍，含 13 个工具的 schema 推断）。
func BenchmarkNewServerAllTools(b *testing.B) {
	ops := stubOps{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewServer(ops, "1.4.0", Config{})
		if s == nil {
			b.Fatal("nil server")
		}
	}
}

// BenchmarkNewServerScoped 带 scope 收敛时的成本（工具更少）
func BenchmarkNewServerScoped(b *testing.B) {
	ops := stubOps{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewServer(ops, "1.4.0", Config{Scopes: []string{"certificates:read"}})
		if s == nil {
			b.Fatal("nil server")
		}
	}
}

// TestNewServerCostSanity 单次构造应在毫秒级（防止回归到不可用）
func TestNewServerCostSanity(t *testing.T) {
	ops := stubOps{}
	start := time.Now()
	const n = 20
	for i := 0; i < n; i++ {
		NewServer(ops, "1.4.0", Config{})
	}
	per := time.Since(start) / n
	t.Logf("每次构造 Server（13 工具）平均耗时: %v", per)
	if per > 50*time.Millisecond {
		t.Errorf("单次构造 %v 过慢，HTTP 模式下每请求都会付出该代价", per)
	}
}
