package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type ruleResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Event      string   `json:"event"`
	Channels   []string `json:"channels"`
	ChannelIDs []string `json:"channel_ids"`
	DaysBefore []int    `json:"days_before"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  string   `json:"created_at"`
}

func toRuleResponse(r *domain.NotificationRule) ruleResponse {
	channels := make([]string, 0, len(r.Channels))
	for _, c := range r.Channels {
		channels = append(channels, string(c))
	}
	// 保证非 nil（历史数据可能存 "null"）
	channelIDs := r.ChannelIDs
	if channelIDs == nil {
		channelIDs = []string{}
	}
	days := r.DaysBefore
	if days == nil {
		days = []int{}
	}
	return ruleResponse{
		ID:         r.ID,
		Name:       r.Name,
		Event:      string(r.Event),
		Channels:   channels,
		ChannelIDs: channelIDs,
		DaysBefore: days,
		Enabled:    r.Enabled,
		CreatedAt:  r.CreatedAt.Format(time.RFC3339),
	}
}

type createRuleRequest struct {
	Name       string   `json:"name"`
	Event      string   `json:"event"`
	Channels   []string `json:"channels"` // 兼容：渠道类型
	ChannelIDs []string `json:"channel_ids"` // 引用通知渠道记录
	DaysBefore []int    `json:"days_before"`
	Enabled    *bool    `json:"enabled"`
}

type NotificationRuleHandler struct {
	notify *application.NotificationService
	tasks  domain.TaskLogStore
}

func newNotificationRuleHandler(notify *application.NotificationService, tasks domain.TaskLogStore) *NotificationRuleHandler {
	return &NotificationRuleHandler{notify: notify, tasks: tasks}
}

func (h *NotificationRuleHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/channels", h.channels)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Post("/", h.create)
		r.Put("/{id}", h.update)
		r.Delete("/{id}", h.delete)
	})
	return r
}

func (h *NotificationRuleHandler) list(w http.ResponseWriter, r *http.Request) {
	rules, err := h.notify.ListRules(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]ruleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toRuleResponse(rule))
	}
	writeOK(w, r, out)
}

var notifyChannels = []map[string]string{
	{"id": "smtp", "name": "邮件 SMTP"},
	{"id": "webhook", "name": "通用 Webhook"},
	{"id": "wecom", "name": "企业微信机器人"},
	{"id": "dingtalk", "name": "钉钉机器人"},
	{"id": "feishu", "name": "飞书机器人"},
}

// channels 兼容端点：返回渠道类型列表（新客户端使用 /notification-channels）
func (h *NotificationRuleHandler) channels(w http.ResponseWriter, r *http.Request) {
	writeOK(w, r, notifyChannelTypes)
}

var notifyChannelTypes = []map[string]string{
	{"id": "smtp", "name": "邮件 SMTP"},
	{"id": "webhook", "name": "通用 Webhook"},
	{"id": "wecom", "name": "企业微信机器人"},
	{"id": "dingtalk", "name": "钉钉机器人"},
	{"id": "feishu", "name": "飞书机器人"},
}

func (h *NotificationRuleHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createRuleRequest
	if !readJSON(w, r, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule, err := h.notify.CreateRule(r.Context(), application.NotificationRuleInput{
		Name:       req.Name,
		Event:      domain.NotificationEvent(req.Event),
		Channels:   req.Channels,
		ChannelIDs: req.ChannelIDs,
		DaysBefore: req.DaysBefore,
		Enabled:    enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toRuleResponse(rule))
}

func (h *NotificationRuleHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.notify.DeleteRule(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *NotificationRuleHandler) update(w http.ResponseWriter, r *http.Request) {
	var req createRuleRequest
	if !readJSON(w, r, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule, err := h.notify.UpdateRule(r.Context(), chi.URLParam(r, "id"), application.NotificationRuleInput{
		Name:       req.Name,
		Event:      domain.NotificationEvent(req.Event),
		Channels:   req.Channels,
		ChannelIDs: req.ChannelIDs,
		DaysBefore: req.DaysBefore,
		Enabled:    enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toRuleResponse(rule))
}
