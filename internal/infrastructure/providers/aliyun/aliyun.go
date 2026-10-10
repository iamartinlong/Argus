package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	cas "github.com/alibabacloud-go/cas-20200407/v2/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

const Name = "aliyun"

type Provider struct {
	enc      Cryptor
	endpoint string // 测试可覆盖
}

type Cryptor interface {
	EncryptString(s string) (string, error)
	DecryptString(enc string) (string, error)
}

func New(enc Cryptor) *Provider { return &Provider{enc: enc} }

func (p *Provider) Name() string { return Name }

// aliyunConfig 渠道凭据与申请信息（加密存储于 ProviderAccount.CredentialsEnc）
type aliyunConfig struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	Region          string `json:"region"` // 默认 cn-hangzhou
	ContactEmail    string `json:"contact_email"`
	ContactPhone    string `json:"contact_phone"`
	ContactName     string `json:"contact_name"`
	ProductCode     string `json:"product_code"` // 免费证书规格，默认 symantec-free-1-free
}

func (p *Provider) loadConfig(account *domain.ProviderAccount) (*aliyunConfig, error) {
	if account.CredentialsEnc == "" {
		return nil, fmt.Errorf("渠道账户未配置凭据")
	}
	plain, err := p.enc.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, fmt.Errorf("解密凭据: %w", err)
	}
	var cfg aliyunConfig
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, fmt.Errorf("解析凭据: %w", err)
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("AccessKey ID/Secret 不能为空")
	}
	if cfg.Region == "" {
		cfg.Region = "cn-hangzhou"
	}
	if cfg.ProductCode == "" {
		cfg.ProductCode = "symantec-free-1-free"
	}
	return &cfg, nil
}

func (p *Provider) newClient(cfg *aliyunConfig) (*cas.Client, error) {
	client, err := cas.NewClient(&openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		RegionId:        tea.String(cfg.Region),
	})
	if err != nil {
		return nil, fmt.Errorf("创建阿里云客户端: %w", err)
	}
	if p.endpoint != "" {
		client.Endpoint = tea.String(p.endpoint)
		client.Protocol = tea.String("http")
	}
	return client, nil
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
	// 轻量接口验证凭据：列出证书订单
	req := &cas.ListUserCertificateOrderRequest{ShowSize: tea.Int64(1)}
	resp, err := client.ListUserCertificateOrderWithOptions(req, &util.RuntimeOptions{})
	if err != nil {
		return fmt.Errorf("凭据验证失败: %w", err)
	}
	_ = resp
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
		return nil, fmt.Errorf("阿里云免费证书单次仅支持单个域名，收到 %d 个", len(req.Domains))
	}
	if cfg.ContactEmail == "" || cfg.ContactPhone == "" || cfg.ContactName == "" {
		return nil, fmt.Errorf("阿里云账户需配置联系人信息（邮箱/电话/姓名）")
	}
	apply := &cas.CreateCertificateRequestRequest{
		Domain:      tea.String(req.Domains[0]),
		Email:       tea.String(cfg.ContactEmail),
		Phone:       tea.String(cfg.ContactPhone),
		Username:    tea.String(cfg.ContactName),
		ProductCode: tea.String(cfg.ProductCode),
		ValidateType: tea.String("DNS"),
	}
	applyResp, err := client.CreateCertificateRequestWithOptions(apply, &util.RuntimeOptions{})
	if err != nil {
		return nil, fmt.Errorf("阿里云申请失败: %w", err)
	}
	orderID := applyResp.Body.OrderId
	if orderID == nil {
		return nil, fmt.Errorf("阿里云申请未返回订单号")
	}
	if err := p.waitForIssue(ctx, client, orderID, 600*time.Second); err != nil {
		return nil, err
	}
	return p.fetchCertificate(client, req.Domains[0])
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
	// 阿里云免费证书无需显式吊销（到期自动失效），此处返回 nil 表示渠道不支持吊销
	return nil
}

func (p *Provider) ValidateDomain(ctx context.Context, req providers.ValidateRequest) error {
	// 阿里云 DNS 验证由平台自动执行（域名托管在阿里云 DNS 时）
	return nil
}

// waitForIssue 轮询 DescribeCertificateState 直到签发
// 验证中状态连续出现超过阈值时返回指引（避免盲目等待：域名需托管阿里云 DNS 或手动完成验证）
func (p *Provider) waitForIssue(ctx context.Context, client *cas.Client, orderID *int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	verifyingCount := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		stateReq := &cas.DescribeCertificateStateRequest{OrderId: orderID}
		stateResp, err := client.DescribeCertificateStateWithOptions(stateReq, &util.RuntimeOptions{})
		if err != nil {
			return fmt.Errorf("查询签发状态失败: %w", err)
		}
		state := tea.StringValue(stateResp.Body.Type)
		switch state {
		case "CERT_ISSUED":
			return nil
		case "VERIFYING":
			// 验证进行中：可能是自动验证（域名托管阿里云 DNS）或等待用户添加 TXT 记录
			verifyingCount++
			if verifyingCount >= 3 {
				uri := tea.StringValue(stateResp.Body.Uri)
				hint := "域名验证未完成：请确认域名已托管在阿里云 DNS 并授权自动验证；"
				if uri != "" {
					hint += "或按验证指引完成手动验证（" + uri + "）"
				}
				hint += "。验证完成后可重试签发。"
				return fmt.Errorf("%s", hint)
			}
		case "VERIFY_FAILED", "APPLY_FAILED", "CANCELED":
			return fmt.Errorf("证书申请失败（状态 %s）: %s", state, tea.StringValue(stateResp.Body.Uri))
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("等待签发超时（%v）：请检查域名验证状态", timeout)
}

// fetchCertificate 从订单列表定位证书并获取 PEM
func (p *Provider) fetchCertificate(client *cas.Client, domain string) (*providers.IssuedCertificate, error) {
	req := &cas.ListUserCertificateOrderRequest{ShowSize: tea.Int64(50)}
	resp, err := client.ListUserCertificateOrderWithOptions(req, &util.RuntimeOptions{})
	if err != nil {
		return nil, fmt.Errorf("查询订单失败: %w", err)
	}
	if resp.Body.CertificateOrderList == nil {
		return nil, fmt.Errorf("未找到证书订单")
	}
	for _, order := range resp.Body.CertificateOrderList {
		if tea.StringValue(order.Domain) != domain && tea.StringValue(order.CommonName) != domain {
			continue
		}
		if tea.StringValue(order.Status) != "ISSUED" {
			continue
		}
		certID := order.CertificateId
		if certID == nil {
			continue
		}
		detailReq := &cas.GetUserCertificateDetailRequest{CertId: certID}
		detailResp, err := client.GetUserCertificateDetailWithOptions(detailReq, &util.RuntimeOptions{})
		if err != nil {
			return nil, fmt.Errorf("获取证书详情失败: %w", err)
		}
		cert := tea.StringValue(detailResp.Body.Cert)
		key := tea.StringValue(detailResp.Body.Key)
		if cert == "" || key == "" {
			return nil, fmt.Errorf("证书内容为空")
		}
		notBefore, notAfter := parseValidity(detailResp.Body.StartDate, detailResp.Body.EndDate)
		return &providers.IssuedCertificate{
			CertificatePEM: cert,
			PrivateKeyPEM:  key,
			IssuerCA:       "Aliyun CAS",
			NotBefore:      notBefore,
			NotAfter:       notAfter,
		}, nil
	}
	return nil, fmt.Errorf("未找到已签发证书（%s）", domain)
}
