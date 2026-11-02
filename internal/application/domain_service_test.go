package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeDomainStore 内存台账存储（带互斥，模拟并发查重）
type fakeDomainStore struct {
	mu     sync.Mutex
	byID   map[string]*domain.Domain
	byName map[string]*domain.Domain
}

func newFakeDomainStore() *fakeDomainStore {
	return &fakeDomainStore{byID: map[string]*domain.Domain{}, byName: map[string]*domain.Domain{}}
}

func (s *fakeDomainStore) Create(_ context.Context, d *domain.Domain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[d.Name]; ok {
		return errors.New("UNIQUE constraint failed: domains.name")
	}
	s.byID[d.ID] = d
	s.byName[d.Name] = d
	return nil
}

func (s *fakeDomainStore) GetByID(_ context.Context, id string) (*domain.Domain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.byID[id]; ok {
		return d, nil
	}
	return nil, domain.ErrNotFound
}

func (s *fakeDomainStore) GetByName(_ context.Context, name string) (*domain.Domain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.byName[name]; ok {
		return d, nil
	}
	return nil, domain.ErrNotFound
}

func (s *fakeDomainStore) List(_ context.Context) ([]*domain.Domain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.Domain, 0, len(s.byID))
	for _, d := range s.byID {
		out = append(out, d)
	}
	return out, nil
}

func (s *fakeDomainStore) Update(_ context.Context, d *domain.Domain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[d.ID]; !ok {
		return domain.ErrNotFound
	}
	delete(s.byName, s.byID[d.ID].Name)
	s.byID[d.ID] = d
	s.byName[d.Name] = d
	return nil
}

func (s *fakeDomainStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	delete(s.byID, id)
	delete(s.byName, d.Name)
	return nil
}

