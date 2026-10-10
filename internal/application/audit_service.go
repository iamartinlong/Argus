package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate id: %v", err))
	}
	return hex.EncodeToString(b)
}

type AuditService struct {
	audit domain.AuditStore
}

func NewAuditService(audit domain.AuditStore) *AuditService {
	return &AuditService{audit: audit}
}

func (s *AuditService) List(ctx context.Context, filter domain.AuditQuery, limit, offset int) ([]*domain.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.audit.List(ctx, filter, limit, offset)
}

// Count 带筛选条件总数（分页元信息）
func (s *AuditService) Count(ctx context.Context, filter domain.AuditQuery) (int64, error) {
	return s.audit.Count(ctx, filter)
}

// Append 实现 domain.AuditStore（供 handler 层记录敏感操作审计）
func (s *AuditService) Append(ctx context.Context, log *domain.AuditLog) error {
	return s.audit.Append(ctx, log)
}

// DeleteOlderThan 保留策略：删除早于指定时间的审计日志
func (s *AuditService) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return s.audit.DeleteOlderThan(ctx, before)
}
