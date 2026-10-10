package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// ErrScanInProgress 上一轮扫描仍在执行（cron/手动触发并发时跳过本轮）
var ErrScanInProgress = errors.New("扫描正在进行中，请稍后重试")

// MaxExpiryReminderDays 最长到期提醒提前期（默认 expiryDays 上限）。
// 通知防重日志的保留期不得短于此值，否则提醒会被重复发送。
const MaxExpiryReminderDays = 30

// CertificateOps 调度器依赖的证书操作（由 CertificateService 实现）
type CertificateOps interface {
	List(ctx context.Context) ([]*domain.Certificate, error)
	Renew(ctx context.Context, certID string, dnsConfig map[string]string, dnsProviderAccountID string) (*domain.Certificate, error)
	// UpdateStatus 持久化过渡状态（valid→expiring→expired），保证基于 status 列的统计口径
	UpdateStatus(ctx context.Context, certID string, status domain.CertificateStatus) error
}

// TaskRunner 任务日志记录（infrastructure/scheduler 实现）
type TaskRunner interface {
	RunTask(ctx context.Context, typ domain.TaskType, fn func(ctx context.Context) error) error
	RunTaskWithSummary(ctx context.Context, typ domain.TaskType, fn func(ctx context.Context) (string, error)) error
}

type SchedulerService struct {
	certs      CertificateOps
	notify     *NotificationService
	tasks      TaskRunner
	clock      Clock
	renewDays  int
	expiryDays []int // 提醒提前天数
	scanMu     sync.Mutex
}

func NewSchedulerService(
	certs CertificateOps,
	notify *NotificationService,
	tasks TaskRunner,
	clock Clock,
	renewDays int,
	expiryDays []int,
) *SchedulerService {
	if renewDays <= 0 {
		renewDays = 30
	}
	if len(expiryDays) == 0 {
		expiryDays = []int{30, 14, 7, 1}
	}
	return &SchedulerService{certs: certs, notify: notify, tasks: tasks, clock: clock, renewDays: renewDays, expiryDays: expiryDays}
}

// RunScan 执行一轮扫描：自动续期 + 到期提醒（供 cron 与手动触发调用）。
// 单飞互斥：cron / 手动触发 / 并发请求同时到达时只有一轮在执行，其余立即返回 ErrScanInProgress。
// 返回结构化摘要 JSON（存入 task_logs.summary），供任务日志详情展示
func (s *SchedulerService) RunScan(ctx context.Context) (string, error) {
	if !s.scanMu.TryLock() {
		return "", ErrScanInProgress
	}
	defer s.scanMu.Unlock()
	start := s.clock.Now()
	certs, err := s.certs.List(ctx)
	if err != nil {
		return "", fmt.Errorf("扫描证书失败: %w", err)
	}
	now := s.clock.Now()
	summary := domain.ScanSummary{
		Scanned: len(certs),
	}
	for _, cert := range certs {
		prev := cert.Status
		cert.UpdateStatus(now)
		// 过渡状态落库（valid→expiring→expired）：UpdateStatus 只改内存，
		// 不落库则 CountExpired 等基于 status 列的统计恒为 0
		if cert.Status != prev {
			switch prev {
			case domain.StatusValid, domain.StatusExpiring, domain.StatusExpired:
				_ = s.certs.UpdateStatus(ctx, cert.ID, cert.Status)
			}
		}
		// 到期识别
		if cert.Status == domain.StatusExpiring || cert.Status == domain.StatusExpired {
			daysLeft := int(math.Ceil(cert.NotAfter.Sub(now).Hours() / 24))
			if daysLeft < 0 {
				daysLeft = 0
			}
			summary.Expiring = append(summary.Expiring, domain.ExpiringInfo{
				Domain:   cert.PrimaryDomain,
				DaysLeft: daysLeft,
				Status:   string(cert.Status),
				Provider: cert.Provider,
			})
		}
		// 自动续期：有效/即将到期且到期 < renewDays 天
		if (cert.Status == domain.StatusValid || cert.Status == domain.StatusExpiring) &&
			!cert.NotAfter.IsZero() && now.Add(time.Duration(s.renewDays)*24*time.Hour).After(cert.NotAfter) {
			if cert.Provider == "import" {
				summary.RenewSkipped = append(summary.RenewSkipped, domain.SkippedInfo{Domain: cert.PrimaryDomain, Reason: "import"})
				continue
			}
			triggered, success, failReason := s.renewOneWithResult(ctx, cert)
			if triggered {
				summary.RenewTriggered++
				if success {
					summary.RenewSuccess = append(summary.RenewSuccess, cert.PrimaryDomain)
				} else if failReason == "csr" {
					summary.RenewSkipped = append(summary.RenewSkipped, domain.SkippedInfo{Domain: cert.PrimaryDomain, Reason: "csr"})
				} else if failReason != "" {
					summary.RenewFailed = append(summary.RenewFailed, domain.RenewFail{Domain: cert.PrimaryDomain, Error: failReason})
				}
			}
			continue
		}
		if sent := s.notifyExpiryIfDueWithResult(ctx, cert, now); sent {
			summary.NotifySent++
		}
	}
	summary.DurationMs = s.clock.Now().Sub(start).Milliseconds()
	return marshalSummary(summary), nil
}

