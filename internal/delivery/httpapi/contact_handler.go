package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type contactResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toContactResponse(c *domain.ContactPerson) contactResponse {
	return contactResponse{
		ID:        c.ID,
		Name:      c.Name,
		Email:     c.Email,
		Phone:     c.Phone,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

type contactRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type ContactHandler struct {
	contacts *application.ContactService
}

func newContactHandler(contacts *application.ContactService) *ContactHandler {
	return &ContactHandler{contacts: contacts}
}

func (h *ContactHandler) Routes() chi.Router {
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

func (h *ContactHandler) list(w http.ResponseWriter, r *http.Request) {
	contacts, err := h.contacts.List(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]contactResponse, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, toContactResponse(c))
	}
	writeOK(w, r, out)
}

func (h *ContactHandler) create(w http.ResponseWriter, r *http.Request) {
	var req contactRequest
	if !readJSON(w, r, &req) {
		return
	}
	contact, err := h.contacts.Create(r.Context(), application.ContactInput{
		Name: req.Name, Email: req.Email, Phone: req.Phone,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toContactResponse(contact))
}

func (h *ContactHandler) update(w http.ResponseWriter, r *http.Request) {
	var req contactRequest
	if !readJSON(w, r, &req) {
		return
	}
	contact, err := h.contacts.Update(r.Context(), chi.URLParam(r, "id"), application.ContactInput{
		Name: req.Name, Email: req.Email, Phone: req.Phone,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toContactResponse(contact))
}

func (h *ContactHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.contacts.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}