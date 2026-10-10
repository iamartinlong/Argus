package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

type NotificationService struct {
	rules        domain.NotificationRuleStore
	logs         domain.NotificationLogStore
	sendLogs     domain.NotificationSendLogStore
	templates    domain.NotificationTemplateStore
	audit        domain.AuditStore
	channels     *ChannelService
	clock        Clock
	sleepBetween func(time.Duration)
}

func NewNotificationService(
	rules domain.NotificationRuleStore,
	logs domain.NotificationLogStore,
	sendLogs domain.NotificationSendLogStore,
	templates domain.NotificationTemplateStore,
	audit domain.AuditStore,
	channels *ChannelService,
	clock Clock,
) *NotificationService {
	return &NotificationService{
		rules: rules, logs: logs, sendLogs: sendLogs, templates: templates,
		audit: audit, channels: channels, clock: clock,
		sleepBetween: time.Sleep,
	}
}

// AttachEventBus 订阅证书/部署事件并触发事件通知（与 DeployService 订阅模式一致）
func (s *NotificationService) AttachEventBus(bus *EventBus) {
	if bus == nil {
		return
	}
	// 事件型通知异步发送：订阅回调运行在签发/续期请求的 Publish 调用栈内，
	// 同步发送（含重试退避）会拖慢 API 响应，故放后台并设总超时
	notifyAsync := func(event domain.NotificationEvent, payload any, extra func(*domain.Certificate) map[string]string) {
		if cert, ok := payload.(*domain.Certificate); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				s.NotifyEvent(ctx, event, cert, extra(cert))
			}()
		}
	}
	bus.Subscribe(EventCertificateIssued, func(e Event) {
		notifyAsync(domain.EventIssueSuccess, e.Payload, func(*domain.Certificate) map[string]string { return nil })
	})
	bus.Subscribe(EventCertificateIssueFail, func(e Event) {
		notifyAsync(domain.EventIssueFailed, e.Payload, func(c *domain.Certificate) map[string]string {
			return map[string]string{"error": c.LastError}
		})
	})
	bus.Subscribe(EventDeploySuccess, func(e Event) {
		if ev, ok := e.Payload.(*DeployEvent); ok {
			notifyAsync(domain.EventDeploySuccess, ev.Cert, func(*domain.Certificate) map[string]string {
				return map[string]string{"target": ev.TargetName}
			})
		}
	})
	bus.Subscribe(EventDeployFailed, func(e Event) {
		if ev, ok := e.Payload.(*DeployEvent); ok {
			notifyAsync(domain.EventDeployFailed, ev.Cert, func(*domain.Certificate) map[string]string {
				return map[string]string{"target": ev.TargetName, "error": ev.Error}
			})
		}
	})
}

// NotifyEvent 触发事件通知：匹配启用规则，逐渠道发送，记录防重日志
func (s *NotificationService) NotifyEvent(ctx context.Context, event domain.NotificationEvent, cert *domain.Certificate, extra map[string]string) {
	if !s.eventSubscribed(cert, event) {
		return
	}
	rules, err := s.rules.ListByEvent(ctx, event)
	if err != nil || len(rules) == 0 {
		return
	}
	details := map[string]string{
		"domain":  cert.PrimaryDomain,
		"status":  string(cert.Status),
	}
	for k, v := range extra {
		details[k] = v
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		title, body := s.render(ctx, event, cert, details)
		for _, chID := range rule.ChannelIDs {
			// daysBefore=-1：事件型通知不写防重日志（每次触发都发送）
			s.sendWithRetry(ctx, event, chID, rule.ID, cert.ID, -1, title, body, details)
		}
	}
}

