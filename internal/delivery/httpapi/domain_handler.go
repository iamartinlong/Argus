package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type DomainHandler struct {
	domains *application.DomainService
}

func newDomainHandler(domains *application.DomainService) *DomainHandler {
	return &DomainHandler{domains: domains}
}

// domainRequest 创建/更新共用（更新留空字段按默认值处理：期望状态空 = must_have_cert）
type domainRequest struct {
	Name           string   `json:"name"`
	ExpectedStatus string   `json:"expected_status"`
	Note           string   `json:"note"`
	Tags           []string `json:"tags"`
}

func (h *DomainHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Post("/", h.create)
		r.Put("/{id}", h.update)
		r.Delete("/{id}", h.delete)
	})
	return r
}

// list 台账列表（富化行：含实时计算的覆盖状态与当前证书）
func (h *DomainHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.domains.Ledger(r.Context(), 0)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, items)
}

func (h *DomainHandler) create(w http.ResponseWriter, r *http.Request) {
	var req domainRequest
	if !readJSON(w, r, &req) {
		return
	}
	d, err := h.domains.Create(r.Context(), actorFrom(r), application.DomainInput{
		Name:           req.Name,
		ExpectedStatus: req.ExpectedStatus,
		Note:           req.Note,
		Tags:           req.Tags,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toDomainItemResponse(d))
}

func (h *DomainHandler) update(w http.ResponseWriter, r *http.Request) {
	var req domainRequest
	if !readJSON(w, r, &req) {
		return
	}
	d, err := h.domains.Update(r.Context(), actorFrom(r), chi.URLParam(r, "id"), application.DomainInput{
		Name:           req.Name,
		ExpectedStatus: req.ExpectedStatus,
		Note:           req.Note,
		Tags:           req.Tags,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toDomainItemResponse(d))
}

func (h *DomainHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.domains.Delete(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

// actorFrom 从上下文取当前用户（API Token 调用时为 nil，审计记 system）
func actorFrom(r *http.Request) *domain.User {
	if p := userFrom(r.Context()); p != nil {
		return p.User
	}
	return nil
}

// domainItemResponse 台账项裸字段（创建/更新返回；列表/详情走富化行）
type domainItemResponse struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	ExpectedStatus string   `json:"expected_status"`
	Source         string   `json:"source"`
	Note           string   `json:"note"`
	Tags           []string `json:"tags"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func toDomainItemResponse(d *domain.Domain) domainItemResponse {
	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}
	return domainItemResponse{
		ID:             d.ID,
		Name:           d.Name,
		ExpectedStatus: string(d.ExpectedStatus),
		Source:         string(d.Source),
		Note:           d.Note,
		Tags:           tags,
		CreatedAt:      d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      d.UpdatedAt.Format(time.RFC3339),
	}
}
