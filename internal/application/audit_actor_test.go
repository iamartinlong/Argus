package application

import (
	"context"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// resolveActor：显式 actor 优先于上下文
func TestResolveActorExplicitWins(t *testing.T) {
	explicit := &domain.User{ID: "u1", Username: "explicit"}
	ctx := domain.WithActor(context.Background(), &domain.User{ID: "u2", Username: "fromctx"})
	got := resolveActor(ctx, explicit)
	if got == nil || got.ID != "u1" {
		t.Fatalf("显式 actor 应优先，got %+v", got)
	}
}

// 无显式 actor 时回退到上下文（T11 的核心机制）
func TestResolveActorFallsBackToContext(t *testing.T) {
	ctx := domain.WithActor(context.Background(), &domain.User{ID: "u2", Username: "fromctx"})
	got := resolveActor(ctx, nil)
	if got == nil || got.ID != "u2" {
		t.Fatalf("应回退到上下文 actor，got %+v", got)
	}
}

// 两者皆无 → nil（调用方记 system）
func TestResolveActorNone(t *testing.T) {
	if got := resolveActor(context.Background(), nil); got != nil {
		t.Fatalf("无 actor 应为 nil，got %+v", got)
	}
}

// auditFields：有 actor 用其 ID/用户名，无则 system
func TestAuditFields(t *testing.T) {
	id, name := auditFields(&domain.User{ID: "u1", Username: "alice"})
	if id != "u1" || name != "alice" {
		t.Fatalf("got %q/%q", id, name)
	}
	id, name = auditFields(nil)
	if id != "" || name != "system" {
		t.Fatalf("nil actor 应为 system，got %q/%q", id, name)
	}
}

// ---------- 端到端：证书操作的审计确实带上上下文中的用户 ----------

// recordingAudit 捕获写入的审计日志
type recordingAudit struct {
	logs []*domain.AuditLog
}

func (r *recordingAudit) Append(_ context.Context, l *domain.AuditLog) error {
	r.logs = append(r.logs, l)
	return nil
}
func (r *recordingAudit) Count(context.Context, domain.AuditQuery) (int64, error) { return 0, nil }
func (r *recordingAudit) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *recordingAudit) List(context.Context, domain.AuditQuery, int, int) ([]*domain.AuditLog, error) {
	return r.logs, nil
}

// 用 auditAction 直接验证：上下文有 actor 时不再记 system
func TestCertificateAuditActionUsesContextActor(t *testing.T) {
	rec := &recordingAudit{}
	svc := &CertificateService{audit: rec, clock: fixedClock{}}

	// 无 actor：记 system（调度器等无人格触发源）
	svc.auditAction(context.Background(), nil, domain.AuditCertIssue, "a.example.com", "")
	if len(rec.logs) != 1 {
		t.Fatalf("应有 1 条审计，got %d", len(rec.logs))
	}
	if rec.logs[0].Username != "system" || rec.logs[0].UserID != "" {
		t.Fatalf("无 actor 应记 system，got %q/%q", rec.logs[0].Username, rec.logs[0].UserID)
	}

	// 上下文有 actor：记真实用户（T11 修复点）
	ctx := domain.WithActor(context.Background(), &domain.User{ID: "u9", Username: "carol", Role: domain.RoleOperator})
	svc.auditAction(ctx, nil, domain.AuditCertRevoke, "a.example.com", "")
	if len(rec.logs) != 2 {
		t.Fatalf("应有 2 条审计，got %d", len(rec.logs))
	}
	last := rec.logs[1]
	if last.Username != "carol" || last.UserID != "u9" {
		t.Fatalf("应记上下文中的用户，got %q/%q", last.Username, last.UserID)
	}
	if last.Action != domain.AuditCertRevoke || last.Resource != "a.example.com" {
		t.Fatalf("审计内容不应被改写: %+v", last)
	}
}

// 显式 actor 仍然优先（DomainService 的既有用法不能被破坏）
func TestCertificateAuditActionExplicitActorWins(t *testing.T) {
	rec := &recordingAudit{}
	svc := &CertificateService{audit: rec, clock: fixedClock{}}
	ctx := domain.WithActor(context.Background(), &domain.User{ID: "ctx", Username: "fromctx"})
	svc.auditAction(ctx, &domain.User{ID: "exp", Username: "explicit"}, domain.AuditCertDelete, "b.example.com", "")
	if rec.logs[0].Username != "explicit" || rec.logs[0].UserID != "exp" {
		t.Fatalf("显式 actor 应优先，got %q/%q", rec.logs[0].Username, rec.logs[0].UserID)
	}
}

// DeployService.auditAction 同样受益
func TestDeployAuditActionUsesContextActor(t *testing.T) {
	rec := &recordingAudit{}
	svc := &DeployService{audit: rec, clock: fixedClock{}}
	ctx := domain.WithActor(context.Background(), &domain.User{ID: "u5", Username: "dave"})
	svc.auditAction(ctx, "deploy.success", "c.example.com")
	if len(rec.logs) != 1 {
		t.Fatalf("应有 1 条审计，got %d", len(rec.logs))
	}
	if rec.logs[0].Username != "dave" || rec.logs[0].UserID != "u5" {
		t.Fatalf("部署审计应记上下文用户，got %q/%q", rec.logs[0].Username, rec.logs[0].UserID)
	}
}
