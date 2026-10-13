package application

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/pkcs12fix"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

// ProviderEngine 抽象签发渠道（由 providers.Registry 实现）
type ProviderEngine interface {
	Get(name string) (providers.CertificateProvider, error)
	Names() []string
}

// ErrCSRCertNotRenewable CSR 模式证书无法自动续期（私钥在客户端）
var ErrCSRCertNotRenewable = errors.New("csr certificate cannot be auto-renewed")

type Cryptor interface {
	EncryptString(s string) (string, error)
	DecryptString(enc string) (string, error)
}

type CertificateService struct {
	certs      domain.CertificateStore
	orders     domain.OrderStore
	accounts   domain.ProviderAccountStore
	dnsAccounts domain.DnsProviderAccountStore
	contacts   domain.ContactStore
	audit      domain.AuditStore
	providers  ProviderEngine
	cryptor    Cryptor
	clock      Clock
	httpPort   int // HTTP-01 验证服务器端口（0 默认 80）
	bus        *EventBus
	// renewMu 按 certID 互斥：cron 扫描/手动续期/批量续期并发触发同一证书时串行化，
	// 避免重复 ACME 下单（易触发 CA 速率限制）与记录互相覆盖
	renewMu sync.Map
}

func NewCertificateService(
	certs domain.CertificateStore,
	orders domain.OrderStore,
	accounts domain.ProviderAccountStore,
	contacts domain.ContactStore,
	audit domain.AuditStore,
	providers ProviderEngine,
	cryptor Cryptor,
	clock Clock,
	httpPort int,
	bus *EventBus,
) *CertificateService {
	if bus == nil {
		bus = NewEventBus()
	}
	return &CertificateService{
		certs: certs, orders: orders, accounts: accounts, contacts: contacts, audit: audit,
		providers: providers, cryptor: cryptor, clock: clock, httpPort: httpPort, bus: bus,
	}
}

// SetDnsAccounts 注入 DNS 账户存储（可选，避免循环依赖）
func (s *CertificateService) SetDnsAccounts(store domain.DnsProviderAccountStore) {
	s.dnsAccounts = store
}

// UpdateStatus 仅更新证书状态列（调度扫描持久化过渡态用），
// 避免整行 Update 与并发续期写入互相覆盖
func (s *CertificateService) UpdateStatus(ctx context.Context, certID string, status domain.CertificateStatus) error {
	return s.certs.UpdateStatus(ctx, certID, status)
}

type IssueInput struct {
	Domains              []string
	AccountID            string
	ContactID            string // 云渠道签发时使用的申请人；空则用全局第一个
	Verification         domain.VerificationType
	DNSConfig            map[string]string
	DNSProviderAccountID string // DNS 验证账户（可空，向后兼容旧 DNSConfig）
	DeployTargetIDs      []string // 签发成功后自动部署的目标
	CSRPEM               string   // 外部 CSR 模式（私钥保留在客户端，仅 ACME 渠道）
	NotifyEvents         []domain.NotificationEvent // 该证书订阅的通知事件（空=全部）
}

