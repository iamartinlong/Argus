package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ssl "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ssl/v20191205"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

const Name = "tencent"

type Provider struct {
	enc      Cryptor
	endpoint string // 测试可覆盖（"127.0.0.1:port"）
	scheme   string // http/https，测试用 http
}

type Cryptor interface {
	EncryptString(s string) (string, error)
	DecryptString(enc string) (string, error)
}

func New(enc Cryptor) *Provider {
	return &Provider{enc: enc, scheme: "https"}
}

func (p *Provider) Name() string { return Name }

// tencentConfig 渠道凭据与申请信息
type tencentConfig struct {
	SecretID     string `json:"secret_id"`
	SecretKey    string `json:"secret_key"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	// DvAuthMethod: DNS_AUTO（域名托管腾讯云 DNS 时自动验证，推荐）
	//              DNS（手动添加 TXT 记录）
	DvAuthMethod string `json:"dv_auth_method"`
}

func (p *Provider) loadConfig(account *domain.ProviderAccount) (*tencentConfig, error) {
	if account.CredentialsEnc == "" {
		return nil, fmt.Errorf("渠道账户未配置凭据")
	}
	plain, err := p.enc.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, fmt.Errorf("解密凭据: %w", err)
	}
	var cfg tencentConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, fmt.Errorf("解析凭据: %w", err)
	}
	if cfg.SecretID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("SecretId/SecretKey 不能为空")
	}
	if cfg.DvAuthMethod == "" {
		cfg.DvAuthMethod = "DNS_AUTO"
	}
	return &cfg, nil
}

func (p *Provider) newClient(cfg *tencentConfig) (*ssl.Client, error) {
	cred := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	cpf := profile.NewClientProfile()
	if p.endpoint != "" {
		cpf.HttpProfile.Endpoint = p.endpoint
		cpf.HttpProfile.Scheme = p.scheme
	} else {
		cpf.HttpProfile.Endpoint = "ssl.tencentcloudapi.com"
	}
	return ssl.NewClient(cred, "", cpf)
}

func (p *Provider) TestConnection(ctx context.Context, account *domain.ProviderAccount) error {
	cfg, err := p.loadConfig(account)
	if err != nil {
		return err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return err
	}
	// 轻量接口验证凭据：列出证书
	req := ssl.NewDescribeCertificatesRequest()
	req.Limit = common.Uint64Ptr(1)
	_, err = client.DescribeCertificates(req)
	if err != nil {
		return fmt.Errorf("凭据验证失败: %w", err)
	}
	return nil
}

// Issue 申请免费证书 → 轮询签发 → 下载 PEM
func (p *Provider) Issue(ctx context.Context, req providers.IssueRequest) (*providers.IssuedCertificate, error) {
	cfg, err := p.loadConfig(req.Account)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	if len(req.Domains) != 1 {
		return nil, fmt.Errorf("腾讯云免费证书单次仅支持单个域名，收到 %d 个", len(req.Domains))
	}
	apply := ssl.NewApplyCertificateRequest()
	apply.DomainName = common.StringPtr(req.Domains[0])
	apply.DvAuthMethod = common.StringPtr(cfg.DvAuthMethod)
	apply.PackageType = common.StringPtr("3") // TrustAsia C1 DV Free
	apply.ContactEmail = common.StringPtr(cfg.ContactEmail)
	apply.ContactPhone = common.StringPtr(cfg.ContactPhone)
	applyResponse, err := client.ApplyCertificate(apply)
	if err != nil {
		return nil, fmt.Errorf("腾讯云申请失败: %w", err)
	}
	certID := applyResponse.Response.CertificateId
	if certID == nil {
		return nil, fmt.Errorf("腾讯云申请未返回证书 ID")
	}
	if err := p.waitForIssue(ctx, client, certID, 600*time.Second); err != nil {
		return nil, err
	}
	return p.fetchCertificate(client, certID)
}

// Renew 免费证书到期重新申请（云厂商续期 = 新申请）
func (p *Provider) Renew(ctx context.Context, req providers.RenewRequest) (*providers.IssuedCertificate, error) {
	return p.Issue(ctx, providers.IssueRequest{
		Account:      req.Account,
		Domains:      req.Domains,
		Verification: req.Verification,
		DNSConfig:    req.DNSConfig,
	})
}

func (p *Provider) Revoke(ctx context.Context, req providers.RevokeRequest) error {
	// 腾讯云免费证书不支持直接吊销 API；到期自动失效，返回 nil 表示渠道不支持
	return nil
}

func (p *Provider) ValidateDomain(ctx context.Context, req providers.ValidateRequest) error {
	// DNS_AUTO 模式由腾讯云自动完成验证
	return nil
}

// waitForIssue 轮询证书状态直到已签发
// 状态码：0=审核中 1=已签发 2=审核失败 3=已过期 4=自动添加DNS记录中 7=已取消 10=已吊销
// 验证相关状态连续出现超过阈值时返回指引（域名需托管腾讯云 DNS 或手动完成验证）
func (p *Provider) waitForIssue(ctx context.Context, client *ssl.Client, certID *string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	verifyingCount := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		detailReq := ssl.NewDescribeCertificateDetailRequest()
		detailReq.CertificateId = certID
		resp, err := client.DescribeCertificateDetail(detailReq)
		if err != nil {
			return fmt.Errorf("查询签发状态失败: %w", err)
		}
		status := resp.Response.Status
		if status == nil {
			time.Sleep(10 * time.Second)
			continue
		}
		switch *status {
		case 1: // 已签发
			return nil
		case 2, 7, 10: // 审核失败/已取消/已吊销
			return fmt.Errorf("证书申请失败（状态码 %d: %s）", *status, deref(resp.Response.StatusMsg))
		case 4: // 自动添加 DNS 记录中：需要域名托管腾讯云 DNS 且授权
			verifyingCount++
			if verifyingCount >= 3 {
				return fmt.Errorf("域名验证未完成：请确认域名已托管在腾讯云 DNS（云解析）且与申请证书为同一账号；或改用 DNS 手动验证方式。验证完成后可重试签发。")
			}
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("等待签发超时（%v）：请检查域名验证状态", timeout)
}

func (p *Provider) fetchCertificate(client *ssl.Client, certID *string) (*providers.IssuedCertificate, error) {
	detailReq := ssl.NewDescribeCertificateDetailRequest()
	detailReq.CertificateId = certID
	resp, err := client.DescribeCertificateDetail(detailReq)
	if err != nil {
		return nil, fmt.Errorf("获取证书详情失败: %w", err)
	}
	cert := resp.Response.CertificatePublicKey
	key := resp.Response.CertificatePrivateKey
	if cert == nil || key == nil || *cert == "" || *key == "" {
		return nil, fmt.Errorf("证书内容为空")
	}
	nb, na := parseValidity(resp.Response.CertBeginTime, resp.Response.CertEndTime)
	return &providers.IssuedCertificate{
		CertificatePEM: *cert,
		PrivateKeyPEM:  *key,
		IssuerCA:       "Tencent Cloud SSL",
		NotBefore:      nb,
		NotAfter:       na,
	}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
