package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeChannelStore 内存通知渠道存储
type fakeChannelStore struct {
	mu       sync.Mutex
	channels map[string]*domain.NotificationChannelRecord
	order    []string
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{channels: map[string]*domain.NotificationChannelRecord{}}
}

func (f *fakeChannelStore) Create(_ context.Context, c *domain.NotificationChannelRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels[c.ID] = c
	f.order = append(f.order, c.ID)
	return nil
}

func (f *fakeChannelStore) GetByID(_ context.Context, id string) (*domain.NotificationChannelRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.channels[id]; ok {
		return c, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeChannelStore) List(context.Context) ([]*domain.NotificationChannelRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.NotificationChannelRecord
	for _, id := range f.order {
		out = append(out, f.channels[id])
	}
	return out, nil
}

func (f *fakeChannelStore) Update(_ context.Context, c *domain.NotificationChannelRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels[c.ID] = c
	return nil
}

func (f *fakeChannelStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.channels, id)
	return nil
}

func (f *fakeChannelStore) Count(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.channels)), nil
}

// fakeNotifierBuilder 固定返回给定 notifier（按类型构建记录）
type fakeNotifierBuilder struct {
	notifier domain.MessageSender
}

func (b *fakeNotifierBuilder) Build(channelType domain.NotificationChannel, config map[string]any) (domain.MessageSender, error) {
	if b.notifier != nil {
		return b.notifier, nil
	}
	return nil, errTestFail
}

// fakeSendLogStore 内存发送历史
type fakeSendLogStore struct {
	mu   sync.Mutex
	logs []*domain.NotificationSendLog
}

func newFakeSendLogStore() *fakeSendLogStore { return &fakeSendLogStore{} }

func (f *fakeSendLogStore) Append(_ context.Context, l *domain.NotificationSendLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
	return nil
}

func (f *fakeSendLogStore) Count(context.Context, domain.NotificationSendLogQuery) (int64, error) {
	return int64(len(f.logs)), nil
}

func (f *fakeSendLogStore) List(context.Context, domain.NotificationSendLogQuery, int, int) ([]*domain.NotificationSendLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs, nil
}

func (f *fakeSendLogStore) CountByStatus(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeSendLogStore) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	out := f.logs[:0]
	for _, l := range f.logs {
		if l.SentAt.Before(time.Now().Add(-365 * 24 * time.Hour)) {
			n++
			continue
		}
		out = append(out, l)
	}
	f.logs = out
	return int64(n), nil
}

// seedChannel 创建渠道并返回 ID
func seedChannel(t *testing.T, svc *ChannelService, name string, channelType domain.NotificationChannel) string {
	t.Helper()
	channel, err := svc.Create(context.Background(), ChannelInput{
		Name:    name,
		Type:    channelType,
		Config:  map[string]any{"url": "http://example.com/hook"},
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	return channel.ID
}

func boolPtr(b bool) *bool { return &b }

