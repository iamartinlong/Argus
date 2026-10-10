package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type channelResponse struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Enabled   bool           `json:"enabled"`
	Config    map[string]any `json:"config,omitempty"` // 仅详情端点返回；敏感键置空
	CreatedAt string         `json:"created_at"`
}

func toChannelResponse(c *domain.NotificationChannelRecord) channelResponse {
	return channelResponse{
		ID:        c.ID,
		Name:      c.Name,
		Type:      string(c.Type),
		Enabled:   c.Enabled,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}

type channelRequest struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Config  map[string]any `json:"config"`
	Enabled *bool          `json:"enabled"`
}

type ChannelHandler struct {
	channels *application.ChannelService
}

func newChannelHandler(channels *application.ChannelService) *ChannelHandler {
	return &ChannelHandler{channels: channels}
}

func (h *ChannelHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Get("/{id}", h.get)
		r.Post("/", h.create)
		r.Put("/{id}", h.update)
		r.Delete("/{id}", h.delete)
		r.Post("/{id}/test", h.test)
	})
	return r
}

// get 渠道详情（含解密配置；敏感键置空，供编辑回填）
func (h *ChannelHandler) get(w http.ResponseWriter, r *http.Request) {
	channel, err := h.channels.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	resp := toChannelResponse(channel)
	if cfg, err := h.channels.GetConfig(r.Context(), channel.ID); err == nil {
		resp.Config = cfg
	}
	writeOK(w, r, resp)
}

func (h *ChannelHandler) list(w http.ResponseWriter, r *http.Request) {
	channels, err := h.channels.List(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]channelResponse, 0, len(channels))
	for _, c := range channels {
		out = append(out, toChannelResponse(c))
	}
	writeOK(w, r, out)
}

func (h *ChannelHandler) create(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if !readJSON(w, r, &req) {
		return
	}
	channel, err := h.channels.Create(r.Context(), application.ChannelInput{
		Name:    req.Name,
		Type:    domain.NotificationChannel(req.Type),
		Config:  req.Config,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toChannelResponse(channel))
}

func (h *ChannelHandler) update(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if !readJSON(w, r, &req) {
		return
	}
	channel, err := h.channels.Update(r.Context(), chi.URLParam(r, "id"), application.ChannelInput{
		Name:    req.Name,
		Type:    domain.NotificationChannel(req.Type),
		Config:  req.Config,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toChannelResponse(channel))
}

func (h *ChannelHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.channels.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *ChannelHandler) test(w http.ResponseWriter, r *http.Request) {
	if err := h.channels.Test(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
