package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/certbundle"

	"github.com/iamartinlong/Argus/internal/application"
)

// LocalDeps 本地模式所需的业务能力（由 cmd 装配 bootstrap 提供）。
type LocalDeps struct {
	Certs    *application.CertificateService
	Deploy   *application.DeployService
	Accounts *application.ProviderAccountService
	DnsAccts *application.DnsProviderAccountService
	Agents   *application.AgentService
	TaskLogs interface {
		List(ctx context.Context, limit, offset int) ([]*domain.TaskLog, error)
	}
	Cryptor application.Cryptor
}

// LocalOps CertOps 的本地实现：直接调用 application 层服务（无网络、无 token）。
type LocalOps struct {
	deps LocalDeps
}

func NewLocalOps(deps LocalDeps) *LocalOps {
	return &LocalOps{deps: deps}
}

func (o *LocalOps) ListCertificates(ctx context.Context, f ListFilter) ([]*domain.Certificate, error) {
	certs, err := o.deps.Certs.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterCertificates(certs, f, time.Now()), nil
}

func (o *LocalOps) GetCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error) {
	if id != "" {
		return o.deps.Certs.Get(ctx, id)
	}
	certs, err := o.deps.Certs.List(ctx)
	if err != nil {
		return nil, err
	}
	name := normalizeDomainName(domainName)
	var match *domain.Certificate
	for _, c := range certs {
		if normalizeDomainName(c.PrimaryDomain) == name || certMatchesKeyword(c, name) {
			// 取到期最晚的一张（通配符/多证书场景）
			if match == nil || c.NotAfter.After(match.NotAfter) {
				match = c
			}
		}
	}
	if match == nil {
		return nil, domain.ErrNotFound
	}
	return match, nil
}

func (o *LocalOps) IssueCertificate(ctx context.Context, in IssueParams) (*domain.Certificate, error) {
	events := make([]domain.NotificationEvent, 0, len(in.NotifyEvents))
	for _, e := range in.NotifyEvents {
		events = append(events, domain.NotificationEvent(e))
	}
	return o.deps.Certs.Issue(ctx, application.IssueInput{
		Domains:              in.Domains,
		AccountID:            in.AccountID,
		Verification:         domain.VerificationType(in.Verification),
		DNSProviderAccountID: in.DNSProviderAccountID,
		DeployTargetIDs:      in.DeployTargetIDs,
		NotifyEvents:         events,
	})
}

func (o *LocalOps) RenewCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error) {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return nil, err
	}
	return o.deps.Certs.Renew(ctx, cert.ID, nil, cert.DNSProviderAccountID)
}

func (o *LocalOps) RevokeCertificate(ctx context.Context, id, domainName string) error {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return err
	}
	return o.deps.Certs.Revoke(ctx, cert.ID)
}

func (o *LocalOps) ImportCertificate(ctx context.Context, in ImportParams) (*domain.Certificate, error) {
	return o.deps.Certs.Import(ctx, in.Domains, in.Certificate, in.PrivateKey, domain.VerifyDNS01, nil)
}

func (o *LocalOps) ListTargets(ctx context.Context) ([]*domain.DeployTarget, error) {
	return o.deps.Deploy.ListTargets(ctx)
}

func (o *LocalOps) DeployCertificate(ctx context.Context, id, domainName string, targetIDs []string) error {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return err
	}
	ids := targetIDs
	if len(ids) == 0 {
		ids = cert.DeployTargetIDs
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: 该证书未绑定部署目标，请显式指定 target_ids", domain.ErrInvalidArgument)
	}
	return o.deps.Deploy.DeployToTargets(ctx, cert.ID, ids)
}

func (o *LocalOps) TestTarget(ctx context.Context, id string) error {
	return o.deps.Deploy.TestTarget(ctx, id)
}

func (o *LocalOps) ListAccounts(ctx context.Context, typ string) ([]AccountBrief, error) {
	var out []AccountBrief
	switch typ {
	case "", "acme":
		accounts, err := o.deps.Accounts.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range accounts {
			out = append(out, AccountBrief{ID: a.ID, Name: a.Name, Provider: a.Provider, Status: a.Status})
		}
	}
	switch typ {
	case "", "dns":
		accts, err := o.deps.DnsAccts.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			out = append(out, AccountBrief{ID: a.ID, Name: a.Name, Provider: string(a.Provider), Status: a.Status})
		}
	}
	if typ != "" && typ != "acme" && typ != "dns" {
		return nil, fmt.Errorf("%w: type 仅支持 acme / dns", domain.ErrInvalidArgument)
	}
	return out, nil
}

func (o *LocalOps) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	return o.deps.Agents.ListAgents(ctx)
}

func (o *LocalOps) TaskLogs(ctx context.Context, limit int) ([]*domain.TaskLog, error) {
	if limit <= 0 {
		limit = 20
	}
	return o.deps.TaskLogs.List(ctx, limit, 0)
}

func (o *LocalOps) DownloadCertificate(ctx context.Context, id, format string) ([]byte, string, error) {
	cert, err := o.deps.Certs.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if !certbundle.FormatSupported(format) {
		return nil, "", fmt.Errorf("%w: 不支持的格式 %q（支持 nginx/apache/iis/tomcat/other）", domain.ErrInvalidArgument, format)
	}
	if cert.PrivateKeyEnc == "" {
		return nil, "", fmt.Errorf("%w: 该证书无私钥（CSR 模式私钥在客户端）", domain.ErrNotFound)
	}
	priv, err := o.deps.Cryptor.DecryptString(cert.PrivateKeyEnc)
	if err != nil {
		return nil, "", fmt.Errorf("私钥解密失败: %w", err)
	}
	data, zipName, err := certbundle.BuildFormatZip(cert.CertificatePEM, priv, format)
	if err != nil {
		return nil, "", err
	}
	return data, cert.PrimaryDomain + "-" + zipName, nil
}

// certMatchesKeyword 域名模糊匹配（主域或任一 SAN）
func certMatchesKeyword(c *domain.Certificate, keyword string) bool {
	k := strings.ToLower(strings.TrimSpace(keyword))
	if k == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.PrimaryDomain), k) {
		return true
	}
	for _, d := range c.Domains {
		if strings.Contains(strings.ToLower(d), k) {
			return true
		}
	}
	return false
}

func normalizeDomainName(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}
