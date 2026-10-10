package application

import (
	"errors"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeCryptor 可逆 base64 测试加密器，见 certificate_service_test.go

func newChannelTestService(builder domain.NotifierBuilder) (*ChannelService, *fakeChannelStore, *fakeRuleStore, *fakeAuditStore) {
	channels := newFakeChannelStore()
	rules := newFakeRuleStore()
	audit := &fakeAuditStore{}
	svc := NewChannelService(channels, rules, audit, builder, fakeCryptor{}, fixedClock{time.Now()})
	return svc, channels, rules, audit
}

// failBuilder 模拟参数不合法（如空 URL）
type failBuilder struct{}

func (failBuilder) Build(_ domain.NotificationChannel, _ map[string]any) (domain.MessageSender, error) {
	return nil, errTestFail
}

type okBuilder struct{}

// Build 按类型校验：webhook 需 url，smtp 需 host；其余类型仅返回发送器
func (okBuilder) Build(channelType domain.NotificationChannel, cfg map[string]any) (domain.MessageSender, error) {
	if channelType == domain.ChannelSMTP {
		host, _ := cfg["host"].(string)
		if host == "" {
			return nil, errTestFail
		}
	} else {
		url, _ := cfg["url"].(string)
		if url == "" {
			return nil, errTestFail
		}
	}
	return &fakeNotifier{}, nil
}

func TestChannelCreateValidatesConfig(t *testing.T) {
	svc, _, _, _ := newChannelTestService(failBuilder{})
	_, err := svc.Create(context.Background(), ChannelInput{
		Name: "test", Type: domain.ChannelWebhook, Config: map[string]any{"url": ""},
	})
	if err == nil {
		t.Fatal("expected config validation error")
	}
}

func TestChannelCreateEncryptsConfig(t *testing.T) {
	svc, store, _, _ := newChannelTestService(okBuilder{})
	channel, err := svc.Create(context.Background(), ChannelInput{
		Name: "test", Type: domain.ChannelWebhook, Config: map[string]any{"url": "http://x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := store.channels[channel.ID]
	if strings.HasPrefix(strings.TrimSpace(stored.ConfigEnc), "enc:") {
		t.Fatalf("expected encrypted config, got %q", stored.ConfigEnc)
	}
	if stored.ConfigEnc == `{"url":"http://x"}` {
		t.Fatalf("config stored in plaintext: %q", stored.ConfigEnc)
	}
}

func TestChannelDeleteCleansRuleReferences(t *testing.T) {
	svc, _, rules, _ := newChannelTestService(okBuilder{})
	id := seedChannel(t, svc, "webhook", domain.ChannelWebhook)
	rule := &domain.NotificationRule{
		ID: "r1", Name: "r", Event: domain.EventCertExpiring,
		ChannelIDs: []string{id, "other"}, Enabled: true,
	}
	if err := rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, _ := rules.GetByID(context.Background(), "r1")
	if len(got.ChannelIDs) != 1 || got.ChannelIDs[0] != "other" {
		t.Fatalf("expected channel ref removed, got %v", got.ChannelIDs)
	}
}

func TestChannelSendToChannelDisabled(t *testing.T) {
	svc, _, _, _ := newChannelTestService(okBuilder{})
	channel, err := svc.Create(context.Background(), ChannelInput{
		Name: "off", Type: domain.ChannelWebhook, Config: map[string]any{"url": "http://x"}, Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendToChannel(context.Background(), channel.ID); err == nil {
		t.Fatal("expected disabled channel error")
	}
}

func TestChannelTestSendsMessage(t *testing.T) {
	notifier := &fakeNotifier{}
	builder := &fakeNotifierBuilder{notifier: notifier}
	svc, _, _, _ := newChannelTestService(builder)
	id := seedChannel(t, svc, "webhook", domain.ChannelWebhook)
	if err := svc.Test(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(notifier.sent))
	}
}

func TestChannelUpdateKeepsEnabledWhenOmitted(t *testing.T) {
	svc, _, _, _ := newChannelTestService(okBuilder{})
	disabled := boolPtr(false)
	id := seedChannel(t, svc, "webhook", domain.ChannelWebhook)
	_, _ = svc.Update(context.Background(), id, ChannelInput{Enabled: disabled})
	// 不传 enabled：保持停用状态
	updated, err := svc.Update(context.Background(), id, ChannelInput{Name: "改名"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("expected enabled unchanged (disabled)")
	}
	if updated.Name != "改名" {
		t.Fatalf("expected renamed, got %q", updated.Name)
	}
}

func TestChannelUpdateTypeChangeValidatesOldConfig(t *testing.T) {
	svc, _, _, _ := newChannelTestService(okBuilder{})
	id := seedChannel(t, svc, "webhook", domain.ChannelWebhook)
	// 类型改为 smtp，但旧配置（url）不适用于 smtp → 拒绝
	_, err := svc.Update(context.Background(), id, ChannelInput{Type: domain.ChannelSMTP})
	if err == nil {
		t.Fatal("expected validation error when old config incompatible with new type")
	}
}

func TestCreateRuleRejectsInvalidEvent(t *testing.T) {
	svc, _, _, _ := newTestNotificationService()
	_, err := svc.CreateRule(context.Background(), NotificationRuleInput{
		Name: "bad", Event: "no_such_event", ChannelIDs: []string{testWebhookChannelID},
	})
	if err == nil {
		t.Fatal("expected invalid event rejected")
	}
}

func TestChannelNameUnique(t *testing.T) {
	svc, _, _, _ := newChannelTestService(okBuilder{})
	ctx := context.Background()

	seedChannel(t, svc, "告警渠道", domain.ChannelWebhook)

	// 同名创建 → 冲突（大小写不敏感）
	_, err := svc.Create(ctx, ChannelInput{
		Name: "告警渠道", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://x"},
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict for duplicate name, got %v", err)
	}
	_, err = svc.Create(ctx, ChannelInput{
		Name: " 告警渠道 ", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://x"},
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict for whitespace-variant name, got %v", err)
	}

	// 不同名 → 成功
	other, err := svc.Create(ctx, ChannelInput{
		Name: "另一渠道", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://x"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 更新为他人同名 → 冲突
	_, err = svc.Update(ctx, other.ID, ChannelInput{Name: "告警渠道"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict on update, got %v", err)
	}
	// 更新保持自身名称 → 允许
	if _, err := svc.Update(ctx, other.ID, ChannelInput{Name: "另一渠道"}); err != nil {
		t.Fatalf("self name update rejected: %v", err)
	}
}

func TestSeedFromConfigIdempotent(t *testing.T) {
	svc, store, _, _ := newChannelTestService(okBuilder{})
	seed := func() []ChannelInput {
		return []ChannelInput{
			{Type: domain.ChannelWebhook, Config: map[string]any{"url": "http://x"}},
			{Type: domain.ChannelSMTP, Config: map[string]any{"host": "smtp.example.com"}},
		}
	}
	created, err := svc.SeedFromConfig(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("expected 2 seeded channels, got %d", created)
	}
	// 幂等：再次调用不新增
	created2, err := svc.SeedFromConfig(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if created2 != 0 {
		t.Fatalf("expected 0 on second seed, got %d", created2)
	}
	if len(store.channels) != 2 {
		t.Fatalf("expected 2 channels total, got %d", len(store.channels))
	}
}

func TestSeedFromConfigDecryptsToConfig(t *testing.T) {
	svc, _, _, _ := newChannelTestService(okBuilder{})
	seed := func() []ChannelInput {
		return []ChannelInput{{Type: domain.ChannelWebhook, Config: map[string]any{"url": "http://x"}}}
	}
	if _, err := svc.SeedFromConfig(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	channels, _ := svc.List(context.Background())
	if len(channels) != 1 {
		t.Fatal("expected 1 channel")
	}
	plain, err := fakeCryptor{}.DecryptString(channels[0].ConfigEnc)
	if err != nil {
		t.Fatalf("decrypt config failed: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["url"] != "http://x" {
		t.Fatalf("unexpected config %v", cfg)
	}
}

func TestBackfillLegacyRuleChannelIDs(t *testing.T) {
	svc, _, rules, _ := newChannelTestService(okBuilder{})
	// 种子：webhook + smtp 渠道
	svc.SeedFromConfig(context.Background(), func() []ChannelInput {
		return []ChannelInput{
			{Type: domain.ChannelWebhook, Config: map[string]any{"url": "http://x"}},
			{Type: domain.ChannelSMTP, Config: map[string]any{"host": "smtp.example.com"}},
		}
	})
	channels, _ := svc.List(context.Background())
	byType := map[string]string{}
	for _, c := range channels {
		byType[string(c.Type)] = c.ID
	}
	// 旧规则：channels=webhook/smtp 类型，无 channel_ids
	rule := &domain.NotificationRule{
		ID: "legacy", Name: "legacy", Event: domain.EventCertExpiring,
		Channels: []domain.NotificationChannel{domain.ChannelWebhook, domain.ChannelSMTP},
		Enabled:  true,
	}
	if err := rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.BackfillLegacyRuleChannelIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("expected 1 rule backfilled, got %d", updated)
	}
	got, _ := rules.GetByID(context.Background(), "legacy")
	if len(got.ChannelIDs) != 2 ||
		got.ChannelIDs[0] != byType["webhook"] || got.ChannelIDs[1] != byType["smtp"] {
		t.Fatalf("unexpected channel ids %v", got.ChannelIDs)
	}
}
