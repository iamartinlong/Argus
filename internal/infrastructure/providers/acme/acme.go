package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/registration"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

const Name = "acme"

type Provider struct {
	enc      Cryptor
	rootCAs  *x509.CertPool // 自定义 CA（自建 ACME CA / 测试用），nil 使用系统信任
	http01Mu sync.Mutex
	http01   challenge.Provider // HTTP-01 验证服务器单例（并发签发复用同一监听端口）
}

// Cryptor 抽象加解密，避免直接依赖具体实现，便于测试
type Cryptor interface {
	EncryptString(s string) (string, error)
	DecryptString(enc string) (string, error)
}

func New(enc Cryptor) *Provider {
	return &Provider{enc: enc}
}

// NewWithCA 指定根 CA 文件（PEM 池）
func NewWithCA(enc Cryptor, caFile string) (*Provider, error) {
	pool, err := loadCertPool(caFile)
	if err != nil {
		return nil, err
	}
	return &Provider{enc: enc, rootCAs: pool}, nil
}

func loadCertPool(caFile string) (*x509.CertPool, error) {
	data, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 文件: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("CA 文件不含有效证书: %s", caFile)
	}
	return pool, nil
}

func (p *Provider) Name() string { return Name }

// acmeUser 实现 lego 的 User 接口
type acmeUser struct {
	email        string
	reg          *registration.Resource
	privateKey   *ecdsa.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.privateKey }

type storedAccount struct {
	PrivateKeyPEM string                  `json:"private_key_pem"`
	Registration  *registration.Resource `json:"registration"`
}

// loadUser 从账户凭据恢复 lego 用户（无则新建）
func (p *Provider) loadUser(account *domain.ProviderAccount) (*acmeUser, error) {
	user := &acmeUser{email: account.Contact}
	if account.CredentialsEnc != "" {
		plain, err := p.enc.DecryptString(account.CredentialsEnc)
		if err != nil {
			return nil, fmt.Errorf("解密账户凭据: %w", err)
		}
		var stored storedAccount
		if err := json.Unmarshal([]byte(plain), &stored); err != nil {
			return nil, fmt.Errorf("解析账户凭据: %w", err)
		}
		key, err := parseECKey(stored.PrivateKeyPEM)
		if err != nil {
			return nil, err
		}
		user.privateKey = key
		user.reg = stored.Registration
		return user, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	user.privateKey = key
	return user, nil
}

// saveUser 加密保存账户密钥与注册信息
func (p *Provider) saveUser(account *domain.ProviderAccount, user *acmeUser) error {
	keyDER, err := x509.MarshalECPrivateKey(user.privateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	stored := storedAccount{PrivateKeyPEM: string(keyPEM), Registration: user.reg}
	plain, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	enc, err := p.enc.EncryptString(string(plain))
	if err != nil {
		return err
	}
	account.CredentialsEnc = enc
	return nil
}

func parseECKey(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("无效的私钥 PEM")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func (p *Provider) newClient(account *domain.ProviderAccount, user *acmeUser) (*lego.Client, error) {
	dir := account.DirectoryURL
	if dir == "" {
		dir = lego.LEDirectoryProduction
	}
	legoCfg := lego.NewConfig(user)
	legoCfg.Certificate.KeyType = certcrypto.EC256
	legoCfg.Certificate.Timeout = 5 * time.Minute
	legoCfg.CADirURL = dir
	if p.rootCAs != nil {
		legoCfg.HTTPClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: p.rootCAs},
			},
		}
	}
	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ACME 客户端: %w", err)
	}
	return client, nil
}

// registerAccount 注册或登录 ACME 账户；返回是否首次注册
func (p *Provider) registerAccount(ctx context.Context, client *lego.Client, account *domain.ProviderAccount, user *acmeUser) (bool, error) {
	_ = ctx
	if user.reg != nil {
		return false, nil
	}
	if account.EABKID != "" {
		hmac, err := p.enc.DecryptString(account.EABHMACKeyEnc)
		if err != nil {
			return false, fmt.Errorf("解密 EAB 密钥: %w", err)
		}
		reg, err := client.Registration.RegisterWithExternalAccountBinding(registration.RegisterEABOptions{
			TermsOfServiceAgreed: true,
			Kid:                  account.EABKID,
			HmacEncoded:          hmac,
		})
		if err != nil {
			return false, fmt.Errorf("ACME 账户注册失败: %w", err)
		}
		user.reg = reg
		return true, nil
	}
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return false, fmt.Errorf("ACME 账户注册失败: %w", err)
	}
	user.reg = reg
	return true, nil
}

