package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// DomainService 域名台账（主数据）服务。
//
// 台账与证书不建映射表：关联 = certificates.domain_id（引用）∪ 域名匹配，
// 覆盖状态在 Ledger() 读取时实时计算（与调度器全量内存处理同款风格）。
type DomainService struct {
	domains domain.DomainStore
	certs   domain.CertificateStore
	audit   domain.AuditStore
	clock   Clock
}

func NewDomainService(domains domain.DomainStore, certs domain.CertificateStore, audit domain.AuditStore, clock Clock) *DomainService {
	return &DomainService{domains: domains, certs: certs, audit: audit, clock: clock}
}

// DomainInput 台账项创建/更新入参（两者同构，更新留空字段保持原值）
type DomainInput struct {
	Name           string
	ExpectedStatus string
	Note           string
	Tags           []string
}

func (s *DomainService) List(ctx context.Context) ([]*domain.Domain, error) {
	return s.domains.List(ctx)
}

func (s *DomainService) Get(ctx context.Context, id string) (*domain.Domain, error) {
	return s.domains.GetByID(ctx, id)
}

func (s *DomainService) Create(ctx context.Context, actor *domain.User, input DomainInput) (*domain.Domain, error) {
	name := domain.NormalizeDomain(input.Name)
	if !domain.ValidDomain(name) {
		return nil, fmt.Errorf("%w: 非法域名 %q（仅允许字母/数字/点/连字符，可带 *. 前缀，≤253 字符）", domain.ErrInvalidArgument, input.Name)
	}
	if _, err := s.domains.GetByName(ctx, name); err == nil {
		return nil, fmt.Errorf("%w: 域名 %s 已在台账中", domain.ErrConflict, name)
	} else if err != domain.ErrNotFound && err != nil {
		return nil, err
	}
	expected, err := normalizeExpectedStatus(input.ExpectedStatus)
	if err != nil {
		return nil, err
	}
	d := &domain.Domain{
		ID:             newID(),
		Name:           name,
		ExpectedStatus: expected,
		Source:         domain.DomainSourceManual,
		Note:           strings.TrimSpace(input.Note),
		Tags:           normalizeTags(input.Tags),
		CreatedAt:      s.clock.Now(),
		UpdatedAt:      s.clock.Now(),
	}
	if err := s.domains.Create(ctx, d); err != nil {
		if isUniqueViolationErr(err) {
			return nil, fmt.Errorf("%w: 域名 %s 已在台账中", domain.ErrConflict, name)
		}
		return nil, err
	}
	s.auditAction(ctx, actor, domain.AuditDomainCreate, d.Name, "expected="+string(d.ExpectedStatus))
	return d, nil
}

func (s *DomainService) Update(ctx context.Context, actor *domain.User, id string, input DomainInput) (*domain.Domain, error) {
	d, err := s.domains.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	name := domain.NormalizeDomain(input.Name)
	if !domain.ValidDomain(name) {
		return nil, fmt.Errorf("%w: 非法域名 %q（仅允许字母/数字/点/连字符，可带 *. 前缀，≤253 字符）", domain.ErrInvalidArgument, input.Name)
	}
	if name != d.Name {
		if _, err := s.domains.GetByName(ctx, name); err == nil {
			return nil, fmt.Errorf("%w: 域名 %s 已在台账中", domain.ErrConflict, name)
		} else if err != domain.ErrNotFound && err != nil {
			return nil, err
		}
	}
	expected, err := normalizeExpectedStatus(input.ExpectedStatus)
	if err != nil {
		return nil, err
	}
	d.Name = name
	d.ExpectedStatus = expected
	d.Note = strings.TrimSpace(input.Note)
	d.Tags = normalizeTags(input.Tags)
	d.UpdatedAt = s.clock.Now()
	if err := s.domains.Update(ctx, d); err != nil {
		if isUniqueViolationErr(err) {
			return nil, fmt.Errorf("%w: 域名 %s 已在台账中", domain.ErrConflict, name)
		}
		return nil, err
	}
	s.auditAction(ctx, actor, domain.AuditDomainUpdate, d.Name, "expected="+string(d.ExpectedStatus))
	return d, nil
}

func (s *DomainService) Delete(ctx context.Context, actor *domain.User, id string) error {
	d, err := s.domains.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.domains.Delete(ctx, id); err != nil {
		return err
	}
	s.auditAction(ctx, actor, domain.AuditDomainDelete, d.Name, "")
	return nil
}

