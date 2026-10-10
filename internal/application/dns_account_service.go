package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iamartinlong/Argus/internal/domain"
)

type DnsProviderAccountService struct {
	accounts domain.DnsProviderAccountStore
	certs    domain.CertificateStore
	audit    domain.AuditStore
	cryptor  Cryptor
	clock    Clock
}

func NewDnsProviderAccountService(
	accounts domain.DnsProviderAccountStore,
	certs domain.CertificateStore,
	audit domain.AuditStore,
	cryptor Cryptor,
	clock Clock,
) *DnsProviderAccountService {
	return &DnsProviderAccountService{accounts: accounts, certs: certs, audit: audit, cryptor: cryptor, clock: clock}
}

type CreateDnsAccountInput struct {
	Name        string
	Provider    string
	Credentials map[string]string
}

func (s *DnsProviderAccountService) Create(ctx context.Context, input CreateDnsAccountInput) (*domain.DnsProviderAccount, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: 名称不能为空", domain.ErrInvalidArgument)
	}
	provider := domain.DnsProvider(input.Provider)
	if !provider.Valid() {
		return nil, fmt.Errorf("%w: 无效 DNS 提供商 %s", domain.ErrInvalidArgument, input.Provider)
	}
	if len(input.Credentials) == 0 {
		return nil, fmt.Errorf("%w: 凭据不能为空", domain.ErrInvalidArgument)
	}
	if err := validateDnsCredentials(provider, input.Credentials); err != nil {
		return nil, err
	}
	plain, _ := json.Marshal(input.Credentials)
	enc, err := s.cryptor.EncryptString(string(plain))
	if err != nil {
		return nil, err
	}
	account := &domain.DnsProviderAccount{
		ID:             newID(),
		Name:           input.Name,
		Provider:       provider,
		CredentialsEnc: enc,
		Status:         "active",
		CreatedAt:      s.clock.Now(),
		UpdatedAt:      s.clock.Now(),
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		return nil, err
	}
	s.auditAction(ctx, domain.AuditAccountCreate, account.Name, "provider="+string(provider))
	return account, nil
}

func (s *DnsProviderAccountService) List(ctx context.Context) ([]*domain.DnsProviderAccount, error) {
	return s.accounts.List(ctx)
}

func (s *DnsProviderAccountService) Get(ctx context.Context, id string) (*domain.DnsProviderAccount, error) {
	return s.accounts.GetByID(ctx, id)
}

func (s *DnsProviderAccountService) Delete(ctx context.Context, id string) error {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// 防止删除被证书引用的账户
	certs, err := s.certs.List(ctx)
	if err == nil {
		for _, c := range certs {
			if c.DNSProviderAccountID == id {
				return fmt.Errorf("%w: 该 DNS 账户仍被证书 %s 引用", domain.ErrConflict, c.PrimaryDomain)
			}
		}
	}
	if err := s.accounts.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAction(ctx, domain.AuditAccountDelete, account.Name, "")
	return nil
}

// dnsSensitiveKeys 精确匹配已知的厂商凭据键；此外 isSensitiveKeyName 按
// 子串兜底（宁可多脱敏），覆盖自定义/未来新增的键名。
var dnsSensitiveKeys = map[string]bool{
	"ALICLOUD_SECRET_KEY":      true,
	"CLOUDFLARE_API_KEY":       true,
	"CLOUDFLARE_DNS_API_TOKEN": true,
	"DNSPOD_API_TOKEN":         true,
	"GODADDY_API_SECRET":       true,
	"GODADDY_API_KEY":          true,
	"secret":                   true,
	"key":                      true,
	"token":                    true,
}

func isSensitiveKeyName(k string) bool {
	if dnsSensitiveKeys[k] {
		return true
	}
	lk := strings.ToLower(k)
	return strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "key")
}

func (s *DnsProviderAccountService) GetMaskedConfig(ctx context.Context, id string) (map[string]any, error) {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account.CredentialsEnc == "" {
		return nil, nil
	}
	plain, err := s.cryptor.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, fmt.Errorf("解密 DNS 账户凭据失败: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	for k := range cfg {
		if isSensitiveKeyName(k) {
			cfg[k] = ""
		}
	}
	return cfg, nil
}

