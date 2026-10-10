package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type targetResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Enabled    bool   `json:"enabled"`
	LastStatus string `json:"last_status"`
	LastError  string `json:"last_error"`
	CreatedAt  string `json:"created_at"`
	Config     map[string]any `json:"config,omitempty"`
}

func toTargetResponse(t *domain.DeployTarget, config map[string]any) targetResponse {
	return targetResponse{
		ID:         t.ID,
		Name:       t.Name,
		Type:       string(t.Type),
		Enabled:    t.Enabled,
		LastStatus: t.LastStatus,
		LastError:  t.LastError,
		CreatedAt:  t.CreatedAt.Format(time.RFC3339),
		Config:     config,
	}
}

type createTargetRequest struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Config  map[string]any `json:"config"`
	Enabled *bool          `json:"enabled"`
}

type recordResponse struct {
	ID            string  `json:"id"`
	TargetID      string  `json:"target_id"`
	CertificateID string  `json:"certificate_id"`
	Status        string  `json:"status"`
	Checksum      string  `json:"checksum"`
	Message       string  `json:"message"`
	Retries       int     `json:"retries"`
	DeployedAt    *string `json:"deployed_at"`
	CreatedAt     string  `json:"created_at"`
}

func toRecordResponse(r *domain.DeployRecord) recordResponse {
	out := recordResponse{
		ID:            r.ID,
		TargetID:      r.TargetID,
		CertificateID: r.CertificateID,
		Status:        string(r.Status),
		Checksum:      r.Checksum,
		Message:       r.Message,
		Retries:       r.Retries,
		CreatedAt:     r.CreatedAt.Format(time.RFC3339),
	}
	if r.DeployedAt != nil {
		ts := r.DeployedAt.Format(time.RFC3339)
		out.DeployedAt = &ts
	}
	return out
}

type DeployHandler struct {
	deploy *application.DeployService
}

func newDeployHandler(deploy *application.DeployService) *DeployHandler {
	return &DeployHandler{deploy: deploy}
}

func (h *DeployHandler) Routes() chi.Router {
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

func (h *DeployHandler) list(w http.ResponseWriter, r *http.Request) {
	targets, err := h.deploy.ListTargets(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]targetResponse, 0, len(targets))
	for _, t := range targets {
		out = append(out, toTargetResponse(t, nil))
	}
	writeOK(w, r, out)
}

func (h *DeployHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if !readJSON(w, r, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	target, err := h.deploy.CreateTarget(r.Context(), application.DeployTargetInput{
		Name:    req.Name,
		Type:    domain.DeployTargetType(req.Type),
		Config:  req.Config,
		Enabled: enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toTargetResponse(target, nil))
}

func (h *DeployHandler) test(w http.ResponseWriter, r *http.Request) {
	if err := h.deploy.TestTarget(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

// get 查询单个目标（含解密后的 config，供查看/编辑回显）
func (h *DeployHandler) get(w http.ResponseWriter, r *http.Request) {
	target, cfg, err := h.deploy.GetTarget(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toTargetResponse(target, cfg))
}

// update 更新目标名称/启用状态/配置
func (h *DeployHandler) update(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if !readJSON(w, r, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	target, err := h.deploy.UpdateTarget(r.Context(), chi.URLParam(r, "id"), application.DeployTargetInput{
		Name:    req.Name,
		Type:    domain.DeployTargetType(req.Type),
		Config:  req.Config,
		Enabled: enabled,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toTargetResponse(target, req.Config))
}

func (h *DeployHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.deploy.DeleteTarget(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