// NotifyExpiry 到期提醒（提前 N 天，防重复）
func (s *NotificationService) NotifyExpiry(ctx context.Context, cert *domain.Certificate, daysBefore int) {
	if !s.eventSubscribed(cert, domain.EventCertExpiring) {
		return
	}
	rules, err := s.rules.ListByEvent(ctx, domain.EventCertExpiring)
	if err != nil || len(rules) == 0 {
		return
	}
	expiry := cert.NotAfter.Format("2006-01-02 15:04")
	details := map[string]string{
		"domain":    cert.PrimaryDomain,
		"expires":   expiry,
		"days_left": fmt.Sprintf("%d", daysBefore),
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		for _, d := range rule.DaysBefore {
			if d != daysBefore {
				continue
			}
			// 防重复：同证书同规则同提前天数只发一次
			sent, err := s.logs.HasSent(ctx, cert.ID, rule.ID, daysBefore)
			if err != nil || sent {
				continue
			}
			details["expires"] = expiry
			details["days_left"] = fmt.Sprintf("%d", daysBefore)
			title, body := s.render(ctx, domain.EventCertExpiring, cert, details)
			for _, chID := range rule.ChannelIDs {
				s.sendWithRetry(ctx, domain.EventCertExpiring, chID, rule.ID, cert.ID, daysBefore, title, body, details)
			}
		}
	}
}

// eventSubscribed 证书是否订阅了该事件。
// 旧证书 notify_events 为空表示未配置：保持现状全部通知（向后兼容）
func (s *NotificationService) eventSubscribed(cert *domain.Certificate, event domain.NotificationEvent) bool {
	if cert == nil || len(cert.NotifyEvents) == 0 {
		return true
	}
	for _, e := range cert.NotifyEvents {
		if e == event {
			return true
		}
	}
	return false
}

// render 渲染消息（优先使用用户自定义模板，未配置时用默认文案）
func (s *NotificationService) render(ctx context.Context, event domain.NotificationEvent, cert *domain.Certificate, details map[string]string) (title, body string) {
	title, body = defaultTemplate(event, cert, details)
	if s.templates == nil {
		return title, body
	}
	t, err := s.templates.Get(ctx, event)
	if err != nil || t == nil || (t.Title == "" && t.Body == "") {
		return title, body
	}
	if t.Title != "" {
		title = renderTemplate(t.Title, details)
	}
	if t.Body != "" {
		body = renderTemplate(t.Body, details)
	}
	return title, body
}

// defaultTemplate 默认文案
func defaultTemplate(event domain.NotificationEvent, cert *domain.Certificate, details map[string]string) (title, body string) {
	switch event {
	case domain.EventRenewSuccess:
		title = "证书续期成功：" + cert.PrimaryDomain
		body = "证书已自动续期，新有效期至 " + cert.NotAfter.Format("2006-01-02")
	case domain.EventRenewFailed:
		title = "证书续期失败：" + cert.PrimaryDomain
		body = "自动续期失败，错误：" + cert.LastError
	case domain.EventIssueSuccess:
		title = "证书签发成功：" + cert.PrimaryDomain
		body = "证书已成功签发，有效期至 " + cert.NotAfter.Format("2006-01-02")
	case domain.EventIssueFailed:
		title = "证书签发失败：" + cert.PrimaryDomain
		body = "签发失败，错误：" + cert.LastError
	case domain.EventDeploySuccess:
		title = "证书部署成功：" + cert.PrimaryDomain
		body = "已成功部署到 " + details["target"]
	case domain.EventDeployFailed:
		title = "证书部署失败：" + cert.PrimaryDomain
		body = "部署到 " + details["target"] + " 失败，错误：" + details["error"]
	default:
		title = "证书即将到期：" + cert.PrimaryDomain
		body = "证书将在 {days_left} 天后到期（{expires}），请及时处理。"
	}
	return title, body
}

