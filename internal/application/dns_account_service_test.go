package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

type fakeDnsAccountStore struct {
	account *domain.DnsProviderAccount
}

func (s *fakeDnsAccountStore) Create(_ context.Context, a *domain.DnsProviderAccount) error {
	return nil
}
func (s *fakeDnsAccountStore) GetByID(_ context.Context, id string) (*domain.DnsProviderAccount, error) {
	if s.account == nil || s.account.ID != id {
		return nil, errors.New("not found")
	}
	return s.account, nil
}
func (s *fakeDnsAccountStore) List(_ context.Context) ([]*domain.DnsProviderAccount, error) {
	return []*domain.DnsProviderAccount{s.account}, nil
}
func (s *fakeDnsAccountStore) Update(_ context.Context, a *domain.DnsProviderAccount) error {
	s.account = a
	return nil
}
func (s *fakeDnsAccountStore) Delete(_ context.Context, id string) error { return nil }

func TestGetMaskedConfigMasksCredentialKeys(t *testing.T) {
	creds := map[string]string{
		"ALICLOUD_ACCESS_KEY":      "ak-id",
		"ALICLOUD_SECRET_KEY":      "ak-secret-plain",
		"CLOUDFLARE_DNS_API_TOKEN": "cf-token",
		"CLOUDFLARE_EMAIL":         "ops@example.com",
		"DNSPOD_API_TOKEN":         "dp-token",
		"GODADDY_API_SECRET":       "gd-secret",
		"MY_CUSTOM_SECRET_VALUE":   "custom-secret",
		"partner_api_token":        "custom-token",
	}
	plain, _ := json.Marshal(creds)
	enc, _ := fakeCryptor{}.EncryptString(string(plain))
	store := &fakeDnsAccountStore{account: &domain.DnsProviderAccount{
		ID:             "dns-1",
		Name:           "test",
		Provider:       domain.DnsAliDNS,
		CredentialsEnc: enc,
		Status:         "active",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}}
	svc := NewDnsProviderAccountService(store, nil, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()})

	masked, err := svc.GetMaskedConfig(context.Background(), "dns-1")
	if err != nil {
		t.Fatalf("GetMaskedConfig: %v", err)
	}
	for _, k := range []string{"ALICLOUD_SECRET_KEY", "CLOUDFLARE_DNS_API_TOKEN", "DNSPOD_API_TOKEN", "GODADDY_API_SECRET", "MY_CUSTOM_SECRET_VALUE", "partner_api_token"} {
		if v, _ := masked[k].(string); v != "" {
			t.Errorf("键 %s 应被脱敏，实际返回 %q", k, v)
		}
	}
	// 含 "key"/"token"/"secret" 子串的键一律脱敏（含 ALICLOUD_ACCESS_KEY）
	for _, k := range []string{"ALICLOUD_ACCESS_KEY"} {
		if v, _ := masked[k].(string); v != "" {
			t.Errorf("键 %s 应按子串规则脱敏，实际返回 %q", k, v)
		}
	}
	// 非敏感键保留原值
	if v, _ := masked["CLOUDFLARE_EMAIL"].(string); v != "ops@example.com" {
		t.Errorf("非敏感键 CLOUDFLARE_EMAIL 应保留原值，实际 %q", v)
	}
}