func (s *CertificateService) Issue(ctx context.Context, input IssueInput) (*domain.Certificate, error) {
	csrMode := input.CSRPEM != ""
	if !csrMode && len(input.Domains) == 0 {
		return nil, fmt.Errorf("%w: 至少需要一个域名", domain.ErrInvalidArgument)
	}
	if !input.Verification.Valid() {
		return nil, fmt.Errorf("%w: 验证方式必须为 http01 或 dns01", domain.ErrInvalidArgument)
	}
	if input.CSRPEM == "" {
		for _, d := range input.Domains {
			clean := strings.ToLower(strings.TrimSpace(d))
			if !validImportDomain(clean) {
				return nil, fmt.Errorf("%w: 非法域名 %q", domain.ErrInvalidArgument, d)
			}
		}
	}
	if err := validNotifyEvents(input.NotifyEvents); err != nil {
		return nil, err
	}
	account, err := s.accounts.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("%w: 渠道账户不存在", domain.ErrNotFound)
	}
	provider, err := s.providers.Get(account.Provider)
	if err != nil {
		return nil, err
	}
	// CSR 模式仅支持 ACME 渠道（云厂商 API 不接受外部 CSR）
	if csrMode && account.Provider != "acme" {
		return nil, fmt.Errorf("%w: CSR 签发仅支持 ACME 渠道（当前 %s）", domain.ErrInvalidArgument, account.Provider)
	}

	cert := &domain.Certificate{
		ID:                   newID(),
		Domains:              input.Domains,
		Provider:             account.Provider,
		Status:               domain.StatusPending,
		AccountID:            account.ID,
		Verification:         input.Verification,
		DNSProviderAccountID: input.DNSProviderAccountID,
		DeployTargetIDs:      input.DeployTargetIDs,
		NotifyEvents:          dedupNotifyEvents(input.NotifyEvents),
		CreatedAt:             s.clock.Now(),
		UpdatedAt:            s.clock.Now(),
	}
	if len(input.Domains) > 0 {
		cert.PrimaryDomain = input.Domains[0]
	}
	// DNS-01 凭据：优先使用 DNS 账户，回退旧 DNSConfig
	dnsConfig := input.DNSConfig
	if input.DNSProviderAccountID != "" && s.dnsAccounts != nil {
		if cfg, err := s.resolveDnsConfig(ctx, input.DNSProviderAccountID); err != nil {
			return nil, fmt.Errorf("解析 DNS 账户失败: %w", err)
		} else {
			dnsConfig = cfg
		}
	}
	// DNS-01 凭据加密持久化，供自动续期复用
	if len(dnsConfig) > 0 {
		enc, err := s.cryptor.EncryptString(mustJSON(dnsConfig))
		if err != nil {
			return nil, err
		}
		cert.DNSConfigEnc = enc
	}
	if err := s.certs.Create(ctx, cert); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditCertIssue, cert.PrimaryDomain, "domains="+strings.Join(cert.Domains, ","))
	order := s.newOrder(ctx, cert.ID, domain.OrderIssue)

	account, contact, err := s.accountWithContact(ctx, account, input.ContactID)
	if err != nil {
		// 回写失败状态，避免悬挂 pending 证书 + running 订单
		cert.Status = domain.StatusFailed
		cert.LastError = err.Error()
		_ = s.certs.Update(ctx, cert)
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		s.finishOrder(ctx, order)
		s.bus.Publish(EventCertificateIssueFail, cert)
		return cert, fmt.Errorf("签发失败: %w", err)
	}
	// 申请人信息快照：签发时固化，不关联 contact_persons（后续联系人变更不影响历史证书）
	if contact != nil {
		cert.ContactName = contact.Name
		cert.ContactEmail = contact.Email
		cert.ContactPhone = contact.Phone
		_ = s.certs.Update(ctx, cert)
	}

	issued, err := provider.Issue(ctx, providers.IssueRequest{
		Account:      account,
		Domains:      input.Domains,
		Verification: input.Verification,
		DNSConfig:    dnsConfig,
		HTTP01Port:   s.httpPort,
		CSRPEM:       input.CSRPEM,
	})
	if err != nil {
		cert.Status = domain.StatusFailed
		cert.LastError = err.Error()
		_ = s.certs.Update(ctx, cert)
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		order.Attempts = 1
		s.finishOrder(ctx, order)
		s.bus.Publish(EventCertificateIssueFail, cert)
		return cert, fmt.Errorf("签发失败: %w", err)
	}

	if err := s.applyIssued(ctx, cert, account.Provider, issued); err != nil {
		// 落库失败（如私钥加密/写库异常）时收尾订单，否则永远停在 running
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		s.finishOrder(ctx, order)
		s.bus.Publish(EventCertificateIssueFail, cert)
		return cert, err
	}
	order.Status = domain.OrderSuccess
	s.finishOrder(ctx, order)
	s.bus.Publish(EventCertificateIssued, cert)
	return cert, nil
}

