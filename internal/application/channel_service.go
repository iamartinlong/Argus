package application

import (
	"context"
	"encoding/json"
	"strings"
	"fmt"

	"github.com/iamartinlong/Argus/internal/domain"
)

// ChannelService 通知渠道管理（界面配置，参数加密存储）
type ChannelService struct {
	channels domain.NotificationChannelStore
	rules    domain.NotificationRuleStore
	audit    domain.AuditStore
	builder  domain.NotifierBuilder
	cryptor  Cryptor
	clock    Clock
}

func NewChannelService(
	channels domain.NotificationChannelStore,
	rules domain.NotificationRuleStore,
	audit domain.AuditStore,
	builder domain.NotifierBuilder,
	cryptor Cryptor,
	clock Clock,
) *ChannelService {
	return &ChannelService{
		channels: channels, rules: rules, audit: audit,
		builder: builder, cryptor: cryptor, clock: clock,
	}
}

type ChannelInput struct {
	Name    string
	Type    domain.NotificationChannel
	Config  map[string]any
	Enabled *bool
}

func (s *ChannelService) Create(ctx context.Context, input ChannelInput) (*domain.NotificationChannelRecord, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: 渠道名称不能为空", domain.ErrInvalidArgument)
	}
	if !input.Type.Valid() {
		return nil, fmt.Errorf("%w: 无效渠道类型 %s", domain.ErrInvalidArgument, input.Type)
	}
	// 名称唯一（区分大小写不敏感，避免"告警"与"告警 "并存混淆）
	if exists, err := s.nameExists(ctx, input.Name, ""); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("%w: 渠道名称已存在", domain.ErrConflict)
	}
	// 参数校验：构建器能创建才允许保存
	if _, err := s.builder.Build(input.Type, input.Config); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidArgument, err)
	}
	plain, err := json.Marshal(input.Config)
	if err != nil {
		return nil, err
	}
	enc, err := s.cryptor.EncryptString(string(plain))
	if err != nil {
		return nil, err
	}
	channel := &domain.NotificationChannelRecord{
		ID:        newID(),
		Name:      input.Name,
		Type:      input.Type,
		ConfigEnc: enc,
		Enabled:   input.Enabled == nil || *input.Enabled,
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}
	if err := s.channels.Create(ctx, channel); err != nil {
		return nil, err
	}
	s.auditChannel(ctx, "channel.create", channel.Name)
	return channel, nil
}

func (s *ChannelService) List(ctx context.Context) ([]*domain.NotificationChannelRecord, error) {
	return s.channels.List(ctx)
}

// nameExists 同名渠道检查（大小写不敏感；excludeID 用于更新时排除自身）
func (s *ChannelService) nameExists(ctx context.Context, name, excludeID string) (bool, error) {
	all, err := s.channels.List(ctx)
	if err != nil {
		return false, err
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for _, c := range all {
		if c.ID == excludeID {
			continue
		}
		if strings.ToLower(strings.TrimSpace(c.Name)) == target {
			return true, nil
		}
	}
	return false, nil
}

func (s *ChannelService) Get(ctx context.Context, id string) (*domain.NotificationChannelRecord, error) {
	return s.channels.GetByID(ctx, id)
}

// sensitiveConfigKeys 不回传的敏感配置键（编辑时留空=保持原值）
var sensitiveConfigKeys = map[string]bool{
	"password": true, "secret": true, "key": true,
}

// GetConfig 解密渠道配置（供编辑回填；敏感键置空）
func (s *ChannelService) GetConfig(ctx context.Context, id string) (map[string]any, error) {
	channel, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	plain, err := s.cryptor.DecryptString(channel.ConfigEnc)
	if err != nil {
		return nil, fmt.Errorf("解密渠道配置失败: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	for k := range cfg {
		if sensitiveConfigKeys[k] {
			cfg[k] = ""
		}
	}
	return cfg, nil
}

func (s *ChannelService) Update(ctx context.Context, id string, input ChannelInput) (*domain.NotificationChannelRecord, error) {
	channel, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		// 名称唯一（排除自身）
		if exists, err := s.nameExists(ctx, input.Name, id); err != nil {
			return nil, err
		} else if exists {
			return nil, fmt.Errorf("%w: 渠道名称已存在", domain.ErrConflict)
		}
		channel.Name = input.Name
	}
	typeChanged := false
	if input.Type != "" {
		if !input.Type.Valid() {
			return nil, fmt.Errorf("%w: 无效渠道类型", domain.ErrInvalidArgument)
		}
		if input.Type != channel.Type {
			channel.Type = input.Type
			typeChanged = true
		}
	}
	// 新配置提供时：合并旧配置（空值保持原值，敏感键留空即保持）
	if input.Config != nil {
		merged := make(map[string]any, len(input.Config))
		for k, v := range input.Config {
			merged[k] = v
		}
		// 回填旧值：空字符串保持原值
		if plainOld, err := s.cryptor.DecryptString(channel.ConfigEnc); err == nil && plainOld != "" {
			var oldCfg map[string]any
			_ = json.Unmarshal([]byte(plainOld), &oldCfg)
			for k, v := range oldCfg {
				if cur, ok := merged[k]; !ok || cur == "" {
					merged[k] = v
				} else if s, ok := cur.(string); ok && s == "" {
					merged[k] = v
				}
			}
		}
		if _, err := s.builder.Build(channel.Type, merged); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrInvalidArgument, err)
		}
		plain, _ := json.Marshal(merged)
		enc, err := s.cryptor.EncryptString(string(plain))
		if err != nil {
			return nil, err
		}
		channel.ConfigEnc = enc
	} else if typeChanged {
		// 仅类型变更且未提供新配置：校验旧配置在新类型下仍可构建
		plain, err := s.cryptor.DecryptString(channel.ConfigEnc)
		if err != nil {
			return nil, fmt.Errorf("解密渠道配置失败: %w", err)
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
			return nil, err
		}
		if _, err := s.builder.Build(channel.Type, cfg); err != nil {
			return nil, fmt.Errorf("%w: 当前配置不适用于该类型: %v", domain.ErrInvalidArgument, err)
		}
	}
	if input.Enabled != nil {
		channel.Enabled = *input.Enabled
	}
	channel.UpdatedAt = s.clock.Now()
	if err := s.channels.Update(ctx, channel); err != nil {
		return nil, err
	}
	s.auditChannel(ctx, "channel.update", channel.Name)
	return channel, nil
}

