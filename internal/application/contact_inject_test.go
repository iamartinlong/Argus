package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

type fakeContactStore struct {
	contacts []*domain.ContactPerson
}

func newFakeContactStore(contacts ...*domain.ContactPerson) *fakeContactStore {
	return &fakeContactStore{contacts: contacts}
}

func (f *fakeContactStore) Create(_ context.Context, c *domain.ContactPerson) error {
	f.contacts = append(f.contacts, c)
	return nil
}
func (f *fakeContactStore) GetByID(_ context.Context, id string) (*domain.ContactPerson, error) {
	for _, c := range f.contacts {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeContactStore) List(context.Context) ([]*domain.ContactPerson, error) {
	return f.contacts, nil
}
func (f *fakeContactStore) Update(_ context.Context, c *domain.ContactPerson) error { return nil }
func (f *fakeContactStore) Delete(_ context.Context, id string) error               { return nil }

func newTestCertServiceWithContacts(contacts *fakeContactStore) *CertificateService {
	certs := newFakeCertificateStore()
	orders := newFakeOrderStore()
	accounts := newFakeProviderAccountStore()
	audit := &fakeAuditStore{}
	return NewCertificateService(certs, orders, accounts, contacts, audit,
		newFakeRegistry(newFakeProvider("aliyun")), fakeCryptor{}, fixedClock{time.Now()}, 80, NewEventBus())
}

func TestAccountWithContactInjectsAndDefaults(t *testing.T) {
	contact := &domain.ContactPerson{
		ID: "c1", Name: "张三", Email: "zs@example.com", Phone: "13800138000",
	}
	svc := newTestCertServiceWithContacts(newFakeContactStore(contact))

	// 预置云账户凭据（不含联系人）
	account := &domain.ProviderAccount{Provider: "aliyun", CredentialsEnc: base64.StdEncoding.EncodeToString([]byte(`{"access_key_id":"ak"}`))}

	// 未指定 contactID → 用全局第一个
	out, _, err := svc.accountWithContact(context.Background(), account, "")
	if err != nil {
		t.Fatalf("default contact: %v", err)
	}
	if out == account {
		t.Fatal("expected account clone, got same pointer")
	}
	plain, err := fakeCryptor{}.DecryptString(out.CredentialsEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["contact_email"] != "zs@example.com" || cfg["contact_phone"] != "13800138000" || cfg["contact_name"] != "张三" {
		t.Fatalf("contact not injected: %+v", cfg)
	}
	if cfg["access_key_id"] != "ak" {
		t.Fatalf("original credentials lost: %+v", cfg)
	}

	// 指定 contactID
	out2, _, err := svc.accountWithContact(context.Background(), account, "c1")
	if err != nil {
		t.Fatalf("specified contact: %v", err)
	}
	plain2, _ := fakeCryptor{}.DecryptString(out2.CredentialsEnc)
	var cfg2 map[string]string
	_ = json.Unmarshal([]byte(plain2), &cfg2)
	if cfg2["contact_email"] != "zs@example.com" {
		t.Fatalf("specified contact not applied: %+v", cfg2)
	}
}

func TestAccountWithContactErrors(t *testing.T) {
	svc := newTestCertServiceWithContacts(newFakeContactStore())
	account := &domain.ProviderAccount{Provider: "aliyun", CredentialsEnc: base64.StdEncoding.EncodeToString([]byte(`{}`))}

	// 无联系人 → 报错
	if _, _, err := svc.accountWithContact(context.Background(), account, ""); err == nil {
		t.Fatal("expected error when no contact configured")
	}
	// 指定的联系人不存在 → 报错
	if _, _, err := svc.accountWithContact(context.Background(), account, "nope"); err == nil {
		t.Fatal("expected error for missing contact id")
	}
}

func TestAccountWithContactAcmePassthrough(t *testing.T) {
	svc := newTestCertServiceWithContacts(newFakeContactStore())
	account := &domain.ProviderAccount{Provider: "acme", CredentialsEnc: ""}
	out, contact, err := svc.accountWithContact(context.Background(), account, "")
	if err != nil {
		t.Fatalf("acme passthrough: %v", err)
	}
	if out != account {
		t.Fatal("expected same account for acme")
	}
	if contact != nil {
		t.Fatal("expected nil contact for acme")
	}
}