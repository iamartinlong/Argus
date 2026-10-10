package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type sendLogResponse struct {
	ID          string `json:"id"`
	CertID      string `json:"cert_id"`
	RuleID      string `json:"rule_id"`
	RuleName    string `json:"rule_name"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Event       string `json:"event"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	SentAt      string `json:"sent_at"`
}

func toSendLogResponse(l *domain.NotificationSendLog) sendLogResponse {
	return sendLogResponse{
		ID:          l.ID,
		CertID:      l.CertID,
		RuleID:      l.RuleID,
		RuleName:    l.RuleName,
		ChannelID:   l.ChannelID,
		ChannelName: l.ChannelName,
		Event:       string(l.Event),
		Status:      l.Status,
		Detail:      l.Detail,
		SentAt:      l.SentAt.Format(time.RFC3339),
	}
}

type SendLogHandler struct {
	notify *application.NotificationService
}

func newSendLogHandler(notify *application.NotificationService) *SendLogHandler {
	return &SendLogHandler{notify: notify}
}

func (h *SendLogHandler) Routes() chi.Router {
	r := chi.NewRouter()
	// 发送历史含规则/渠道/证书运维细节，仅 admin/operator 可查看
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Get("/", h.list)
	})
	return r
}

// Export 导出 CSV（全量，上限 10000 条）
func (h *SendLogHandler) Export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.NotificationSendLogQuery{
		ChannelID: q.Get("channel_id"),
		Status:    q.Get("status"),
		Keyword:   q.Get("keyword"),
	}
	logs, err := h.notify.ListSendLogs(r.Context(), filter, 10000, 0)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	rows := make([][]string, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, []string{
			l.SentAt.Format(time.RFC3339), l.CertID, l.RuleName, l.ChannelName,
			string(l.Event), l.Status, l.Detail,
		})
	}
	writeCSV(w, "notification-send-logs.csv",
		[]string{"sent_at", "cert_id", "rule", "channel", "event", "status", "detail"}, rows)
}

func (h *SendLogHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	q := r.URL.Query()
	filter := domain.NotificationSendLogQuery{
		ChannelID: q.Get("channel_id"),
		Status:    q.Get("status"),
		Keyword:   q.Get("keyword"),
	}
	logs, err := h.notify.ListSendLogs(r.Context(), filter, limit, offset)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	total, _ := h.notify.CountSendLogs(r.Context(), filter)
	out := make([]sendLogResponse, 0, len(logs))
	for _, l := range logs {
		out = append(out, toSendLogResponse(l))
	}
	writeOK(w, r, pageResponse{Items: out, Total: total, Limit: limit, Offset: offset})
}