// accountWithContact 云渠道签发/续期前注入全局申请人信息。
// - ACME 渠道无需申请人，原样返回（contact=nil）
// - contactID 非空 → 使用指定申请人；为空 → 使用全局第一个
// - 未配置申请人时报错（云渠道签发必须携带联系人）
// 实现：临时解密账户凭据 → 覆盖 contact_* 字段 → 重新加密，返回副本（不落库）
// 返回注入后的账户副本与使用的联系人快照（ACME/无凭据时 contact=nil）
func (s *CertificateService) accountWithContact(ctx context.Context, account *domain.ProviderAccount, contactID string) (*domain.ProviderAccount, *domain.ContactPerson, error) {
	if account.Provider == "acme" || s.contacts == nil {
		return account, nil, nil
	}
	if account.CredentialsEnc == "" {
		return account, nil, nil // 无凭据的云账户后续签发会报错
	}
	var contact *domain.ContactPerson
	if contactID != "" {
		var err error
		contact, err = s.contacts.GetByID(ctx, contactID)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: 申请人不存在", domain.ErrNotFound)
		}
	} else {
		list, err := s.contacts.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(list) > 0 {
			contact = list[0]
		}
	}
	if contact == nil {
		return nil, nil, fmt.Errorf("%w: 请先在系统→申请人信息 配置联系人（云渠道签发需要申请人邮箱/电话）", domain.ErrInvalidArgument)
	}
	plain, err := s.cryptor.DecryptString(account.CredentialsEnc)
	if err != nil {
		return nil, nil, fmt.Errorf("解密账户凭据失败: %w", err)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, nil, err
	}
	cfg["contact_email"] = contact.Email
	cfg["contact_phone"] = contact.Phone
	if account.Provider == "aliyun" {
		cfg["contact_name"] = contact.Name
	}
	enc, err := s.cryptor.EncryptString(mustJSON(cfg))
	if err != nil {
		return nil, nil, err
	}
	clone := *account
	clone.CredentialsEnc = enc
	return &clone, contact, nil
}

func (s *CertificateService) Renew(ctx context.Context, certID string, dnsConfig map[string]string, dnsProviderAccountID string) (*domain.Certificate, error) {
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return nil, err
	}
	if cert.Status == domain.StatusRevoked {
		return nil, fmt.Errorf("%w: 已吊销的证书不能续期", domain.ErrInvalidArgument)
	}
	// CSR 模式证书：私钥由客户端持有，无法自动续期（避免每轮扫描失败噪音）
	if cert.PrivateKeyEnc == "" {
		return nil, fmt.Errorf("%w: CSR 模式证书私钥由客户端持有，无法自动续期，请重新签发", ErrCSRCertNotRenewable)
	}
	// 导入的证书没有签发渠道/账户，无法重新向 CA 签发（避免以空账户查询误报 not found）
	if cert.AccountID == "" {
		return nil, fmt.Errorf("%w: 导入的证书无法续期（无签发渠道），请重新导入证书", domain.ErrInvalidArgument)
	}
	// 单证书互斥 + 双重检查：拿锁期间其他调用方（扫描/批量/手动并发）已完成续期时，
	// 直接返回新证书，不再向 CA 重复下单
	muAny, _ := s.renewMu.LoadOrStore(certID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if fresh, ferr := s.certs.GetByID(ctx, certID); ferr == nil &&
		!fresh.NotAfter.IsZero() && fresh.NotAfter.After(cert.NotAfter) {
		return fresh, nil
	}
	account, err := s.accounts.GetByID(ctx, cert.AccountID)
	if err != nil {
		return nil, err
	}
	provider, err := s.providers.Get(account.Provider)
	if err != nil {
		return nil, err
	}
	order := s.newOrder(ctx, cert.ID, domain.OrderRenew)
	// 续期优先复用已有私钥
	privKey := ""
	if cert.PrivateKeyEnc != "" {
		privKey, err = s.cryptor.DecryptString(cert.PrivateKeyEnc)
		if err != nil {
			privKey = ""
		}
	}
	// DNS-01：显式传入的 DNS 账户优先，其次证书关联账户，最后回退旧 DNSConfigEnc
	if dnsProviderAccountID != "" && s.dnsAccounts != nil {
		if cfg, err := s.resolveDnsConfig(ctx, dnsProviderAccountID); err != nil {
			return nil, fmt.Errorf("解析 DNS 账户失败: %w", err)
		} else {
			dnsConfig = cfg
			// 显式更换 DNS 账户时回填证书，供后续自动续期复用
			cert.DNSProviderAccountID = dnsProviderAccountID
			if enc, err := s.cryptor.EncryptString(mustJSON(dnsConfig)); err == nil {
				cert.DNSConfigEnc = enc
			}
			_ = s.certs.Update(ctx, cert)
		}
	} else if cert.DNSProviderAccountID != "" && s.dnsAccounts != nil {
		if cfg, err := s.resolveDnsConfig(ctx, cert.DNSProviderAccountID); err == nil {
			dnsConfig = cfg
		} else if len(dnsConfig) == 0 && cert.DNSConfigEnc != "" {
			if plain, derr := s.cryptor.DecryptString(cert.DNSConfigEnc); derr == nil {
				_ = json.Unmarshal([]byte(plain), &dnsConfig)
			}
		}
	} else if len(dnsConfig) == 0 && cert.DNSConfigEnc != "" {
		if plain, derr := s.cryptor.DecryptString(cert.DNSConfigEnc); derr == nil {
			_ = json.Unmarshal([]byte(plain), &dnsConfig)
		}
	}
	account, contact, err := s.accountWithContact(ctx, account, "")
	if err != nil {
		return nil, err
	}
	// 续期同样固化当前申请人快照
	if contact != nil {
		cert.ContactName = contact.Name
		cert.ContactEmail = contact.Email
		cert.ContactPhone = contact.Phone
		_ = s.certs.Update(ctx, cert)
	}
	issued, err := provider.Renew(ctx, providers.RenewRequest{
		Account:        account,
		Domains:        cert.Domains,
		Verification:   cert.Verification,
		DNSConfig:      dnsConfig,
		CertificatePEM: cert.CertificatePEM,
		PrivateKeyPEM:  privKey,
	})
	if err != nil {
		cert.Status = domain.StatusFailed
		cert.LastError = err.Error()
		_ = s.certs.Update(ctx, cert)
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		s.finishOrder(ctx, order)
		s.bus.Publish(EventCertificateRenewFail, cert)
		return cert, fmt.Errorf("续期失败: %w", err)
	}
	if err := s.applyIssued(ctx, cert, account.Provider, issued); err != nil {
		// 同上：续期结果落库失败也要收尾订单
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		s.finishOrder(ctx, order)
		s.bus.Publish(EventCertificateRenewFail, cert)
		return cert, err
	}
	order.Status = domain.OrderSuccess
	s.finishOrder(ctx, order)
	s.auditAction(ctx, nil, domain.AuditCertRenew, cert.PrimaryDomain, "")
	s.bus.Publish(EventCertificateRenewed, cert)
	return cert, nil
}

