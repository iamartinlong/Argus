package notify

import (
	"fmt"

	"encoding/json"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
)

// Builder 从渠道配置构建 Notifier（界面配置的渠道经此动态创建）
type Builder struct{}

func NewBuilder() *Builder { return &Builder{} }

// Build 按渠道类型与参数构建 MessageSender
func (Builder) Build(channelType domain.NotificationChannel, cfg map[string]any) (domain.MessageSender, error) {
	switch channelType {
	case domain.ChannelWebhook:
		var c config.WebhookConfig
		if err := mapToStruct(cfg, &c); err != nil {
			return nil, err
		}
		if c.URL == "" {
			return nil, fmt.Errorf("webhook url 不能为空")
		}
		return NewWebhook(c.URL, c.Secret), nil
	case domain.ChannelSMTP:
		var c config.SMTPConfig
		if err := mapToStruct(cfg, &c); err != nil {
			return nil, err
		}
		if c.Host == "" {
			return nil, fmt.Errorf("smtp host 不能为空")
		}
		return NewSMTP(c), nil
	case domain.ChannelWeCom:
		var c config.IMWebhook
		if err := mapToStruct(cfg, &c); err != nil {
			return nil, err
		}
		if c.WebhookURL == "" {
			return nil, fmt.Errorf("企业微信 webhook url 不能为空")
		}
		return NewWeCom(c.WebhookURL, c.Key), nil
	case domain.ChannelDingTalk:
		var c config.IMWebhook
		if err := mapToStruct(cfg, &c); err != nil {
			return nil, err
		}
		if c.WebhookURL == "" {
			return nil, fmt.Errorf("钉钉 webhook url 不能为空")
		}
		return NewDingTalk(c.WebhookURL, c.Key), nil
	case domain.ChannelFeishu:
		var c config.IMWebhook
		if err := mapToStruct(cfg, &c); err != nil {
			return nil, err
		}
		if c.WebhookURL == "" {
			return nil, fmt.Errorf("飞书 webhook url 不能为空")
		}
		return NewFeishu(c.WebhookURL), nil
	default:
		return nil, fmt.Errorf("未知通知渠道类型: %s", channelType)
	}
}

func mapToStruct(m map[string]any, out any) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