func marshalSummary(s domain.ScanSummary) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	if len(b) > 8192 {
		// 截断保护：超 8KB 截断并标记
		trunc := domain.ScanSummary{
			Scanned:        s.Scanned,
			RenewTriggered: s.RenewTriggered,
			NotifySent:     s.NotifySent,
			DurationMs:     s.DurationMs,
		}
		if len(s.Expiring) > 0 {
			trunc.Expiring = s.Expiring[:1]
		}
		if len(s.RenewFailed) > 0 {
			trunc.RenewFailed = s.RenewFailed[:1]
		}
		b, _ = json.Marshal(trunc)
	}
	return string(b)
}

// renewOneWithResult 单证书续期（记录独立任务日志，失败/成功均通知），返回是否触发与结果
func (s *SchedulerService) renewOneWithResult(ctx context.Context, cert *domain.Certificate) (triggered bool, success bool, failReason string) {
	if cert.Provider == "import" {
		return false, false, "import"
	}
	triggered = true
	var rErr error
	_ = s.tasks.RunTask(ctx, domain.TaskAutoRenew, func(ctx context.Context) error {
		ctx2, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		renewed, err := s.certs.Renew(ctx2, cert.ID, nil, "")
		if err != nil {
			if errors.Is(err, ErrCSRCertNotRenewable) {
				rErr = ErrCSRCertNotRenewable
				return nil
			}
			cert.LastError = err.Error()
			s.notify.NotifyEvent(ctx, domain.EventRenewFailed, cert, map[string]string{"error": err.Error()})
			rErr = err
			return err
		}
		s.notify.NotifyEvent(ctx, domain.EventRenewSuccess, renewed, nil)
		return nil
	})
	if rErr != nil {
		if errors.Is(rErr, ErrCSRCertNotRenewable) {
			return true, false, "csr"
		}
		return true, false, rErr.Error()
	}
	// 成功：若 Renew 未报错，视为 success（即使 CSR 静默跳过已在上层处理）
	return true, true, ""
}

func (s *SchedulerService) notifyExpiryIfDueWithResult(ctx context.Context, cert *domain.Certificate, now time.Time) bool {
	if cert.Status == domain.StatusRevoked || cert.Status == domain.StatusExpired {
		return false
	}
	if cert.NotAfter.IsZero() {
		return false
	}
	daysLeft := int(math.Ceil(cert.NotAfter.Sub(now).Hours() / 24))
	if daysLeft < 0 {
		daysLeft = 0
	}
	// 取"最紧"的适用提醒（daysLeft <= d 中最小的 d）：服务停机跨过某个提醒日时
	// 下一轮仍会补发；同一提醒的重复发送由防重日志（cert+rule+daysBefore）拦截
	best := 0
	for _, d := range s.expiryDays {
		if daysLeft <= d && (best == 0 || d < best) {
			best = d
		}
	}
	if best == 0 {
		return false
	}
	s.notify.NotifyExpiry(ctx, cert, best)
	return true
}