func (s *CertificateService) Revoke(ctx context.Context, certID string) error {
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return err
	}
	if cert.Provider == "" || cert.CertificatePEM == "" {
		return fmt.Errorf("%w: 无法吊销", domain.ErrInvalidArgument)
	}
	// 导入的证书没有签发渠道/账户，无法在 CA 吊销（避免以空账户查询误报 not found）
	if cert.AccountID == "" {
		return fmt.Errorf("%w: 导入的证书无法吊销（无签发渠道），请直接在证书管理中删除", domain.ErrInvalidArgument)
	}
	account, err := s.accounts.GetByID(ctx, cert.AccountID)
	if err != nil {
		return err
	}
	provider, err := s.providers.Get(account.Provider)
	if err != nil {
		return err
	}
	order := s.newOrder(ctx, cert.ID, domain.OrderRevoke)
	if err := provider.Revoke(ctx, providers.RevokeRequest{
		Account:        account,
		CertificatePEM: cert.CertificatePEM,
	}); err != nil {
		order.Status = domain.OrderFailed
		order.Error = err.Error()
		s.finishOrder(ctx, order)
		return fmt.Errorf("吊销失败: %w", err)
	}
	cert.Status = domain.StatusRevoked
	cert.UpdatedAt = s.clock.Now()
	if err := s.certs.Update(ctx, cert); err != nil {
		return err
	}
	order.Status = domain.OrderSuccess
	s.finishOrder(ctx, order)
	s.auditAction(ctx, nil, domain.AuditCertRevoke, cert.PrimaryDomain, "")
	s.bus.Publish(EventCertificateRevoked, cert)
	return nil
}

func (s *CertificateService) Import(
	ctx context.Context,
	domains []string,
	certPEM, privKeyPEM string,
	verification domain.VerificationType,
	notifyEvents []domain.NotificationEvent,
) (*domain.Certificate, error) {
	if certPEM == "" || privKeyPEM == "" {
		return nil, fmt.Errorf("%w: 证书与私钥不能为空", domain.ErrInvalidArgument)
	}
	if err := validNotifyEvents(notifyEvents); err != nil {
		return nil, err
	}
	nb, na, err := certValidity(certPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidArgument, err)
	}
	privEnc, err := s.cryptor.EncryptString(privKeyPEM)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		domains = []string{extractCN(certPEM)}
	}
	// 域名强制格式校验：防注入部署 hook 命令（{domain} 模板拼接 shell）
	// 同时与证书实际 SAN 一致性由上层 UI 保证；此处仅拦截非法字符
	normalized := make([]string, 0, len(domains))
	for _, d := range domains {
		clean := strings.ToLower(strings.TrimSpace(d))
		if !validImportDomain(clean) {
			return nil, fmt.Errorf("%w: 非法域名 %q（仅允许字母/数字/点/连字符，≤253 字符）", domain.ErrInvalidArgument, d)
		}
		normalized = append(normalized, clean)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("%w: 至少需要一个域名", domain.ErrInvalidArgument)
	}
	cert := &domain.Certificate{
		ID:             newID(),
		Domains:        normalized,
		PrimaryDomain:  normalized[0],
		Provider:       "import",
		Status:         domain.StatusValid,
		NotBefore:      nb,
		NotAfter:       na,
		CertificatePEM: certPEM,
		PrivateKeyEnc:  privEnc,
		Verification:   verification,
		NotifyEvents:    dedupNotifyEvents(notifyEvents),
		CreatedAt:       s.clock.Now(),
		UpdatedAt:      s.clock.Now(),
	}
	if err := s.certs.Create(ctx, cert); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditCertImport, cert.PrimaryDomain, "domains="+strings.Join(cert.Domains, ","))
	return cert, nil
}

