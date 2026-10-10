package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type dnsAccountResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Provider       string         `json:"provider"`
	HasCredentials bool           `json:"has_credentials"`
	Status         string         `json:"status"`
	LastError      string         `json:"last_error"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	Config         map[string]any `json:"config,omitempty"`
}

func toDnsAccountResponse(a *domain.DnsProviderAccount) dnsAccountResponse {
	return dnsAccountResponse{
		ID:             a.ID,
		Name:           a.Name,
		Provider:       string(a.Provider),
		HasCredentials: a.CredentialsEnc != "",
		Status:         a.Status,
		LastError:      a.LastError,
		CreatedAt:      a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      a.UpdatedAt.Format(time.RFC3339),
	}
}

type createDnsAccountRequest struct {
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Credentials map[string]string `json:"credentials"`
}

type DnsAccountHandler struct {
	accounts *application.DnsProviderAccountService
}

func newDnsAccountHandler(accounts *application.DnsProviderAccountService) *DnsAccountHandler {
	return &DnsAccountHandler{accounts: accounts}
}

func (h *DnsAccountHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/providers", h.providerList)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Get("/{id}", h.get)
		r.Post("/", h.create)
		r.Put("/{id}", h.update)
		r.Delete("/{id}", h.delete)
	})
	return r
}

func (h *DnsAccountHandler) list(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.accounts.List(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]dnsAccountResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toDnsAccountResponse(a))
	}
	writeOK(w, r, out)
}

func (h *DnsAccountHandler) providerList(w http.ResponseWriter, r *http.Request) {
	writeOK(w, r, []string{"alidns", "cloudflare", "dnspod", "godaddy"})
}

func (h *DnsAccountHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createDnsAccountRequest
	if !readJSON(w, r, &req) {
		return
	}
	account, err := h.accounts.Create(r.Context(), application.CreateDnsAccountInput{
		Name:        req.Name,
		Provider:    req.Provider,
		Credentials: req.Credentials,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toDnsAccountResponse(account))
}

func (h *DnsAccountHandler) get(w http.ResponseWriter, r *http.Request) {
	account, err := h.accounts.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	resp := toDnsAccountResponse(account)
	if cfg, err := h.accounts.GetMaskedConfig(r.Context(), account.ID); err == nil {
		resp.Config = cfg
	}
	writeOK(w, r, resp)
}

func (h *DnsAccountHandler) update(w http.ResponseWriter, r *http.Request) {
	var req createDnsAccountRequest
	if !readJSON(w, r, &req) {
		return
	}
	account, err := h.accounts.Update(r.Context(), chi.URLParam(r, "id"), application.UpdateDnsAccountInput{
		Name:        req.Name,
		Credentials: req.Credentials,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toDnsAccountResponse(account))
}

func (h *DnsAccountHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.accounts.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
