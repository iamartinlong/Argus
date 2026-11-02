package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestDomainStoreCRUD(t *testing.T) {
	db := newTestDB(t)
	store := NewDomainStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	d := &domain.Domain{
		ID:             "d1",
		Name:           "www.example.com",
		ExpectedStatus: domain.ExpectedMustHaveCert,
		Source:         domain.DomainSourceManual,
		Note:           "主站",
		Tags:           []string{"prod", "web"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByID(ctx, "d1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != d.Name || got.ExpectedStatus != domain.ExpectedMustHaveCert || len(got.Tags) != 2 {
		t.Fatalf("got = %+v", got)
	}

	byName, err := store.GetByName(ctx, "www.example.com")
	if err != nil || byName.ID != "d1" {
		t.Fatalf("GetByName = %v, %v", byName, err)
	}

	// 唯一约束：同名第二个创建应失败
	dup := &domain.Domain{ID: "d2", Name: "www.example.com", ExpectedStatus: domain.ExpectedMustHaveCert,
		Source: domain.DomainSourceManual, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(ctx, dup); err == nil {
		t.Fatal("expected unique violation on duplicate name")
	}

	// 更新（含改名）
	got.Name = "new.example.com"
	got.ExpectedStatus = domain.ExpectedMonitorOnly
	got.UpdatedAt = now.Add(time.Hour)
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := store.GetByID(ctx, "d1")
	if after.Name != "new.example.com" || after.ExpectedStatus != domain.ExpectedMonitorOnly {
		t.Fatalf("after update = %+v", after)
	}

	list, err := store.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d items, err %v", len(list), err)
	}

	// 不存在
	if _, err := store.GetByID(ctx, "nope"); err != domain.ErrNotFound {
		t.Fatalf("GetByID(missing) err = %v, want not_found", err)
	}
	if err := store.Delete(ctx, "nope"); err != domain.ErrNotFound {
		t.Fatalf("Delete(missing) err = %v, want not_found", err)
	}
	if err := store.Delete(ctx, "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if list, _ := store.List(ctx); len(list) != 0 {
		t.Fatalf("List after delete = %d, want 0", len(list))
	}
}

// TestCertificateDomainColumns 验证 certificates 新列可读写（迁移 19）
func TestCertificateDomainColumns(t *testing.T) {
	db := newTestDB(t)
	store := NewCertificateStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	c := &domain.Certificate{
		ID:            "c-dom",
		Domains:       []string{"www.example.com"},
		PrimaryDomain: "www.example.com",
		Provider:      "import",
		Status:        domain.StatusValid,
		DomainID:      "d1",
		DomainName:    "www.example.com",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.GetByID(ctx, "c-dom")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DomainID != "d1" || got.DomainName != "www.example.com" {
		t.Fatalf("domain cols = %q/%q", got.DomainID, got.DomainName)
	}
}
