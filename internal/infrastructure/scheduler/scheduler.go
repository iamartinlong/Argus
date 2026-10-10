package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

// Runner 定时任务执行器：记录任务日志 + cron 调度
type Runner struct {
	cron    *cron.Cron
	tasks   domain.TaskLogStore
	clock   application.Clock
}

func NewRunner(tasks domain.TaskLogStore, clock application.Clock) *Runner {
	return &Runner{
		// Recover 链：任务 panic 不崩溃整个进程
		cron:  cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		tasks: tasks,
		clock: clock,
	}
}

// Start 启动定时调度（scanMinutes 间隔扫描）
func (r *Runner) Start(scanMinutes int, fn func(ctx context.Context) error) {
	if scanMinutes <= 0 {
		scanMinutes = 60
	}
	spec := fmt.Sprintf("@every %dm", scanMinutes)
	_, _ = r.cron.AddFunc(spec, func() {
		ctx := context.Background()
		_ = r.RunTask(ctx, domain.TaskScanCertificates, fn)
	})
	r.cron.Start()
}

// StartWithSummary 启动带摘要的扫描调度（任务日志写入 summary JSON）
func (r *Runner) StartWithSummary(scanMinutes int, fn func(ctx context.Context) (string, error)) {
	if scanMinutes <= 0 {
		scanMinutes = 60
	}
	spec := fmt.Sprintf("@every %dm", scanMinutes)
	_, _ = r.cron.AddFunc(spec, func() {
		ctx := context.Background()
		_ = r.RunTaskWithSummary(ctx, domain.TaskScanCertificates, fn)
	})
	r.cron.Start()
}

func (r *Runner) Stop() {
	r.cron.Stop()
}

// RunTask 执行任务并记录日志（panic 恢复：记录失败日志而非崩溃进程）
func (r *Runner) RunTask(ctx context.Context, typ domain.TaskType, fn func(ctx context.Context) error) (err error) {
	entry := &domain.TaskLog{
		ID:        newTaskID(),
		Type:      typ,
		Status:    domain.TaskRunning,
		StartedAt: r.clock.Now(),
	}
	if cerr := r.tasks.Create(ctx, entry); cerr != nil {
		log.Printf("记录任务日志失败: %v", cerr)
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("任务 %s panic: %v", typ, p)
			log.Printf("任务 %s panic（已恢复）: %v", typ, p)
		}
		now := r.clock.Now()
		if err != nil {
			entry.Status = domain.TaskFailed
			entry.Detail = err.Error()
		} else {
			entry.Status = domain.TaskSuccess
		}
		entry.FinishedAt = &now
		if uerr := r.tasks.Update(ctx, entry); uerr != nil {
			log.Printf("更新任务日志失败: %v", uerr)
		}
	}()
	err = fn(ctx)
	return err
}

// RunTaskWithSummary 执行任务并记录结构化摘要（summary JSON 存入 task_logs.summary）
func (r *Runner) RunTaskWithSummary(ctx context.Context, typ domain.TaskType, fn func(ctx context.Context) (string, error)) (err error) {
	entry := &domain.TaskLog{
		ID:        newTaskID(),
		Type:      typ,
		Status:    domain.TaskRunning,
		StartedAt: r.clock.Now(),
	}
	if cerr := r.tasks.Create(ctx, entry); cerr != nil {
		log.Printf("记录任务日志失败: %v", cerr)
	}
	var summary string
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("任务 %s panic: %v", typ, p)
			log.Printf("任务 %s panic（已恢复）: %v", typ, p)
		}
		now := r.clock.Now()
		if err != nil {
			entry.Status = domain.TaskFailed
			entry.Detail = err.Error()
			entry.Summary = summary
		} else {
			entry.Status = domain.TaskSuccess
			entry.Summary = summary
		}
		entry.FinishedAt = &now
		if uerr := r.tasks.Update(ctx, entry); uerr != nil {
			log.Printf("更新任务日志失败: %v", uerr)
		}
	}()
	summary, err = fn(ctx)
	return err
}

func newTaskID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