// ImportPFX 从 PKCS#12（.pfx/.p12）导入证书与私钥（云厂商 Windows/IIS 常见格式）。
// 多域名从证书 SAN/CN 自动提取；私钥序列化为 PKCS8 PEM。
func (s *CertificateService) ImportPFX(
	ctx context.Context,
	pfxData []byte,
	password string,
	verification domain.VerificationType,
	notifyEvents []domain.NotificationEvent,
) (*domain.Certificate, error) {
	if len(pfxData) == 0 {
		return nil, fmt.Errorf("%w: PFX 内容不能为空", domain.ErrInvalidArgument)
	}
	if err := validNotifyEvents(notifyEvents); err != nil {
		return nil, err
	}
	// BER 编码兼容：部分平台（如阿里云）导出的 PFX 使用 ASN.1 不定长编码，
	// Go 的 DER 解析器会拒绝；先修复为 DER（含 MAC 重算），失败则按原样尝试
	// （正常 DER 文件走此分支也无副作用）。
	repaired, rerr := pkcs12fix.Repair(pfxData, password)
	if rerr == nil {
		pfxData = repaired
	}
	privKey, leaf, caCerts, err := pkcs12.DecodeChain(pfxData, password)
	if err != nil {
		// 密码错误（MAC 校验或私钥包解密失败）与格式问题给出一站式提示
		if errors.Is(err, pkcs12.ErrIncorrectPassword) || errors.Is(err, pkcs12.ErrDecryption) {
			return nil, fmt.Errorf("%w: PFX 密码错误（或文件损坏）", domain.ErrInvalidArgument)
		}
		return nil, fmt.Errorf("%w: PFX 解析失败（密码错误或格式不符）: %v", domain.ErrInvalidArgument, err)
	}
	// 校验私钥与叶子证书公钥匹配，避免导入损坏的证书
	if !keyMatchesCert(leaf, privKey) {
		return nil, fmt.Errorf("%w: PFX 中的私钥与证书不匹配", domain.ErrInvalidArgument)
	}
	// 私钥序列化为 PKCS8 PEM
	keyDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("%w: 私钥序列化失败: %v", domain.ErrInvalidArgument, err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	// 证书序列化：叶子 + 中间/根链（多块）
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}))
	for _, c := range caCerts {
		certPEM += string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
	}

	// 多域名从 SAN/CN 提取
	domains := make([]string, 0, len(leaf.DNSNames)+1)
	seen := map[string]bool{}
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	for _, d := range leaf.DNSNames {
		add(d)
	}
	add(leaf.Subject.CommonName)
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: PFX 证书未包含域名", domain.ErrInvalidArgument)
	}

	return s.Import(ctx, domains, certPEM, keyPEM, verification, notifyEvents)
}

// validImportDomain 域名格式校验：字母数字/点/连字符，允许通配符前缀 *.
func validImportDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	rest := d
	if strings.HasPrefix(rest, "*.") {
		rest = rest[2:]
	}
	for _, r := range rest {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func (s *CertificateService) List(ctx context.Context) ([]*domain.Certificate, error) {
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range certs {
		c.UpdateStatus(s.clock.Now())
	}
	return certs, nil
}

func (s *CertificateService) Get(ctx context.Context, id string) (*domain.Certificate, error) {
	cert, err := s.certs.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cert.UpdateStatus(s.clock.Now())
	return cert, nil
}

func (s *CertificateService) Delete(ctx context.Context, id string) error {
	cert, err := s.certs.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.certs.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAction(ctx, nil, domain.AuditCertDelete, cert.PrimaryDomain, "")
	return nil
}

func (s *CertificateService) Orders(ctx context.Context, certID string) ([]*domain.Order, error) {
	return s.orders.ListByCertificate(ctx, certID)
}

// UpdateTargets 更新证书的部署目标绑定
func (s *CertificateService) UpdateTargets(ctx context.Context, certID string, targetIDs []string) error {
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return err
	}
	cert.DeployTargetIDs = targetIDs
	cert.UpdatedAt = s.clock.Now()
	if err := s.certs.Update(ctx, cert); err != nil {
		return err
	}
	s.auditAction(ctx, nil, domain.AuditCertTargets, cert.PrimaryDomain, "targets="+strings.Join(targetIDs, ","))
	return nil
}

