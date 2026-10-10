package httpapi

import (
	"encoding/csv"
	"net/http"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// sanitizeCSV 防 CSV 公式注入：以 = + - @ \t \r 开头的单元格前缀加单引号
func sanitizeCSV(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// writeCSV 流式输出 CSV 附件（单元格自动防公式注入）
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, row := range rows {
		clean := make([]string, len(row))
		for i, cell := range row {
			clean[i] = sanitizeCSV(cell)
		}
		_ = cw.Write(clean)
	}
	cw.Flush()
}

func (h *Handler) exportAudit(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	if p == nil || p.User.Role != domain.RoleAdmin {
		writeError(w, r, http.StatusForbidden, "无权限")
		return
	}
	q := r.URL.Query()
	filter := domain.AuditQuery{
		Username: q.Get("username"),
		Action:   q.Get("action"),
		Keyword:  q.Get("keyword"),
	}
	// 全量导出（不分页，上限 10000 条防滥用）
	logs, err := h.audit.List(r.Context(), filter, 10000, 0)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	rows := make([][]string, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, []string{
			l.CreatedAt.Format(time.RFC3339), l.Username, string(l.Action), l.Resource, l.Detail, l.IP,
		})
	}
	writeCSV(w, "audit-logs.csv",
		[]string{"time", "username", "action", "resource", "detail", "ip"}, rows)
}

func (h *Handler) exportTaskLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.tasks.List(r.Context(), 10000, 0)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	rows := make([][]string, 0, len(logs))
	for _, l := range logs {
		finished := ""
		if l.FinishedAt != nil {
			finished = l.FinishedAt.Format(time.RFC3339)
		}
		rows = append(rows, []string{
			l.StartedAt.Format(time.RFC3339), finished, string(l.Type), string(l.Status), l.Detail,
		})
	}
	writeCSV(w, "task-logs.csv",
		[]string{"started_at", "finished_at", "type", "status", "detail"}, rows)
}
