package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/acme"
	acmeapi "github.com/go-acme/lego/v4/acme/api"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

// IssueWithCSR 使用外部 CSR 签发证书（私钥保留在客户端，经 ACME finalize）
// 流程：解析并校验 CSR → 创建订单 → 完成域名验证（HTTP-01/DNS-01）→ finalize
func (p *Provider) IssueWithCSR(ctx context.Context, req providers.IssueRequest, csrPEM string) (*providers.IssuedCertificate, error) {
	csr, err := parseCSR(csrPEM)
	if err != nil {
		return nil, err
	}
	domains := append([]string{csr.Subject.CommonName}, csr.DNSNames...)
	domains = dedupStrings(domains)
	if len(domains) == 0 {
		return nil, fmt.Errorf("CSR 未包含域名（Subject CN 或 SAN DNS）")
	}
	if len(domains) > 100 {
		return nil, fmt.Errorf("CSR 域名数量超过上限（100）")
	}

	// 账户（已注册则复用 kid；未注册先注册）
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
	if user.reg == nil || user.reg.URI == "" {
		return nil, fmt.Errorf("ACME 账户未注册")
	}

	dir := req.Account.DirectoryURL
	if dir == "" {
		dir = lego.LEDirectoryProduction
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if p.rootCAs != nil {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: p.rootCAs},
		}
	}
	core, err := acmeapi.New(httpClient, "argus", dir, user.reg.URI, user.privateKey)
	if err != nil {
		return nil, fmt.Errorf("创建 ACME API 客户端: %w", err)
	}

	order, err := core.Orders.New(domains)
	if err != nil {
		return nil, fmt.Errorf("创建订单失败: %w", err)
	}

	// 完成各授权域名的验证
	thumbprint, err := jwkThumbprint(&user.privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	for _, authzURL := range order.Authorizations {
		if err := p.solveAuthorization(ctx, core, req, authzURL, thumbprint); err != nil {
			return nil, err
		}
	}

	// finalize（lego 封装 JWS POST）
	finalOrder, err := core.Orders.UpdateForCSR(order.Finalize, csr.Raw)
	if err != nil {
		return nil, fmt.Errorf("finalize 失败: %w", err)
	}

	// 轮询订单至 valid
	var certURL string
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("等待证书签发超时")
		}
		if finalOrder.Status == "valid" && finalOrder.Certificate != "" {
			certURL = finalOrder.Certificate
			break
		}
		if finalOrder.Status == "invalid" {
			return nil, fmt.Errorf("订单状态 invalid")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		got, err := core.Orders.Get(order.Location)
		if err != nil {
			return nil, err
		}
		if got.Status == "valid" && got.Certificate != "" {
			certURL = got.Certificate
			break
		}
		finalOrder = got
	}

	chain, _, err := core.Certificates.Get(certURL, true)
	if err != nil {
		return nil, fmt.Errorf("下载证书失败: %w", err)
	}
	// 持久化账户密钥与注册信息（首次注册后必须保存，否则下次签发会重新注册新账户）
	if err := p.saveUser(req.Account, user); err != nil {
		return nil, fmt.Errorf("保存账户凭据失败: %w", err)
	}
	notBefore, notAfter := certValidity(chain)
	return &providers.IssuedCertificate{
		CertificatePEM: string(chain),
		PrivateKeyPEM:  "", // 外部 CSR 模式：私钥由客户端持有，服务端不落盘
		IssuerCA:       "",
		NotBefore:      notBefore,
		NotAfter:       notAfter,
	}, nil
}

// solveAuthorization 完成单个授权的域名验证
func (p *Provider) solveAuthorization(ctx context.Context, core *acmeapi.Core, req providers.IssueRequest, authzURL, thumbprint string) error {
	authz, err := core.Authorizations.Get(authzURL)
	if err != nil {
		return fmt.Errorf("获取授权失败: %w", err)
	}
	if authz.Status == "valid" {
		return nil
	}
	var chl *acme.Challenge
	for i := range authz.Challenges {
		c := &authz.Challenges[i]
		if req.Verification == domain.VerifyHTTP01 && c.Type == "http-01" {
			chl = c
			break
		}
		if req.Verification == domain.VerifyDNS01 && c.Type == "dns-01" {
			chl = c
			break
		}
	}
	if chl == nil {
		return fmt.Errorf("授权不包含 %s 类型挑战", req.Verification)
	}
	keyAuth := chl.Token + "." + thumbprint

	// 部署验证资源
	if req.Verification == domain.VerifyHTTP01 {
		if p.http01 == nil {
			p.http01Mu.Lock()
			if p.http01 == nil {
				port := "80"
				if req.HTTP01Port > 0 {
					port = fmt.Sprintf("%d", req.HTTP01Port)
				}
				p.http01 = http01.NewProviderServer("0.0.0.0", port)
			}
			p.http01Mu.Unlock()
		}
		if err := p.http01.Present(authz.Identifier.Value, chl.Token, keyAuth); err != nil {
			return fmt.Errorf("HTTP-01 部署失败: %w", err)
		}
		defer p.http01.CleanUp(authz.Identifier.Value, chl.Token, keyAuth)
	} else {
		dp, err := p.dnsProvider(req.DNSConfig)
		if err != nil {
			return err
		}
		if err := dp.Present(authz.Identifier.Value, chl.Token, keyAuth); err != nil {
			return fmt.Errorf("DNS-01 部署失败: %w", err)
		}
		defer dp.CleanUp(authz.Identifier.Value, chl.Token, keyAuth)
	}

	// Accept 挑战
	if _, err := core.Challenges.New(chl.URL); err != nil {
		return fmt.Errorf("接受挑战失败: %w", err)
	}

	// 轮询授权至 valid
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("域名验证超时: %s", authz.Identifier.Value)
		}
		cur, err := core.Authorizations.Get(authzURL)
		if err != nil {
			return err
		}
		if cur.Status == "valid" {
			return nil
		}
		if cur.Status == "invalid" {
			return fmt.Errorf("域名验证失败: %s", authz.Identifier.Value)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// parseCSR 解析并校验 CSR 签名（证明持有对应私钥）
func parseCSR(pemStr string) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("无效的 CSR PEM（需要 CERTIFICATE REQUEST 块）")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析 CSR 失败: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR 签名验证失败: %w", err)
	}
	return csr, nil
}

// jwkThumbprint RFC7638 账户公钥 thumbprint（keyAuthorization 组成部分）
func jwkThumbprint(pub *ecdsa.PublicKey) (string, error) {
	// 仅支持 P-256（账户密钥为 EC256）
	size := (pub.Curve.Params().BitSize + 7) / 8
	x := pub.X.Bytes()
	y := pub.Y.Bytes()
	if len(x) < size {
		x = append(make([]byte, size-len(x)), x...)
	}
	if len(y) < size {
		y = append(make([]byte, size-len(y)), y...)
	}
	jwk := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":%q,"y":%q}`,
		base64.RawURLEncoding.EncodeToString(x),
		base64.RawURLEncoding.EncodeToString(y))
	// 规范序列化：固定键序（crv,kty,x,y）已满足 RFC7638
	sum := sha256.Sum256([]byte(jwk))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
