package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepathJoin(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func filepathJoin(elem ...string) string {
	s := elem[0]
	for _, e := range elem[1:] {
		s += "/" + e
	}
	return s
}

// TestMigrationIdempotent 重复 Open 迁移幂等
func TestMigrationIdempotent(t *testing.T) {
	path := filepathJoin(t.TempDir(), "mig.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// 再次打开（模拟并发启动的第二次迁移检查）不应报错
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after migrate: %v", err)
	}
	db2.Close()
}

func TestNotifyLogDedupe(t *testing.T) {
	db := newTestDB(t)
	store := NewNotificationLogStore(db)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if err := store.Append(ctx, &domain.NotificationLog{ID: "n1", CertID: "c1", RuleID: "r1", DaysBefore: 7, SentAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	sent, err := store.HasSent(ctx, "c1", "r1", 7)
	if err != nil || !sent {
		t.Fatalf("has sent: %v %v", sent, err)
	}
	// 不同天数 → 未发送
	sent, _ = store.HasSent(ctx, "c1", "r1", 30)
	if sent {
		t.Fatal("expected not sent for different days")
	}
	// 保留清理
	if n, err := store.DeleteOlderThan(ctx, now.Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("delete older: %d %v", n, err)
	}
	if n, err := store.DeleteOlderThan(ctx, now.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("delete older2: %d %v", n, err)
	}
}

func TestAgentClaimNext(t *testing.T) {
	db := newTestDB(t)
	agents := NewAgentStore(db)
	tasks := NewAgentTaskStore(db)
	ctx := context.Background()
	if err := agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "offline", CreatedAt: time.Now(), UpdatedAt: time.Now(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Create(ctx, &domain.AgentTask{ID: "t1", AgentID: "a1", CertificateID: "c1", Status: domain.AgentTaskPending, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	claimed, err := tasks.ClaimNext(ctx, "a1")
	if err != nil || claimed == nil || claimed.Status != domain.AgentTaskRunning {
		t.Fatalf("claim: %v %+v", err, claimed)
	}
	// 已领取后无 pending 任务
	again, err := tasks.ClaimNext(ctx, "a1")
	if err != nil || again != nil {
		t.Fatalf("second claim should be nil: %v %+v", err, again)
	}
	// 任务历史
	history, err := tasks.ListByAgent(ctx, "a1", 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history: %v len=%d", err, len(history))
	}
}

func TestAgentTaskMarkStaleTasks(t *testing.T) {
	db := newTestDB(t)
	tasks := NewAgentTaskStore(db)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-30 * time.Minute)

	// 超时的 running 任务（已领取未回执，应被标记并返回）
	_ = tasks.Create(ctx, &domain.AgentTask{ID: "t1", AgentID: "a1", CertificateID: "c1", TargetID: "g1",
		Status: domain.AgentTaskRunning, ClaimedAt: &old, CreatedAt: now})
	// 超时的 pending 任务（从未被领取，应被标记并返回）
	_ = tasks.Create(ctx, &domain.AgentTask{ID: "t4", AgentID: "a4", CertificateID: "c4", TargetID: "g4",
		Status: domain.AgentTaskPending, CreatedAt: old})
	// 未超时的 running 任务（不应返回）
	recent := now.Add(-time.Minute)
	_ = tasks.Create(ctx, &domain.AgentTask{ID: "t2", AgentID: "a2", CertificateID: "c2", TargetID: "g2",
		Status: domain.AgentTaskRunning, ClaimedAt: &recent, CreatedAt: now})
	// 新入队的 pending 任务（未超时，不受影响）
	_ = tasks.Create(ctx, &domain.AgentTask{ID: "t3", AgentID: "a3", CertificateID: "c3", TargetID: "g3",
		Status: domain.AgentTaskPending, CreatedAt: now})

	affected, err := tasks.MarkStaleTasks(ctx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(affected) != 2 {
		t.Fatalf("expected 2 affected (t1,t4), got %+v", affected)
	}
	got, _ := tasks.GetByID(ctx, "t1")
	if got.Status != domain.AgentTaskFailed || got.Message != "agent 超时未回执" {
		t.Fatalf("t1 not marked failed: %+v", got)
	}
	got4, _ := tasks.GetByID(ctx, "t4")
	if got4.Status != domain.AgentTaskFailed || got4.Message != "agent 未领取超时" {
		t.Fatalf("t4 not marked failed: %+v", got4)
	}
	got2, _ := tasks.GetByID(ctx, "t2")
	if got2.Status != domain.AgentTaskRunning {
		t.Fatalf("t2 should remain running: %+v", got2)
	}
	got3, _ := tasks.GetByID(ctx, "t3")
	if got3.Status != domain.AgentTaskPending {
		t.Fatalf("t3 should remain pending: %+v", got3)
	}
	// 再次调用无受影响任务
	again, err := tasks.MarkStaleTasks(ctx, 10*time.Minute)
	if err != nil || len(again) != 0 {
		t.Fatalf("second call: err=%v affected=%+v", err, again)
	}
}

func TestAgentMarkStaleOffline(t *testing.T) {
	db := newTestDB(t)
	agents := NewAgentStore(db)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-time.Hour)
	recent := &domain.Agent{ID: "a1", Name: "recent", TokenHash: "h1", Status: "online", Enabled: true, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now}
	stale := &domain.Agent{ID: "a2", Name: "stale", TokenHash: "h2", Status: "online", Enabled: true, LastSeenAt: &old, CreatedAt: now, UpdatedAt: now}
	disabled := &domain.Agent{ID: "a3", Name: "disabled", TokenHash: "h3", Status: "offline", Enabled: false, LastSeenAt: &old, CreatedAt: now, UpdatedAt: now}
	for _, a := range []*domain.Agent{recent, stale, disabled} {
		if err := agents.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	n, err := agents.MarkStaleOffline(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
	g, _ := agents.GetByID(ctx, "a2")
	if g.Status != "offline" {
		t.Fatalf("stale should be offline, got %s", g.Status)
	}
	r, _ := agents.GetByID(ctx, "a1")
	if r.Status != "online" {
		t.Fatalf("recent should stay online, got %s", r.Status)
	}
}

func TestDeployRecordMarkRetrying(t *testing.T) {
	db := newTestDB(t)
	store := NewDeployRecordStore(db)
	ctx := context.Background()
	rec := &domain.DeployRecord{
		ID: "d1", TargetID: "t", CertificateID: "c",
		Status: domain.DeployFailed, Retries: 1, CreatedAt: time.Now(),
	}
	if err := store.Create(ctx, rec); err != nil {
		t.Fatal(err)
	}
	// 抢占成功：failed→running, retries 2
	if err := store.MarkRetrying(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetByID(ctx, "d1")
	if got.Status != domain.DeployRunning || got.Retries != 2 {
		t.Fatalf("after mark: %+v", got)
	}
	// 非 failed 状态不可抢占
	if err := store.MarkRetrying(ctx, "d1"); err == nil {
		t.Fatal("expected error when not failed")
	}
	// 超限（retries=3）不可抢占
	rec2 := &domain.DeployRecord{
		ID: "d2", TargetID: "t", CertificateID: "c",
		Status: domain.DeployFailed, Retries: 3, CreatedAt: time.Now(),
	}
	_ = store.Create(ctx, rec2)
	if err := store.MarkRetrying(ctx, "d2"); err == nil {
		t.Fatal("expected error when retries exhausted")
	}
}

func TestDeployRecordMarkTimedOut(t *testing.T) {
	db := newTestDB(t)
	store := NewDeployRecordStore(db)
	ctx := context.Background()
	// running 记录（应被标记）
	rec := &domain.DeployRecord{
		ID: "d1", TargetID: "t1", CertificateID: "c1",
		Status: domain.DeployRunning, Retries: 1, CreatedAt: time.Now(),
	}
	_ = store.Create(ctx, rec)
	// 同 cert+target 但非 running（不应改）
	rec2 := &domain.DeployRecord{
		ID: "d2", TargetID: "t1", CertificateID: "c1",
		Status: domain.DeploySuccess, CreatedAt: time.Now(),
	}
	_ = store.Create(ctx, rec2)
	// 同 cert 不同 target（不应改）
	rec3 := &domain.DeployRecord{
		ID: "d3", TargetID: "t2", CertificateID: "c1",
		Status: domain.DeployRunning, CreatedAt: time.Now(),
	}
	_ = store.Create(ctx, rec3)

	n, err := store.MarkTimedOut(ctx, "c1", "t1", "agent 未领取超时")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 affected, got %d", n)
	}

	got, _ := store.GetByID(ctx, "d1")
	if got.Status != domain.DeployFailed || got.Retries != 3 || got.Message != "agent 未领取超时" {
		t.Fatalf("timed-out record: %+v", got)
	}
	got2, _ := store.GetByID(ctx, "d2")
	if got2.Status != domain.DeploySuccess {
		t.Fatalf("non-running record should be untouched: %+v", got2)
	}
	got3, _ := store.GetByID(ctx, "d3")
	if got3.Status != domain.DeployRunning {
		t.Fatalf("different target record should be untouched: %+v", got3)
	}
}

func TestChannelStoreCRUD(t *testing.T) {
	db := newTestDB(t)
	store := NewNotificationChannelStore(db)
	ctx := context.Background()
	c := &domain.NotificationChannelRecord{
		ID: "ch1", Name: "测试", Type: domain.ChannelWebhook,
		ConfigEnc: "enc:xxx", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByID(ctx, "ch1")
	if err != nil || got.Name != "测试" || !got.Enabled {
		t.Fatalf("get: %v %+v", err, got)
	}
	if n, _ := store.Count(ctx); n != 1 {
		t.Fatalf("count: %d", n)
	}
	c.Enabled = false
	if err := store.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(ctx, "ch1")
	if got.Enabled {
		t.Fatal("update failed")
	}
	if err := store.Delete(ctx, "ch1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByID(ctx, "ch1"); err != domain.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}
