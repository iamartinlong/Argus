package domain

import "time"

type CertificateStatus string

const (
	StatusPending  CertificateStatus = "pending"
	StatusValid    CertificateStatus = "valid"
	StatusExpiring CertificateStatus = "expiring"
	StatusExpired  CertificateStatus = "expired"
	StatusRevoked  CertificateStatus = "revoked"
	StatusFailed   CertificateStatus = "failed"
)

type VerificationType string

const (
	VerifyHTTP01 VerificationType = "http01"
	VerifyDNS01  VerificationType = "dns01"
)

func (v VerificationType) Valid() bool {
	return v == VerifyHTTP01 || v == VerifyDNS01
}

type Certificate struct {
	ID             string
	Domains        []string
	PrimaryDomain  string
	Provider       string // acme | aliyun | tencent | import
	Status         CertificateStatus
	IssuerCA       string
	NotBefore      time.Time
	NotAfter       time.Time
	CertificatePEM string
	PrivateKeyEnc  string // AES-GCM 密文
	AccountID            string // ProviderAccount.ID
	Verification         VerificationType
	DNSConfigEnc         string // DNS-01 凭据 JSON 的 AES-GCM 密文（自动续期复用，兼容旧数据）
	DNSProviderAccountID string // DNS 验证账户 ID（可空，向后兼容旧 DNSConfigEnc）
	DeployTargetIDs []string // 关联部署目标（签发/续期成功后自动部署）
	NotifyEvents   []NotificationEvent // 通知事件勾选（空=全部事件可通知，向后兼容）
	ContactName    string // 申请人快照（签发时固化，不关联 contact_persons）
	ContactEmail   string
	ContactPhone   string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (c *Certificate) UpdateStatus(now time.Time) {
	// revoked/failed 为终态；pending 无有效期时保持
	if c.Status == StatusRevoked || c.Status == StatusFailed {
		return
	}
	if c.NotAfter.IsZero() {
		c.Status = StatusPending
		return
	}
	switch {
	case now.After(c.NotAfter):
		c.Status = StatusExpired
	case now.Add(30 * 24 * time.Hour).After(c.NotAfter):
		c.Status = StatusExpiring
	default:
		c.Status = StatusValid
	}
}

type OrderType string

const (
	OrderIssue OrderType = "issue"
	OrderRenew OrderType = "renew"
	OrderRevoke OrderType = "revoke"
)

type OrderStatus string

const (
	OrderRunning OrderStatus = "running"
	OrderSuccess OrderStatus = "success"
	OrderFailed  OrderStatus = "failed"
)

type Order struct {
	ID            string
	CertificateID string
	Type          OrderType
	Status        OrderStatus
	Error         string
	Attempts      int
	StartedAt     time.Time
	FinishedAt    *time.Time
}

type ProviderAccount struct {
	ID             string
	Provider       string // acme | aliyun | tencent
	Name           string
	DirectoryURL   string // acme 目录
	Contact        string // acme 邮箱
	EABKID         string // EAB kid（加密前存储）
	EABHMACKeyEnc  string // EAB hmac key 密文
	CredentialsEnc string // 云凭据/账号密钥 密文（JSON）
	Status         string // active | error
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
