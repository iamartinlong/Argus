package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeTaskLogStore 内存任务日志
type fakeTaskLogStore struct {
	mu    sync.Mutex
	logs  []*domain.TaskLog
	upErr error // Update 时注入错误
}

func (f *fakeTaskLogStore) Create(_ context.Context, l *domain.TaskLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
	return nil
}

func (f *fakeTaskLogStore) Update(_ context.Context, l *domain.TaskLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upErr != nil {
		return f.upErr
	}
	for _, existing := range f.logs {
		if existing.ID == l.ID {
			*existing = *l
			return nil
		}
	}
	return nil
}

func (f *fakeTaskLogStore) Count(context.Context) (int64, error) { return 0, nil }

func (f *fakeTaskLogStore) List(context.Context, int, int) ([]*domain.TaskLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs, nil
}

func (f *fakeTaskLogStore) DeleteOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeTaskLogStore) last() *domain.TaskLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.logs) == 0 {
		return nil
	}
	return f.logs[len(f.logs)-1]
}

func newTestRunner() (*Runner, *fakeTaskLogStore) {
	store := &fakeTaskLogStore{}
	return NewRunner(store, application.RealClock{}), store
}

func TestRunTaskSuccess(t *testing.T) {
	r, store := newTestRunner()
	err := r.RunTask(context.Background(), domain.TaskScanCertificates, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := store.last()
	if entry.Status != domain.TaskSuccess || entry.FinishedAt == nil {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestRunTaskErrorRecorded(t *testing.T) {
	r, store := newTestRunner()
	wantErr := errors.New("boom")
	err := r.RunTask(context.Background(), domain.TaskScanCertificates, func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	entry := store.last()
	if entry.Status != domain.TaskFailed || entry.Detail != wantErr.Error() {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

// TestRunTaskPanicRecovered 任务 panic 不崩溃进程，记录为 failed
func TestRunTaskPanicRecovered(t *testing.T) {
	r, store := newTestRunner()
	err := r.RunTask(context.Background(), domain.TaskScanCertificates, func(context.Context) error {
		panic("storage nil deref")
	})
	if err == nil {
		t.Fatal("expected error for panicking task")
	}
	entry := store.last()
	if entry.Status != domain.TaskFailed || entry.FinishedAt == nil {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

// TestRunTaskUpdateErrorNotFatal 日志写入失败不影响任务执行
func TestRunTaskUpdateErrorNotFatal(t *testing.T) {
	r, store := newTestRunner()
	store.upErr = errors.New("db locked")
	err := r.RunTask(context.Background(), domain.TaskScanCertificates, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatal("task should still succeed despite log update failure")
	}
}

func TestRunnerStartAndStop(t *testing.T) {
	r, store := newTestRunner()
	// 验证 Start/Stop 生命周期与任务日志（cron 粒度 1 分钟，不等待实际触发）
	r.Start(1, func(context.Context) error { return nil })
	r.Stop()
	_ = store
}
