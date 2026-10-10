package application

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestLoginLimiterConcurrent 并发读写不崩溃（-race 下验证无数据竞争）
func TestLoginLimiterConcurrent(t *testing.T) {
	limiter := NewLoginLimiter(5, 15*time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			user := fmt.Sprintf("user%d", n%10)
			ip := fmt.Sprintf("10.0.0.%d", n%5)
			_ = limiter.Check(user, ip)
			limiter.RecordFailure(user, ip)
			limiter.Reset(user, ip)
		}(i)
	}
	wg.Wait()
	// 状态一致：锁定后 Check 返回错误
	limiter.maxFailures = 2
	limiter.RecordFailure("u", "ip")
	limiter.RecordFailure("u", "ip")
	if err := limiter.Check("u", "ip"); err == nil {
		t.Fatal("expected lockout")
	}
	// 锁定过期后恢复
	limiter.locks["u@ip"] = time.Now().Add(-time.Minute)
	if err := limiter.Check("u", "ip"); err != nil {
		t.Fatalf("expected recovery after lockout expiry, got %v", err)
	}
}