// ResolveByName 按规范化域名反查台账项（CertificateService.Issue 落副本用）。
// 未登记返回 (nil, nil)——调用方据此留空 domain_id/domain_name。
func (s *DomainService) ResolveByName(ctx context.Context, name string) (*domain.Domain, error) {
	d, err := s.domains.GetByName(ctx, domain.NormalizeDomain(name))
	if err == domain.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// DomainLedgerItem 台账富化行：台账字段 + 实时计算的覆盖状态与当前证书。
// 直接作为 API 响应序列化（覆盖/证书为计算字段，不落库）。
type DomainLedgerItem struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	ExpectedStatus string              `json:"expected_status"`
	Source         string              `json:"source"`
	Note           string              `json:"note"`
	Tags           []string            `json:"tags"`
	Coverage       domain.Coverage     `json:"coverage"`
	DaysToExpire   *int                `json:"days_to_expire"`
	CertCount      int                 `json:"cert_count"`
	CurrentCert    *LedgerCertBrief    `json:"current_cert"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
}

// LedgerCertBrief 当前证书摘要
type LedgerCertBrief struct {
	ID            string `json:"id"`
	PrimaryDomain string `json:"primary_domain"`
	Status        string `json:"status"`
	Provider      string `json:"provider"`
	NotAfter      string `json:"not_after"`
}

// Ledger 台账富化列表：全量证书内存分组，逐域名计算覆盖状态。
// renewDays 为 renew_days_before（<=0 按 30 天）。
func (s *DomainService) Ledger(ctx context.Context, renewDays int) ([]*DomainLedgerItem, error) {
	domains, err := s.domains.List(ctx)
	if err != nil {
		return nil, err
	}
	certs, err := s.certs.List(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()

	// 关联分组：domain_id 精确关联 ∪ 域名/通配符匹配（覆盖历史证书与多 SAN）
	grouped := make(map[string][]*domain.Certificate, len(domains))
	for _, d := range domains {
		for _, c := range certs {
			if c.DomainID == d.ID || domain.MatchesDomain(c.Domains, d.Name) {
				grouped[d.ID] = append(grouped[d.ID], c)
			}
		}
	}

	out := make([]*DomainLedgerItem, 0, len(domains))
	for _, d := range domains {
		related := grouped[d.ID]
		item := &DomainLedgerItem{
			ID:             d.ID,
			Name:           d.Name,
			ExpectedStatus: string(d.ExpectedStatus),
			Source:         string(d.Source),
			Note:           d.Note,
			Tags:           d.Tags,
			Coverage:       domain.ComputeCoverage(related, d.ExpectedStatus, now, renewDays),
			CertCount:      len(related),
			CreatedAt:      d.CreatedAt.Format(time.RFC3339),
			UpdatedAt:      d.UpdatedAt.Format(time.RFC3339),
		}
		if item.Tags == nil {
			item.Tags = []string{}
		}
		if brief, notAfter := currentCertBrief(related); brief != nil {
			item.CurrentCert = brief
			days := domain.DaysToExpire(notAfter, now)
			item.DaysToExpire = &days
		}
		out = append(out, item)
	}
	return out, nil
}

// currentCertBrief 取"当前证书"：NotAfter 最大且非零者（与 ComputeCoverage 同规则）。
// 返回摘要与 NotAfter（供计算剩余天数）。
func currentCertBrief(certs []*domain.Certificate) (*LedgerCertBrief, time.Time) {
	var current *domain.Certificate
	for _, c := range certs {
		if c.NotAfter.IsZero() {
			continue
		}
		if current == nil || c.NotAfter.After(current.NotAfter) ||
			(c.NotAfter.Equal(current.NotAfter) && c.UpdatedAt.After(current.UpdatedAt)) {
			current = c
		}
	}
	if current == nil {
		return nil, time.Time{}
	}
	return &LedgerCertBrief{
		ID:            current.ID,
		PrimaryDomain: current.PrimaryDomain,
		Status:        string(current.Status),
		Provider:      current.Provider,
		NotAfter:      current.NotAfter.Format(time.RFC3339),
	}, current.NotAfter
}

func normalizeExpectedStatus(v string) (domain.ExpectedStatus, error) {
	e := domain.ExpectedStatus(strings.TrimSpace(v))
	if e == "" {
		return domain.ExpectedMustHaveCert, nil
	}
	if !e.Valid() {
		return "", fmt.Errorf("%w: 期望状态必须为 must_have_cert 或 monitor_only", domain.ErrInvalidArgument)
	}
	return e, nil
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func (s *DomainService) auditAction(ctx context.Context, actor *domain.User, action domain.AuditAction, resource, detail string) {
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

// isUniqueViolationErr 唯一约束冲突（域名查重兜底，防并发创建穿透）
func isUniqueViolationErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "constraint failed")
}
