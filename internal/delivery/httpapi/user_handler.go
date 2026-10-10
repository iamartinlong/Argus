package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type createUserRequest struct {
	Username string      `json:"username"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

type updateUserRequest struct {
	Role   domain.Role      `json:"role"`
	Status domain.UserStatus `json:"status"`
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type UserHandler struct {
	users *application.UserService
}

func newUserHandler(users *application.UserService) *UserHandler {
	return &UserHandler{users: users}
}

func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(requireRole(domain.RoleAdmin))
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Put("/{id}", h.update)
	r.Delete("/{id}", h.delete)
	return r
}

func (h *UserHandler) MeRoutes() chi.Router {
	r := chi.NewRouter()
	r.Put("/password", h.changePassword)
	return r
}

func (h *UserHandler) list(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u))
	}
	writeOK(w, r, out)
}

func (h *UserHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !readJSON(w, r, &req) {
		return
	}
	p := userFrom(r.Context())
	user, err := h.users.CreateUser(r.Context(), p.User, req.Username, req.Password, req.Role)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toUserResponse(user))
}

func (h *UserHandler) update(w http.ResponseWriter, r *http.Request) {
	var req updateUserRequest
	if !readJSON(w, r, &req) {
		return
	}
	p := userFrom(r.Context())
	user, err := h.users.UpdateUser(r.Context(), p.User, chi.URLParam(r, "id"), req.Role, req.Status)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toUserResponse(user))
}

func (h *UserHandler) delete(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	if err := h.users.DeleteUser(r.Context(), p.User, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *UserHandler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if !readJSON(w, r, &req) {
		return
	}
	p := userFrom(r.Context())
	if err := h.users.ChangePassword(r.Context(), p.User, req.OldPassword, req.NewPassword); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
