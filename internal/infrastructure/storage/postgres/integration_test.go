package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

// TestPostgresIntegration 需要运行中的 PostgreSQL（docker compose 或本地）
// docker run -d --name argus-pg-test -e POSTGRES_PASSWORD=testpass -e POSTGRES_DB=argus -p 55432:5432 postgres:16-alpine
func TestPostgresIntegration(t *testing.T) {
	dsn := "postgres://postgres:testpass@localhost:55432/argus?sslmode=disable"
	db, err := Open(dsn)
	if err != nil {
		t.Skipf("postgres 不可用（%v），跳过集成测试", err)
	}
	t.Cleanup(func() { db.Close() })

	// 迁移幂等：再次 Open 不报错
	db2, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen after migrate: %v", err)
	}
	db2.Close()

	ctx := context.Background()

	// 清理历史测试数据（容器持久化）
	for _, table := range []string{"agent_tasks", "agents", "notification_logs", "audit_logs", "users"} {
		if _, err := db.ExecContext(ctx, "TRUNCATE TABLE "+table+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}

	// 用户 CRUD（含唯一约束、外键）
	users := sqlite.NewUserStore(db)
	u := &domain.User{
		ID: "u1", Username: "pguser", PasswordHash: "hash", Role: domain.RoleAdmin,
		Status: domain.UserActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	got, err := users.GetByUsername(ctx, "pguser")
	if err != nil || got.ID != "u1" {
		t.Fatalf("get user: %v %+v", err, got)
	}
	if n, _ := users.Count(ctx); n != 1 {
		t.Fatalf("count: %d", n)
	}
	// 唯一约束
	dup := *u
	dup.ID = "u2"
	if err := users.Create(ctx, &dup); err == nil {
		t.Fatal("expected unique violation on duplicate username")
	}

	// 审计（含 LIKE ESCAPE 筛选）
	audit := sqlite.NewAuditStore(db)
	if err := audit.Append(ctx, &domain.AuditLog{
		ID: "a1", Username: "pguser", Action: "login.success", Resource: "x", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	if err := audit.Append(ctx, &domain.AuditLog{
		ID: "a2", Username: "pguser", Action: "certificate.import", Resource: "a.example.com", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("append audit2: %v", err)
	}
	logs, err := audit.List(ctx, domain.AuditQuery{Action: "certificate."}, 10, 0)
	if err != nil || len(logs) != 1 {
		t.Fatalf("audit filter: %v len=%d", err, len(logs))
	}
	logs, err = audit.List(ctx, domain.AuditQuery{Keyword: "50%"}, 10, 0)
	if err != nil || len(logs) != 0 {
		t.Fatalf("audit escape: %v len=%d", err, len(logs))
	}
	if n, _ := audit.DeleteOlderThan(ctx, time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("audit delete older: %d", n)
	}

	// 通知防重（INSERT OR IGNORE → ON CONFLICT）
	notifyLogs := sqlite.NewNotificationLogStore(db)
	nl := &domain.NotificationLog{ID: "n1", CertID: "c1", RuleID: "r1", DaysBefore: 7, SentAt: time.Now()}
	if err := notifyLogs.Append(ctx, nl); err != nil {
		t.Fatalf("append notify log: %v", err)
	}
	if err := notifyLogs.Append(ctx, nl); err != nil {
		t.Fatalf("append duplicate notify log: %v", err)
	}
	sent, err := notifyLogs.HasSent(ctx, "c1", "r1", 7)
	if err != nil || !sent {
		t.Fatalf("has sent: %v %v", sent, err)
	}

	// Agent 领取任务（BeginTx 事务路径）
	agents := sqlite.NewAgentStore(db)
	ag := &domain.Agent{ID: "ag1", Name: "pg-agent", TokenHash: "th", Status: "offline",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), Enabled: true}
	if err := agents.Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	tasks := sqlite.NewAgentTaskStore(db)
	if err := tasks.Create(ctx, &domain.AgentTask{
		ID: "t1", AgentID: "ag1", CertificateID: "c1", Status: domain.AgentTaskPending, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := tasks.ClaimNext(ctx, "ag1")
	if err != nil || claimed == nil || claimed.ID != "t1" {
		t.Fatalf("claim next: %v %+v", err, claimed)
	}
	if claimed.Status != domain.AgentTaskRunning {
		t.Fatalf("expected running, got %s", claimed.Status)
	}

	// 并发领取竞态：两个连接同时 ClaimNext，仅一个成功
	if err := tasks.Create(ctx, &domain.AgentTask{
		ID: "t2", AgentID: "ag1", CertificateID: "c1", Status: domain.AgentTaskPending, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create task2: %v", err)
	}
	results := make(chan *domain.AgentTask, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			t, err := tasks.ClaimNext(context.Background(), "ag1")
			if err != nil {
				results <- nil
				return
			}
			results <- t
		}()
	}
	close(start)
	var claimedCount int
	for i := 0; i < 2; i++ {
		if t := <-results; t != nil {
			claimedCount++
		}
	}
	if claimedCount != 1 {
		t.Fatalf("expected exactly 1 task claimed under race, got %d", claimedCount)
	}
}