// renderTemplate 替换 {key} 变量
func renderTemplate(text string, details map[string]string) string {
	for k, v := range details {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

// ListTemplates 全部已配置模板
func (s *NotificationService) ListTemplates(ctx context.Context) ([]*domain.NotificationTemplate, error) {
	return s.templates.List(ctx)
}

// SaveTemplate 保存模板（title/body 均为空则删除配置，回退默认）
func (s *NotificationService) SaveTemplate(ctx context.Context, t *domain.NotificationTemplate) error {
	if !t.Event.Valid() {
		return fmt.Errorf("%w: 无效通知事件 %s", domain.ErrInvalidArgument, t.Event)
	}
	if t.Title == "" && t.Body == "" {
		// 空模板视为删除自定义配置
		return s.templates.Delete(ctx, t.Event)
	}
	// 标题禁止换行（防 SMTP 头注入）；长度限制
	if strings.ContainsAny(t.Title, "\r\n") {
		return fmt.Errorf("%w: 标题不能包含换行符", domain.ErrInvalidArgument)
	}
	if len(t.Title) > 1000 {
		return fmt.Errorf("%w: 标题过长（上限 1000 字符）", domain.ErrInvalidArgument)
	}
	if len(t.Body) > 10000 {
		return fmt.Errorf("%w: 正文过长（上限 10000 字符）", domain.ErrInvalidArgument)
	}
	t.UpdatedAt = s.clock.Now()
	return s.templates.Upsert(ctx, t)
}

func (s *NotificationService) sendWithRetry(ctx context.Context, event domain.NotificationEvent, channelID, ruleID, certID string, daysBefore int, title, body string, details map[string]string) {
	channelName := channelID
	if ch, err := s.channels.Get(ctx, channelID); err == nil {
		channelName = ch.Name
	}
	ruleName := ""
	if rule, err := s.rules.GetByID(ctx, ruleID); err == nil {
		ruleName = rule.Name
	}
	notifier, err := s.channels.SendToChannel(ctx, channelID)
	if err != nil {
		s.recordSend(ctx, event, certID, ruleID, ruleName, channelID, channelName, false, err.Error())
		s.auditNotify(ctx, "channel:"+channelID, title, false, err.Error())
		return
	}
	msg := domain.NotificationMessage{Title: title, Body: body, Details: details}
	var lastErr error
	// 立即 + 2 次重试（5s / 10s 退避）；最后一次失败后不再等待
	for attempt := 0; attempt < 3; attempt++ {
		if err := notifier.Send(msg); err == nil {
			s.recordSend(ctx, event, certID, ruleID, ruleName, channelID, channelName, true, "")
			// 仅到期提醒（daysBefore>=0）写防重日志；事件型通知（-1）不记录
			if daysBefore >= 0 {
				_ = s.logs.Append(ctx, &domain.NotificationLog{
					ID:         newID(),
					CertID:     certID,
					RuleID:     ruleID,
					DaysBefore: daysBefore,
					SentAt:     s.clock.Now(),
				})
			}
			s.auditNotify(ctx, channelID, title, true, "")
			return
		} else {
			lastErr = err
			if attempt == 2 || ctx.Err() != nil {
				break // 最后一次失败或 ctx 已取消：不再退避等待
			}
			s.sleepBetween(time.Duration(attempt+1) * 5 * time.Second)
			if ctx.Err() != nil {
				break
			}
		}
	}
	s.recordSend(ctx, event, certID, ruleID, ruleName, channelID, channelName, false, lastErr.Error())
	s.auditNotify(ctx, channelID, title, false, lastErr.Error())
}

// recordSend 记录发送历史（成功/失败均记录）
func (s *NotificationService) recordSend(ctx context.Context, event domain.NotificationEvent, certID, ruleID, ruleName, channelID, channelName string, ok bool, detail string) {
	status := "success"
	if !ok {
		status = "failed"
	}
	_ = s.sendLogs.Append(ctx, &domain.NotificationSendLog{
		ID:          newID(),
		CertID:      certID,
		RuleID:      ruleID,
		RuleName:    ruleName,
		ChannelID:   channelID,
		ChannelName: channelName,
		Event:       event,
		Status:      status,
		Detail:      detail,
		SentAt:      s.clock.Now(),
	})
}

// ListSendLogs 发送历史（管理员/操作员可查看）
func (s *NotificationService) ListSendLogs(ctx context.Context, filter domain.NotificationSendLogQuery, limit, offset int) ([]*domain.NotificationSendLog, error) {
	return s.sendLogs.List(ctx, filter, limit, offset)
}

// CountSendLogs 发送历史总数（分页元信息）
func (s *NotificationService) CountSendLogs(ctx context.Context, filter domain.NotificationSendLogQuery) (int64, error) {
	return s.sendLogs.Count(ctx, filter)
}

// auditNotify 通知审计
func (s *NotificationService) auditNotify(ctx context.Context, channel, title string, ok bool, detail string) {
	action := domain.AuditAction("notify.sent")
	if !ok {
		action = "notify.failed"
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Username:  "system",
		Action:    action,
		Resource:  title,
		Detail:    fmt.Sprintf("channel=%s %s", channel, detail),
		CreatedAt: s.clock.Now(),
	})
}

// ---------------- 规则管理 ----------------

type NotificationRuleInput struct {
	Name       string
	Event      domain.NotificationEvent
	Channels   []string // 兼容：渠道类型名（旧客户端）
	ChannelIDs []string // 引用界面配置的渠道
	DaysBefore []int
	Enabled    bool
}

func (s *NotificationService) CreateRule(ctx context.Context, input NotificationRuleInput) (*domain.NotificationRule, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: 规则名称不能为空", domain.ErrInvalidArgument)
	}
	if !input.Event.Valid() {
		return nil, fmt.Errorf("%w: 无效通知事件 %s", domain.ErrInvalidArgument, input.Event)
	}
	if len(input.ChannelIDs) == 0 && len(input.Channels) == 0 {
		return nil, fmt.Errorf("%w: 至少选择一个通知渠道", domain.ErrInvalidArgument)
	}
	// 校验引用的渠道存在（停用渠道仍可引用，发送时跳过）
	for _, cid := range input.ChannelIDs {
		if _, err := s.channels.Get(ctx, cid); err != nil {
			return nil, fmt.Errorf("%w: 通知渠道不存在", domain.ErrInvalidArgument)
		}
	}
	var channels []domain.NotificationChannel
	for _, c := range input.Channels {
		ch := domain.NotificationChannel(c)
		if !ch.Valid() {
			return nil, fmt.Errorf("%w: 无效通知渠道 %s", domain.ErrInvalidArgument, c)
		}
		channels = append(channels, ch)
	}
	rule := &domain.NotificationRule{
		ID:         newID(),
		Name:       input.Name,
		Event:      input.Event,
		Channels:   channels,
		ChannelIDs: input.ChannelIDs,
		DaysBefore: input.DaysBefore,
		Enabled:    input.Enabled,
		CreatedAt:  s.clock.Now(),
		UpdatedAt:  s.clock.Now(),
	}
	if err := s.rules.Create(ctx, rule); err != nil {
		return nil, err
	}
	s.auditRule(ctx, "rule.create", rule.Name)
	return rule, nil
}