// UpdateNotifyEvents 更新证书的通知事件订阅（签发后可在详情页调整）
func (s *CertificateService) UpdateNotifyEvents(ctx context.Context, certID string, notifyEvents []domain.NotificationEvent) (*domain.Certificate, error) {
	if err := validNotifyEvents(notifyEvents); err != nil {
		return nil, err
	}
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return nil, err
	}
	cert.NotifyEvents = dedupNotifyEvents(notifyEvents)
	cert.UpdatedAt = s.clock.Now()
	if err := s.certs.Update(ctx, cert); err != nil {
		return nil, err
	}
	s.auditAction(ctx, nil, domain.AuditCertNotify, cert.PrimaryDomain, "events="+strings.Join(stringEvents(notifyEvents), ","))
	return cert, nil
}

func stringEvents(events []domain.NotificationEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e))
	}
	return out
}

// applyIssued 将签发结果写入证书（私钥加密存储）
func (s *CertificateService) applyIssued(ctx context.Context, cert *domain.Certificate, providerName string, issued *providers.IssuedCertificate) error {
	privEnc := ""
	if issued.PrivateKeyPEM != "" { // CSR 模式私钥由客户端持有，不落盘
		enc, err := s.cryptor.EncryptString(issued.PrivateKeyPEM)
		if err != nil {
			return err
		}
		privEnc = enc
	}
	cert.CertificatePEM = issued.CertificatePEM
	cert.PrivateKeyEnc = privEnc
	cert.IssuerCA = issued.IssuerCA
	cert.NotBefore = issued.NotBefore
	cert.NotAfter = issued.NotAfter
	cert.Provider = providerName
	cert.LastError = ""
	cert.UpdatedAt = s.clock.Now()
	cert.UpdateStatus(s.clock.Now())
	return s.certs.Update(ctx, cert)
}

func (s *CertificateService) newOrder(ctx context.Context, certID string, typ domain.OrderType) *domain.Order {
	order := &domain.Order{
		ID:            newID(),
		CertificateID: certID,
		Type:          typ,
		Status:        domain.OrderRunning,
		StartedAt:     s.clock.Now(),
	}
	if err := s.orders.Create(ctx, order); err != nil {
		log.Printf("创建订单记录失败 (cert=%s type=%s): %v", certID, typ, err)
	}
	return order
}

func (s *CertificateService) finishOrder(ctx context.Context, order *domain.Order) {
	now := s.clock.Now()
	order.FinishedAt = &now
	if err := s.orders.Update(ctx, order); err != nil {
		log.Printf("更新订单记录失败 (order=%s status=%s): %v", order.ID, order.Status, err)
	}
}

// auditAction 记录审计（actor 为 nil 时由调度器等系统触发）
func (s *CertificateService) auditAction(ctx context.Context, actor *domain.User, action domain.AuditAction, resource, detail string) {
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

func (s *CertificateService) resolveDnsConfig(ctx context.Context, dnsAccountID string) (map[string]string, error) {
	account, err := s.dnsAccounts.GetByID(ctx, dnsAccountID)
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

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// validNotifyEvents 校验通知事件合法性；空列表合法（表示全部事件）
func validNotifyEvents(events []domain.NotificationEvent) error {
	for _, e := range events {
		if !e.Valid() {
			return fmt.Errorf("%w: 无效通知事件 %s", domain.ErrInvalidArgument, e)
		}
	}
	return nil
}

// dedupNotifyEvents 去重并保持顺序；空输入返回 nil（存储为 "[]"，过滤逻辑视为全部事件）
func dedupNotifyEvents(events []domain.NotificationEvent) []domain.NotificationEvent {
	if len(events) == 0 {
		return nil
	}
	seen := map[domain.NotificationEvent]bool{}
	out := make([]domain.NotificationEvent, 0, len(events))
	for _, e := range events {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