func newTestDomainService() (*DomainService, *fakeDomainStore, *fakeAuditStore) {
	store := newFakeDomainStore()
	audit := &fakeAuditStore{}
	svc := NewDomainService(store, nil, audit, fixedClock{time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	return svc, store, audit
}

func TestDomainServiceCreateNormalizesAndDedupes(t *testing.T) {
	svc, _, _ := newTestDomainService()
	d, err := svc.Create(context.Background(), nil, DomainInput{Name: "  WWW.Example.COM. "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d.Name != "www.example.com" {
		t.Fatalf("name = %q, want www.example.com", d.Name)
	}
	if d.ExpectedStatus != domain.ExpectedMustHaveCert {
		t.Fatalf("expected = %q, want must_have_cert", d.ExpectedStatus)
	}
	if d.Source != domain.DomainSourceManual {
		t.Fatalf("source = %q, want manual", d.Source)
	}

	_, err = svc.Create(context.Background(), nil, DomainInput{Name: "www.example.com"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate create err = %v, want conflict", err)
	}

	_, err = svc.Create(context.Background(), nil, DomainInput{Name: "bad; rm -rf /"})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid domain err = %v, want invalid_argument", err)
	}
}

func TestDomainServiceUpdateRenameChecksConflict(t *testing.T) {
	svc, _, _ := newTestDomainService()
	a, _ := svc.Create(context.Background(), nil, DomainInput{Name: "a.example.com"})
	b, _ := svc.Create(context.Background(), nil, DomainInput{Name: "b.example.com"})

	// 改名撞已有域名 → 409
	if _, err := svc.Update(context.Background(), nil, a.ID, DomainInput{Name: "b.example.com"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("rename conflict err = %v, want conflict", err)
	}
	// 正常改名 + 改期望状态 + 标签去重
	updated, err := svc.Update(context.Background(), nil, a.ID, DomainInput{
		Name: "A2.Example.com", ExpectedStatus: "monitor_only", Note: " 备注 ", Tags: []string{"prod", "prod", " "},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "a2.example.com" || updated.ExpectedStatus != domain.ExpectedMonitorOnly {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.Note != "备注" || len(updated.Tags) != 1 || updated.Tags[0] != "prod" {
		t.Fatalf("note/tags = %q/%v", updated.Note, updated.Tags)
	}
	// 更新不存在 → 404
	if _, err := svc.Update(context.Background(), nil, "nope", DomainInput{Name: "x.com"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update missing err = %v, want not_found", err)
	}
	_ = b
}

func TestDomainServiceDeleteAndAudit(t *testing.T) {
	svc, _, audit := newTestDomainService()
	d, _ := svc.Create(context.Background(), nil, DomainInput{Name: "del.example.com"})
	if err := svc.Delete(context.Background(), nil, d.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(context.Background(), nil, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete twice err = %v, want not_found", err)
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	var created, deleted bool
	for _, l := range audit.logs {
		switch l.Action {
		case domain.AuditDomainCreate:
			created = true
		case domain.AuditDomainDelete:
			deleted = true
		}
	}
	if !created || !deleted {
		t.Fatalf("audit actions missing: create=%v delete=%v", created, deleted)
	}
}

func TestDomainServiceResolveByName(t *testing.T) {
	svc, _, _ := newTestDomainService()
	created, _ := svc.Create(context.Background(), nil, DomainInput{Name: "Resolve.Example.com"})
	got, err := svc.ResolveByName(context.Background(), "resolve.example.com")
	if err != nil || got == nil || got.ID != created.ID {
		t.Fatalf("ResolveByName hit = %v, %v", got, err)
	}
	miss, err := svc.ResolveByName(context.Background(), "absent.example.com")
	if err != nil || miss != nil {
		t.Fatalf("ResolveByName miss = %v, %v (want nil, nil)", miss, err)
	}
}

func TestDomainServiceLedger(t *testing.T) {
	store := newFakeDomainStore()
	certs := &fakeCertStoreForLedger{certs: []*domain.Certificate{
		{ID: "c1", Domains: []string{"www.example.com"}, PrimaryDomain: "www.example.com",
			Status: domain.StatusExpiring, Provider: "acme",
			NotAfter: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)},
		{ID: "c2", Domains: []string{"*.wild.com"}, PrimaryDomain: "*.wild.com",
			Status: domain.StatusValid, Provider: "acme",
			NotAfter: time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)},
	}}
	svc := NewDomainService(store, certs, &fakeAuditStore{}, fixedClock{time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()
	if _, err := svc.Create(ctx, nil, DomainInput{Name: "www.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, nil, DomainInput{Name: "api.wild.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, nil, DomainInput{Name: "missing.example.com"}); err != nil {
		t.Fatal(err)
	}

	items, err := svc.Ledger(ctx, 30)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	byName := map[string]*DomainLedgerItem{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if got := byName["www.example.com"]; got == nil || got.Coverage != domain.CoverageExpiring || got.CertCount != 1 {
		t.Fatalf("www.example.com item = %+v", got)
	} else if got.DaysToExpire == nil || *got.DaysToExpire != 10 {
		t.Fatalf("www.example.com days = %v", got.DaysToExpire)
	}
	// 通配符父域覆盖子域
	if got := byName["api.wild.com"]; got == nil || got.Coverage != domain.CoverageCovered {
		t.Fatalf("api.wild.com item = %+v (want covered via *.wild.com)", got)
	}
	if got := byName["missing.example.com"]; got == nil || got.Coverage != domain.CoverageMissing || got.CurrentCert != nil {
		t.Fatalf("missing.example.com item = %+v", got)
	}
}

// fakeCertStoreForLedger 仅实现 List（台账视图只需要全量证书）
type fakeCertStoreForLedger struct {
	certs []*domain.Certificate
}

func (f *fakeCertStoreForLedger) List(context.Context) ([]*domain.Certificate, error) {
	return f.certs, nil
}

func (f *fakeCertStoreForLedger) Create(context.Context, *domain.Certificate) error { return nil }
func (f *fakeCertStoreForLedger) GetByID(context.Context, string) (*domain.Certificate, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeCertStoreForLedger) Update(context.Context, *domain.Certificate) error { return nil }
func (f *fakeCertStoreForLedger) UpdateStatus(context.Context, string, domain.CertificateStatus) error {
	return nil
}
func (f *fakeCertStoreForLedger) Delete(context.Context, string) error { return nil }
func (f *fakeCertStoreForLedger) Count(context.Context) (int64, error) {
	return int64(len(f.certs)), nil
}
func (f *fakeCertStoreForLedger) CountExpiring(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeCertStoreForLedger) CountExpired(context.Context) (int64, error) { return 0, nil }
func (f *fakeCertStoreForLedger) CountByAccount(context.Context, string) (int64, error) {
	return 0, nil
}

// 确保 tags 输出不包含空白项
func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" a ", "a", "", "  ", "b"})
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("normalizeTags = %v", got)
	}
}

// TestIssueLinksDomainLedger 签发时按 primary domain 反查台账：
// 命中 → 落 domain_id 引用 + domain_name 副本；未登记 → 两列留空（覆盖走名称匹配）
func TestIssueLinksDomainLedger(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")

	store := newFakeDomainStore()
	if err := store.Create(context.Background(), &domain.Domain{
		ID: "dom-1", Name: "www.example.com",
		ExpectedStatus: domain.ExpectedMustHaveCert, Source: domain.DomainSourceManual,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	svc.SetDomains(store)

	// 大小写混合输入也应命中（反查前规范化）
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"WWW.Example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if cert.DomainID != "dom-1" || cert.DomainName != "www.example.com" {
		t.Fatalf("domain link = %q/%q, want dom-1/www.example.com", cert.DomainID, cert.DomainName)
	}

	// 未登记域名：不留悬挂引用
	cert2, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"other.example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatalf("Issue 2: %v", err)
	}
	if cert2.DomainID != "" || cert2.DomainName != "" {
		t.Fatalf("unregistered link = %q/%q, want empty", cert2.DomainID, cert2.DomainName)
	}
}