func (s *NotificationService) ListRules(ctx context.Context) ([]*domain.NotificationRule, error) {
	return s.rules.List(ctx)
}

// UpdateRule 更新规则（全量更新；校验与创建一致）
func (s *NotificationService) UpdateRule(ctx context.Context, id string, input NotificationRuleInput) (*domain.NotificationRule, error) {
	rule, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name == "" {
		return nil, fmt.Errorf("%w: 规则名称不能为空", domain.ErrInvalidArgument)
	}
	if !input.Event.Valid() {
		return nil, fmt.Errorf("%w: 无效通知事件 %s", domain.ErrInvalidArgument, input.Event)
	}
	if len(input.ChannelIDs) == 0 && len(input.Channels) == 0 {
		return nil, fmt.Errorf("%w: 至少选择一个通知渠道", domain.ErrInvalidArgument)
	}
	// 校验引用的渠道存在（停用渠道仍可引用，发送时跳过）
	for _, cid := range input.ChannelIDs {
		if _, err := s.channels.Get(ctx, cid); err != nil {
			return nil, fmt.Errorf("%w: 通知渠道不存在", domain.ErrInvalidArgument)
		}
	}
	var channels []domain.NotificationChannel
	for _, c := range input.Channels {
		ch := domain.NotificationChannel(c)
		if !ch.Valid() {
			return nil, fmt.Errorf("%w: 无效通知渠道 %s", domain.ErrInvalidArgument, c)
		}
		channels = append(channels, ch)
	}
	rule.Name = input.Name
	rule.Event = input.Event
	rule.Channels = channels
	rule.ChannelIDs = input.ChannelIDs
	rule.DaysBefore = input.DaysBefore
	rule.Enabled = input.Enabled
	rule.UpdatedAt = s.clock.Now()
	if err := s.rules.Update(ctx, rule); err != nil {
		return nil, err
	}
	s.auditRule(ctx, "rule.update", rule.Name)
	return rule, nil
}

func (s *NotificationService) DeleteRule(ctx context.Context, id string) error {
	rule, err := s.rules.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.rules.Delete(ctx, id); err != nil {
		return err
	}
	s.auditRule(ctx, "rule.delete", rule.Name)
	return nil
}

func (s *NotificationService) auditRule(ctx context.Context, action string, name string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    domain.AuditAction(action),
		Resource:  name,
		CreatedAt: s.clock.Now(),
	})
}
