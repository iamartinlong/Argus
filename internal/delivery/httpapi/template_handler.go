package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type templateResponse struct {
	Event     string `json:"event"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}

func toTemplateResponse(t *domain.NotificationTemplate) templateResponse {
	return templateResponse{
		Event:     string(t.Event),
		Title:     t.Title,
		Body:      t.Body,
		UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
}

type templateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type TemplateHandler struct {
	notify *application.NotificationService
}

func newTemplateHandler(notify *application.NotificationService) *TemplateHandler {
	return &TemplateHandler{notify: notify}
}

func (h *TemplateHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Put("/{event}", h.save)
		r.Delete("/{event}", h.reset)
	})
	return r
}

func (h *TemplateHandler) list(w http.ResponseWriter, r *http.Request) {
	templates, err := h.notify.ListTemplates(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]templateResponse, 0, len(templates))
	for _, t := range templates {
		out = append(out, toTemplateResponse(t))
	}
	writeOK(w, r, out)
}

func (h *TemplateHandler) save(w http.ResponseWriter, r *http.Request) {
	var req templateRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.notify.SaveTemplate(r.Context(), &domain.NotificationTemplate{
		Event: domain.NotificationEvent(chi.URLParam(r, "event")),
		Title: req.Title,
		Body:  req.Body,
	}); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *TemplateHandler) reset(w http.ResponseWriter, r *http.Request) {
	if err := h.notify.SaveTemplate(r.Context(), &domain.NotificationTemplate{
		Event: domain.NotificationEvent(chi.URLParam(r, "event")),
	}); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