type UpdateDnsAccountInput struct {
	Name        string
	Credentials map[string]string // nil 表示不修改
	Status      string
}

func (s *DnsProviderAccountService) Update(ctx context.Context, id string, input UpdateDnsAccountInput) (*domain.DnsProviderAccount, error) {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		account.Name = input.Name
	}
	if input.Credentials != nil {
		// 合并旧凭据：空值保持原值，支持轮换时仅更新部分键
		plain, err := s.cryptor.DecryptString(account.CredentialsEnc)
		if err != nil {
			return nil, fmt.Errorf("解密旧凭据失败: %w", err)
		}
		var oldCfg map[string]string
		_ = json.Unmarshal([]byte(plain), &oldCfg)
		if oldCfg == nil {
			oldCfg = map[string]string{}
		}
		merged := make(map[string]string, len(oldCfg))
		for k, v := range oldCfg {
			merged[k] = v
		}
		for k, v := range input.Credentials {
			if v != "" {
				merged[k] = v
			}
		}
		if err := validateDnsCredentials(account.Provider, merged); err != nil {
			return nil, err
		}
		encPlain, _ := json.Marshal(merged)
		enc, err := s.cryptor.EncryptString(string(encPlain))
		if err != nil {
			return nil, err
		}
		account.CredentialsEnc = enc
	}
	if input.Status != "" {
		account.Status = input.Status
	}
	account.UpdatedAt = s.clock.Now()
	if err := s.accounts.Update(ctx, account); err != nil {
		return nil, err
	}
	s.auditAction(ctx, domain.AuditAccountUpdate, account.Name, "provider="+string(account.Provider))
	return account, nil
}

func validateDnsCredentials(provider domain.DnsProvider, creds map[string]string) error {
	switch provider {
	case domain.DnsAliDNS:
		if creds["ALICLOUD_ACCESS_KEY"] == "" || creds["ALICLOUD_SECRET_KEY"] == "" {
			return fmt.Errorf("%w: alidns 需要 ALICLOUD_ACCESS_KEY 与 ALICLOUD_SECRET_KEY", domain.ErrInvalidArgument)
		}
	case domain.DnsCloudflare:
		if creds["CLOUDFLARE_DNS_API_TOKEN"] == "" && (creds["CLOUDFLARE_API_KEY"] == "" || creds["CLOUDFLARE_EMAIL"] == "") {
			return fmt.Errorf("%w: cloudflare 需要 CLOUDFLARE_DNS_API_TOKEN 或 CLOUDFLARE_API_KEY + CLOUDFLARE_EMAIL", domain.ErrInvalidArgument)
		}
	case domain.DnsDnspod:
		if creds["DNSPOD_API_TOKEN"] == "" {
			return fmt.Errorf("%w: dnspod 需要 DNSPOD_API_TOKEN", domain.ErrInvalidArgument)
		}
	case domain.DnsGodaddy:
		if creds["GODADDY_API_KEY"] == "" || creds["GODADDY_API_SECRET"] == "" {
			return fmt.Errorf("%w: godaddy 需要 GODADDY_API_KEY 与 GODADDY_API_SECRET", domain.ErrInvalidArgument)
		}
	}
	return nil
}

func (s *DnsProviderAccountService) auditAction(ctx context.Context, action domain.AuditAction, resource, detail string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    action,
		Resource:  resource,
		Detail:    detail,
		CreatedAt: s.clock.Now(),
	})
}

// ResolveDNSConfig 将 DNS 账户凭据解析为签发所需的 DNSConfig
func (s *DnsProviderAccountService) ResolveDNSConfig(ctx context.Context, id string) (map[string]string, error) {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	plain, err := s.cryptor.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, fmt.Errorf("解密 DNS 账户凭据失败: %w", err)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	cfg["provider"] = string(account.Provider)
	return cfg, nil
}
