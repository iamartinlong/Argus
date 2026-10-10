package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// CertificateProvider 是全部签发渠道的统一接口（application 层依赖此接口）。
type CertificateProvider interface {
	Name() string
	// Issue 签发证书，返回统一结果。内部处理异步申请的轮询。
	Issue(ctx context.Context, req IssueRequest) (*IssuedCertificate, error)
	// Renew 续期（ACME 支持；云厂商可为空实现）
	Renew(ctx context.Context, req RenewRequest) (*IssuedCertificate, error)
	// Revoke 吊销（可选，返回 nil 表示不支持）
	Revoke(ctx context.Context, req RevokeRequest) error
	// ValidateDomain 域名验证（云厂商签发前置：DNS 记录验证等；ACME 渠道在 Issue 内完成，返回 nil）
	ValidateDomain(ctx context.Context, req ValidateRequest) error
	// TestConnection 验证渠道凭据可用性（云厂商 DNS 验证前调用）
	TestConnection(ctx context.Context, account *domain.ProviderAccount) error
}

type ValidateRequest struct {
	Account      *domain.ProviderAccount
	Domains      []string
	DNSConfig    map[string]string
}

type IssueRequest struct {
	Account      *domain.ProviderAccount
	Domains      []string
	Verification domain.VerificationType // http01 | dns01
	// DNS01 凭据（access key 等，若 lego DNS provider 需要）
	DNSConfig map[string]string
	// HTTP01 验证服务器端口（0 表示随机）
	HTTP01Port int
	// 可复用密钥（续期时传入已有私钥；空则新生成）
	PrivateKeyPEM string
	// 外部 CSR（PEM）：非空时以 CSR 模式签发（私钥保留在客户端，仅 ACME 渠道支持）
	CSRPEM string
}

type RenewRequest struct {
	Account         *domain.ProviderAccount
	Domains         []string
	Verification    domain.VerificationType
	DNSConfig       map[string]string
	CertificatePEM  string // 旧证书（lego Renew 需要解析）
	PrivateKeyPEM   string
}

type RevokeRequest struct {
	Account        *domain.ProviderAccount
	CertificatePEM string
}

type IssuedCertificate struct {
	CertificatePEM string // 叶子 + 链
	PrivateKeyPEM  string
	IssuerCA       string
	NotBefore      time.Time
	NotAfter       time.Time
}

// Registry 渠道注册表
type Registry struct {
	mu        sync.RWMutex
	providers map[string]CertificateProvider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]CertificateProvider{}}
}

func (r *Registry) Register(p CertificateProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (CertificateProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("未注册的证书渠道: %s", name)
	}
	return p, nil
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}