func (s *ChannelService) Delete(ctx context.Context, id string) error {
	channel, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// 从引用该渠道的规则中移除
	rules, err := s.rules.List(ctx)
	if err == nil {
		for _, rule := range rules {
			ids := make([]string, 0, len(rule.ChannelIDs))
			changed := false
			for _, cid := range rule.ChannelIDs {
				if cid == id {
					changed = true
					continue
				}
				ids = append(ids, cid)
			}
			if changed {
				rule.ChannelIDs = ids
				_ = s.rules.Update(ctx, rule)
			}
		}
	}
	if err := s.channels.Delete(ctx, id); err != nil {
		return err
	}
	s.auditChannel(ctx, "channel.delete", channel.Name)
	return nil
}

// Test 发送测试消息
func (s *ChannelService) Test(ctx context.Context, id string) error {
	channel, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return err
	}
	sender, err := s.sender(ctx, channel)
	if err != nil {
		return err
	}
	return sender.Send(domain.NotificationMessage{
		Title: "Argus 测试通知",
		Body:  "这是一条来自 Argus 的测试消息，收到即表示渠道配置正常。",
	})
}

// sender 解密配置并构建发送器
func (s *ChannelService) sender(ctx context.Context, channel *domain.NotificationChannelRecord) (domain.MessageSender, error) {
	plain, err := s.cryptor.DecryptString(channel.ConfigEnc)
	if err != nil {
		return nil, fmt.Errorf("解密渠道配置失败: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	return s.builder.Build(channel.Type, cfg)
}

// SendToChannel 供 NotificationService 使用：按渠道 ID 构建发送器
func (s *ChannelService) SendToChannel(ctx context.Context, channelID string) (domain.MessageSender, error) {
	channel, err := s.channels.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if !channel.Enabled {
		return nil, fmt.Errorf("渠道 %s 已停用", channel.Name)
	}
	return s.sender(ctx, channel)
}

func (s *ChannelService) auditChannel(ctx context.Context, action, name string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    domain.AuditAction(action),
		Resource:  name,
		CreatedAt: s.clock.Now(),
	})
}

// SeedFromConfig 启动时种子导入：config.yaml 渠道 → DB（幂等，仅表空时）
func (s *ChannelService) SeedFromConfig(ctx context.Context, seed func() []ChannelInput) (int, error) {
	count, err := s.channels.Count(ctx)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	created := 0
	for _, input := range seed() {
		input.Name = string(input.Type) // 种子渠道名=类型名（旧规则回填依据）
		enabled := true
		input.Enabled = &enabled
		if _, err := s.Create(ctx, input); err != nil {
			return created, fmt.Errorf("种子渠道 %s 导入失败: %w", input.Type, err)
		}
		created++
	}
	return created, nil
}

// BackfillLegacyRuleChannelIDs 旧规则回填：channels（类型）→ channel_ids（种子渠道 ID）
func (s *ChannelService) BackfillLegacyRuleChannelIDs(ctx context.Context) (int, error) {
	rules, err := s.rules.List(ctx)
	if err != nil {
		return 0, err
	}
	all, err := s.channels.List(ctx)
	if err != nil {
		return 0, err
	}
	byType := map[string]*domain.NotificationChannelRecord{}
	for _, c := range all {
		byType[string(c.Type)] = c
	}
	updated := 0
	for _, rule := range rules {
		if len(rule.ChannelIDs) > 0 || len(rule.Channels) == 0 {
			continue
		}
		var ids []string
		for _, t := range rule.Channels {
			if c, ok := byType[string(t)]; ok {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			rule.ChannelIDs = ids
			if err := s.rules.Update(ctx, rule); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}

