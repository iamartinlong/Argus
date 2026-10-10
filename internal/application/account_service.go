package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/iamartinlong/Argus/internal/domain"
)

type ProviderAccountService struct {
	accounts  domain.ProviderAccountStore
	certs     domain.CertificateStore
	audit     domain.AuditStore
	providers ProviderEngine
	cryptor   Cryptor
	clock     Clock
}

func NewProviderAccountService(
	accounts domain.ProviderAccountStore,
	certs domain.CertificateStore,
	audit domain.AuditStore,
	providers ProviderEngine,
	cryptor Cryptor,
	clock Clock,
) *ProviderAccountService {
	return &ProviderAccountService{accounts: accounts, certs: certs, audit: audit, providers: providers, cryptor: cryptor, clock: clock}
}

type CreateAccountInput struct {
	Provider     string
	Name         string
	DirectoryURL string
	Contact      string
	EABKID       string
	EABHMACKey   string
	Credentials  map[string]string // 云厂商凭据（aliyun/tencent 渠道必填）
}

func (s *ProviderAccountService) Create(ctx context.Context, input CreateAccountInput) (*domain.ProviderAccount, error) {
	if input.Provider == "" {
		return nil, fmt.Errorf("%w: 渠道类型不能为空", domain.ErrInvalidArgument)
	}
	if input.Name == "" {
		input.Name = input.Provider
	}
	// 验证渠道已注册
	if _, err := s.providers.Get(input.Provider); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidArgument, err)
	}
	if input.Provider == "acme" {
		if input.DirectoryURL == "" {
			input.DirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"
		}
	}
	var eabHMACEnc string
	if input.EABHMACKey != "" {
		enc, err := s.cryptor.EncryptString(input.EABHMACKey)
		if err != nil {
			return nil, err
		}
		eabHMACEnc = enc
	}
	var credEnc string
	if len(input.Credentials) > 0 {
		plain, err := json.Marshal(input.Credentials)
		if err != nil {
			return nil, err
		}
		enc, err := s.cryptor.EncryptString(string(plain))
		if err != nil {
			return nil, err
		}
		credEnc = enc
	}
	if input.Provider != "acme" && credEnc == "" {
		return nil, fmt.Errorf("%w: 渠道 %s 需要配置云凭据", domain.ErrInvalidArgument, input.Provider)
	}
	account := &domain.ProviderAccount{
		ID:             newID(),
		Provider:       input.Provider,
		Name:           input.Name,
		DirectoryURL:   input.DirectoryURL,
		Contact:        input.Contact,
		EABKID:         input.EABKID,
		EABHMACKeyEnc:  eabHMACEnc,
		CredentialsEnc: credEnc,
		Status:         "active",
		CreatedAt:      s.clock.Now(),
		UpdatedAt:      s.clock.Now(),
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditAccountCreate, account.Name, "provider="+account.Provider)
	return account, nil
}

func (s *ProviderAccountService) List(ctx context.Context) ([]*domain.ProviderAccount, error) {
	return s.accounts.List(ctx)
}

func (s *ProviderAccountService) Get(ctx context.Context, id string) (*domain.ProviderAccount, error) {
	return s.accounts.GetByID(ctx, id)
}

func (s *ProviderAccountService) Delete(ctx context.Context, id string) error {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// 防止删除被证书引用的账户
	if n, err := s.certs.CountByAccount(ctx, id); err == nil && n > 0 {
		return fmt.Errorf("%w: 该账户下仍有 %d 个证书，请先删除或转移", domain.ErrConflict, n)
	}
	if err := s.accounts.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAction(ctx, nil, domain.AuditAccountDelete, account.Name, "")
	return nil
}

// accountSensitiveKeys 不回传的敏感凭据键（编辑时留空=保持原值）
var accountSensitiveKeys = map[string]bool{
	"access_key_secret": true, "secret_key": true, "secret_id": true,
	"private_key_pem": true, // ACME 注册私钥
}

// GetMaskedConfig 解密账户凭据（供编辑回填；敏感键置空）
func (s *ProviderAccountService) GetMaskedConfig(ctx context.Context, id string) (map[string]any, error) {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account.CredentialsEnc == "" {
		return nil, nil
	}
	plain, err := s.cryptor.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, fmt.Errorf("解密账户凭据失败: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	for k := range cfg {
		if accountSensitiveKeys[k] {
			cfg[k] = ""
		}
	}
	return cfg, nil
}

type UpdateAccountInput struct {
	Name         string
	DirectoryURL string
	Contact      string
	EABKID       string
	EABHMACKey   string // 留空表示不修改
	Credentials  map[string]string // 为 nil 表示不修改
	Status       string
}

func (s *ProviderAccountService) Update(ctx context.Context, id string, input UpdateAccountInput) (*domain.ProviderAccount, error) {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		account.Name = input.Name
	}
	if account.Provider == "acme" {
		if input.DirectoryURL != "" {
			account.DirectoryURL = input.DirectoryURL
		}
		account.Contact = input.Contact
		if input.EABKID != "" {
			account.EABKID = input.EABKID
		}
		if input.EABHMACKey != "" {
			enc, err := s.cryptor.EncryptString(input.EABHMACKey)
			if err != nil {
				return nil, err
			}
			account.EABHMACKeyEnc = enc
		}
	} else if input.Credentials != nil {
		// 合并旧凭据：空值保持原值，支持留空保持
		plainOld, err := s.cryptor.DecryptString(account.CredentialsEnc)
		var oldCfg map[string]string
		if err == nil && plainOld != "" {
			_ = json.Unmarshal([]byte(plainOld), &oldCfg)
		}
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
		plain, err := json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		enc, err := s.cryptor.EncryptString(string(plain))
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
	s.auditAction(ctx, nil, domain.AuditAccountUpdate, account.Name, "provider="+account.Provider)
	return account, nil
}

// Test 验证账户可用性：ACME 账户将在此完成注册并持久化凭据
func (s *ProviderAccountService) Test(ctx context.Context, id string) error {
	account, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return err
	}
	provider, err := s.providers.Get(account.Provider)
	if err != nil {
		return err
	}
	if err := provider.TestConnection(ctx, account); err != nil {
		account.Status = "error"
		account.LastError = err.Error()
		account.UpdatedAt = s.clock.Now()
		_ = s.accounts.Update(ctx, account)
		return err
	}
	account.Status = "active"
	account.LastError = ""
	account.UpdatedAt = s.clock.Now()
	if err := s.accounts.Update(ctx, account); err != nil {
		return err
	}
	s.auditAction(ctx, nil, domain.AuditAccountTest, account.Name, "")
	return nil
}

func (s *ProviderAccountService) auditAction(ctx context.Context, actor *domain.User, action domain.AuditAction, resource, detail string) {
	userID, username := "", "system"
	if actor != nil {
		userID, username = actor.ID, actor.Username
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  resource,
		Detail:    detail,
		CreatedAt: s.clock.Now(),
	})
}