// setupChallenges 配置验证方式（HTTP-01 验证服务器为单例，支持并发签发）
func (p *Provider) setupChallenges(client *lego.Client, verification domain.VerificationType, dnsConfig map[string]string, httpPort int) error {
	if verification == domain.VerifyHTTP01 {
		if p.http01 == nil {
			p.http01Mu.Lock()
			defer p.http01Mu.Unlock()
			if p.http01 == nil {
				port := "80"
				if httpPort > 0 {
					port = fmt.Sprintf("%d", httpPort)
				}
				p.http01 = http01.NewProviderServer("0.0.0.0", port)
			}
		}
		return client.Challenge.SetHTTP01Provider(p.http01)
	}
	if verification == domain.VerifyDNS01 {
		dp, err := p.dnsProvider(dnsConfig)
		if err != nil {
			return err
		}
		return client.Challenge.SetDNS01Provider(dp)
	}
	return fmt.Errorf("不支持的验证方式: %s", verification)
}

func (p *Provider) ValidateDomain(ctx context.Context, req providers.ValidateRequest) error {
	// ACME 渠道的域名验证在 Issue 内部完成（HTTP-01/DNS-01 challenge）
	return nil
}

var dnsProviderNames = []string{"alidns", "cloudflare", "dnspod", "godaddy"}

func (p *Provider) dnsProvider(dnsConfig map[string]string) (challenge.Provider, error) {
	name := dnsConfig["provider"]
	if name == "" {
		return nil, fmt.Errorf("DNS-01 需要指定 provider（支持: %s）", strings.Join(dnsProviderNames, ", "))
	}
	// 显式传 config 构造 provider：不写进程环境变量（os.Setenv 会污染全局且并发续期互相覆盖）
	switch name {
	case "alidns":
		cfg := alidns.NewDefaultConfig()
		cfg.APIKey = dnsConfig["ALICLOUD_ACCESS_KEY"]
		cfg.SecretKey = dnsConfig["ALICLOUD_SECRET_KEY"]
		return alidns.NewDNSProviderConfig(cfg)
	case "cloudflare":
		cfg := cloudflare.NewDefaultConfig()
		cfg.AuthToken = dnsConfig["CLOUDFLARE_DNS_API_TOKEN"]
		if cfg.AuthToken == "" {
			cfg.AuthEmail = dnsConfig["CLOUDFLARE_EMAIL"]
			cfg.AuthKey = dnsConfig["CLOUDFLARE_API_KEY"]
		}
		return cloudflare.NewDNSProviderConfig(cfg)
	case "dnspod":
		cfg := dnspod.NewDefaultConfig()
		cfg.LoginToken = dnsConfig["DNSPOD_API_TOKEN"]
		return dnspod.NewDNSProviderConfig(cfg)
	case "godaddy":
		cfg := godaddy.NewDefaultConfig()
		cfg.APIKey = dnsConfig["GODADDY_API_KEY"]
		cfg.APISecret = dnsConfig["GODADDY_API_SECRET"]
		return godaddy.NewDNSProviderConfig(cfg)
	default:
		return nil, fmt.Errorf("不支持的 DNS provider: %s", name)
	}
}

