package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type accountResponse struct {
	ID             string         `json:"id"`
	Provider       string         `json:"provider"`
	Name           string         `json:"name"`
	DirectoryURL   string         `json:"directory_url"`
	Contact        string         `json:"contact"`
	EABKID         string         `json:"eab_kid,omitempty"`
	HasEAB         bool           `json:"has_eab"`
	HasCredentials bool           `json:"has_credentials"`
	Status         string         `json:"status"`
	LastError      string         `json:"last_error"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	Config         map[string]any `json:"config,omitempty"` // 仅详情端点返回；敏感键置空
}

func toAccountResponse(a *domain.ProviderAccount) accountResponse {
	return accountResponse{
		ID:             a.ID,
		Provider:       a.Provider,
		Name:           a.Name,
		DirectoryURL:   a.DirectoryURL,
		Contact:        a.Contact,
		EABKID:         a.EABKID,
		HasEAB:         a.EABKID != "" || a.EABHMACKeyEnc != "",
		HasCredentials: a.CredentialsEnc != "",
		Status:         a.Status,
		LastError:      a.LastError,
		CreatedAt:      a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      a.UpdatedAt.Format(time.RFC3339),
	}
}

type createAccountRequest struct {
	Provider     string            `json:"provider"`
	Name         string            `json:"name"`
	DirectoryURL string            `json:"directory_url"`
	Contact      string            `json:"contact"`
	EABKID       string            `json:"eab_kid"`
	EABHMACKey   string            `json:"eab_hmac_key"`
	Credentials  map[string]string `json:"credentials"`
}

type AccountHandler struct {
	accounts *application.ProviderAccountService
	providers []string
}

func newAccountHandler(accounts *application.ProviderAccountService, providers []string) *AccountHandler {
	return &AccountHandler{accounts: accounts, providers: providers}
}

func (h *AccountHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/providers", h.providerList)
	// 写操作：仅 admin/operator
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Get("/{id}", h.get)
		r.Post("/", h.create)
		r.Put("/{id}", h.update)
		r.Post("/{id}/test", h.test)
		r.Delete("/{id}", h.delete)
	})
	return r
}

func (h *AccountHandler) list(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.accounts.List(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]accountResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toAccountResponse(a))
	}
	writeOK(w, r, out)
}

func (h *AccountHandler) providerList(w http.ResponseWriter, r *http.Request) {
	writeOK(w, r, h.providers)
}

func (h *AccountHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if !readJSON(w, r, &req) {
		return
	}
	account, err := h.accounts.Create(r.Context(), application.CreateAccountInput{
		Provider:     req.Provider,
		Name:         req.Name,
		DirectoryURL: req.DirectoryURL,
		Contact:      req.Contact,
		EABKID:       req.EABKID,
		EABHMACKey:   req.EABHMACKey,
		Credentials:  req.Credentials,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toAccountResponse(account))
}

// get 账户详情（含解密凭据；敏感键置空，供编辑回填）
func (h *AccountHandler) get(w http.ResponseWriter, r *http.Request) {
	account, err := h.accounts.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	resp := toAccountResponse(account)
	if cfg, err := h.accounts.GetMaskedConfig(r.Context(), account.ID); err == nil {
		resp.Config = cfg
	}
	writeOK(w, r, resp)
}

func (h *AccountHandler) update(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if !readJSON(w, r, &req) {
		return
	}
	account, err := h.accounts.Update(r.Context(), chi.URLParam(r, "id"), application.UpdateAccountInput{
		Name:         req.Name,
		DirectoryURL: req.DirectoryURL,
		Contact:      req.Contact,
		EABKID:       req.EABKID,
		EABHMACKey:   req.EABHMACKey,
		Credentials:  req.Credentials,
	})
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toAccountResponse(account))
}

func (h *AccountHandler) test(w http.ResponseWriter, r *http.Request) {
	if err := h.accounts.Test(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *AccountHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.accounts.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