func (p *Provider) Issue(ctx context.Context, req providers.IssueRequest) (*providers.IssuedCertificate, error) {
	if req.CSRPEM != "" {
		return p.IssueWithCSR(ctx, req, req.CSRPEM)
	}
	if len(req.Domains) == 0 {
		return nil, fmt.Errorf("域名不能为空")
	}
	user, err := p.loadUser(req.Account)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(req.Account, user)
	if err != nil {
		return nil, err
	}
	if _, err := p.registerAccount(ctx, client, req.Account, user); err != nil {
		return nil, err
	}
	if err := p.setupChallenges(client, req.Verification, req.DNSConfig, req.HTTP01Port); err != nil {
		return nil, err
	}
	obtain := certificate.ObtainRequest{
		Domains: req.Domains,
		Bundle:  true,
	}
	if req.PrivateKeyPEM != "" {
		privKey, err := parsePrivateKey(req.PrivateKeyPEM)
		if err != nil {
			return nil, err
		}
		obtain.PrivateKey = privKey
	}
	certs, err := client.Certificate.Obtain(obtain)
	if err != nil {
		return nil, fmt.Errorf("ACME 签发失败: %w", err)
	}
	if err := p.saveUser(req.Account, user); err != nil {
		return nil, err
	}
	issuer := string(certs.IssuerCertificate)
	notBefore, notAfter := certValidity(certs.Certificate)
	return &providers.IssuedCertificate{
		CertificatePEM: string(certs.Certificate),
		PrivateKeyPEM:  string(certs.PrivateKey), // lego 返回的即为 PEM 编码
		IssuerCA:       issuer,
		NotBefore:      notBefore,
		NotAfter:       notAfter,
	}, nil
}

func (p *Provider) Renew(ctx context.Context, req providers.RenewRequest) (*providers.IssuedCertificate, error) {
	user, err := p.loadUser(req.Account)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(req.Account, user)
	if err != nil {
		return nil, err
	}
	if err := p.setupChallenges(client, req.Verification, req.DNSConfig, 0); err != nil {
		return nil, err
	}
	privKey, err := parsePrivateKey(req.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	if privKey == nil {
		return nil, fmt.Errorf("续期需要旧证书私钥")
	}
	if len(req.Domains) == 0 {
		return nil, fmt.Errorf("续期需要至少一个域名")
	}
	renewReq := certificate.Resource{
		Domain:      req.Domains[0],
		Certificate: []byte(req.CertificatePEM),
		PrivateKey:  []byte(req.PrivateKeyPEM),
	}
	certs, err := client.Certificate.Renew(renewReq, true, false, "")
	if err != nil {
		return nil, fmt.Errorf("ACME 续期失败: %w", err)
	}
	if err := p.saveUser(req.Account, user); err != nil {
		return nil, err
	}
	notBefore, notAfter := certValidity(certs.Certificate)
	return &providers.IssuedCertificate{
		CertificatePEM: string(certs.Certificate),
		PrivateKeyPEM:  string(certs.PrivateKey),
		IssuerCA:       string(certs.IssuerCertificate),
		NotBefore:      notBefore,
		NotAfter:       notAfter,
	}, nil
}

func (p *Provider) Revoke(ctx context.Context, req providers.RevokeRequest) error {
	user, err := p.loadUser(req.Account)
	if err != nil {
		return err
	}
	client, err := p.newClient(req.Account, user)
	if err != nil {
		return err
	}
	return client.Certificate.Revoke([]byte(req.CertificatePEM))
}

// parsePrivateKey 解析 PEM 私钥为 ECDSA 密钥（空则返回 nil，lego 将自动生成）
func parsePrivateKey(pemStr string) (*ecdsa.PrivateKey, error) {
	if pemStr == "" {
		return nil, nil
	}
	key, err := parseECKey(pemStr)
	if err == nil {
		return key, nil
	}
	// 兼容 PKCS8
	block, _ := pem.Decode([]byte(pemStr))
	if block != nil {
		if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			if ec, ok := k.(*ecdsa.PrivateKey); ok {
				return ec, nil
			}
		}
	}
	return nil, fmt.Errorf("解析私钥失败: %w", err)
}

func (p *Provider) TestConnection(ctx context.Context, account *domain.ProviderAccount) error {
	user, err := p.loadUser(account)
	if err != nil {
		return err
	}
	client, err := p.newClient(account, user)
	if err != nil {
		return err
	}
	reg, err := p.registerAccount(ctx, client, account, user)
	if err != nil {
		return err
	}
	if reg {
		if err := p.saveUser(account, user); err != nil {
			return err
		}
	}
	return nil
}

// certValidity 从 PEM 解析证书有效期
func certValidity(certPEM []byte) (time.Time, time.Time) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, time.Time{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}
	}
	return cert.NotBefore, cert.NotAfter
}
