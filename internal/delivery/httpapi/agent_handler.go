package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

// AgentHandler 同时处理 agent 出站轮询接口（无需 JWT）与管理接口（需认证）
type AgentHandler struct {
	agents *application.AgentService
}

func newAgentHandler(agents *application.AgentService) *AgentHandler {
	return &AgentHandler{agents: agents}
}

// AgentRoutes agent 出站接口（token 认证）
func (h *AgentHandler) AgentRoutes() chi.Router {
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Get("/tasks", h.tasks)
	r.Post("/tasks/{id}/result", h.result)
	r.Get("/upgrade", h.upgradeInfo)
	r.Get("/binaries/{version}/download", h.binaryDownload)
	return r
}

// AdminRoutes 管理接口（JWT/API Token 认证，挂认证组内）
func (h *AgentHandler) AdminRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/binaries", h.listBinaries)
	r.Get("/{id}/tasks", h.taskHistory)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin))
		r.Get("/{id}", h.get)
		r.Post("/", h.create)
		r.Post("/binaries", h.uploadBinary)
		r.Post("/batch-upgrade", h.batchUpgrade)
		r.Put("/{id}", h.rename)
		r.Post("/{id}/upgrade", h.requestUpgrade)
		r.Post("/{id}/enabled", h.setEnabled)
		r.Delete("/{id}", h.delete)
	})
	return r
}

type taskHistoryResponse struct {
	ID                string `json:"id"`
	CertificateID     string `json:"certificate_id"`
	CertificateDomain string `json:"certificate_domain"`
	TargetID          string `json:"target_id"`
	Status            string `json:"status"`
	Checksum          string `json:"checksum"`
	Message           string `json:"message"`
	CreatedAt         string `json:"created_at"`
	FinishedAt        string `json:"finished_at"`
}

func (h *AgentHandler) taskHistory(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.agents.TaskHistory(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]taskHistoryResponse, 0, len(tasks))
	for _, t := range tasks {
		resp := taskHistoryResponse{
			ID: t.ID, CertificateID: t.CertificateID, CertificateDomain: t.CertificateDomain, TargetID: t.TargetID,
			Status: string(t.Status), Checksum: t.Checksum, Message: t.Message,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		}
		if t.FinishedAt != nil {
			resp.FinishedAt = t.FinishedAt.Format(time.RFC3339)
		}
		out = append(out, resp)
	}
	writeOK(w, r, out)
}

type batchUpgradeRequest struct {
	IDs     []string `json:"ids"`
	Version string   `json:"version"`
}

// batchUpgrade 批量升级（上限 50，逐个请求，失败收集）
func (h *AgentHandler) batchUpgrade(w http.ResponseWriter, r *http.Request) {
	var req batchUpgradeRequest
	if !readJSON(w, r, &req) {
		return
	}
	ids := dedupIDs(req.IDs)
	if len(ids) == 0 {
		writeError(w, r, http.StatusBadRequest, "请选择至少一个 Agent")
		return
	}
	if len(ids) > 50 {
		writeError(w, r, http.StatusBadRequest, "单次批量升级最多 50 个 Agent")
		return
	}
	result := batchResult{Failed: []batchFailure{}}
	for _, id := range ids {
		if err := h.agents.RequestUpgrade(r.Context(), id, req.Version); err != nil {
			result.Failed = append(result.Failed, batchFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Success++
	}
	writeOK(w, r, result)
}

type registerRequest struct {
	Token   string `json:"token"`
	Version string `json:"version"`
	OS      string `json:"os"`
}

func (h *AgentHandler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !readJSON(w, r, &req) {
		return
	}
	agent, err := h.agents.Authenticate(r.Context(), req.Token, req.Version, req.OS)
	if err != nil {
		writeRawError(w, http.StatusUnauthorized, "Agent Token 无效")
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]any{"id": agent.ID, "status": "ok"})
}

func (h *AgentHandler) tasks(w http.ResponseWriter, r *http.Request) {
	agent := agentFromRequest(w, r, h)
	if agent == nil {
		return
	}
	payload, err := h.agents.ClaimTask(r.Context(), agent.ID)
	if err != nil {
		writeRawError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if payload == nil {
		writeRawJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]any{"task": payload})
}

type taskResultRequest struct {
	TaskID   string `json:"task_id"`
	Success  bool   `json:"success"`
	Checksum string `json:"checksum"`
	Message  string `json:"message"`
}

func (h *AgentHandler) result(w http.ResponseWriter, r *http.Request) {
	agent := agentFromRequest(w, r, h)
	if agent == nil {
		return
	}
	var req taskResultRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.agents.SubmitResult(r.Context(), agent.ID, chi.URLParam(r, "id"), req.Success, req.Checksum, req.Message); err != nil {
		writeRawError(w, mapError(err), err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// agentFromRequest 校验 agent token（X-Agent-Token 头）
func agentFromRequest(w http.ResponseWriter, r *http.Request, h *AgentHandler) *domain.Agent {
	token := r.Header.Get("X-Agent-Token")
	if token == "" {
		writeRawError(w, http.StatusUnauthorized, "缺少 Agent Token")
		return nil
	}
	agent, err := h.agents.Authenticate(r.Context(), token, "", "")
	if err != nil {
		writeRawError(w, http.StatusUnauthorized, "Agent Token 无效")
		return nil
	}
	return agent
}

// upgradeInfo agent 查询升级指令
func (h *AgentHandler) upgradeInfo(w http.ResponseWriter, r *http.Request) {
	agent := agentFromRequest(w, r, h)
	if agent == nil {
		return
	}
	info, err := h.agents.QueryUpgrade(r.Context(), agent.ID)
	if err != nil {
		writeRawError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if info == nil {
		writeRawJSON(w, http.StatusOK, map[string]any{"upgrade": nil})
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]any{"upgrade": info})
}

// binaryDownload agent 下载新版本二进制
func (h *AgentHandler) binaryDownload(w http.ResponseWriter, r *http.Request) {
	agent := agentFromRequest(w, r, h)
	if agent == nil {
		return
	}
	version := chi.URLParam(r, "version")
	// 仅允许下载当前下发给该 agent 的版本
	if agent.UpgradeVersion != version {
		writeError(w, r, http.StatusForbidden, "未下发该版本的升级指令")
		return
	}
	data, err := h.agents.BinaryData(r.Context(), version)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "二进制不存在")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\"argus-agent-"+version+"\"")
	_, _ = w.Write(data)
}

// uploadBinary admin 上传 Agent 新版本二进制（multipart: version + file）
func (h *AgentHandler) uploadBinary(w http.ResponseWriter, r *http.Request) {
	version := r.FormValue("version")
	if version == "" {
		writeError(w, r, http.StatusBadRequest, "缺少 version 字段")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()
	// 大小限制 256MB（防内存耗尽）
	data, err := io.ReadAll(io.LimitReader(file, 256<<20))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if len(data) >= 256<<20 {
		writeError(w, r, http.StatusRequestEntityTooLarge, "文件超过 256MB 上限")
		return
	}
	sha, err := h.agents.UploadBinary(r.Context(), version, data)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]any{"version": version, "sha256": sha})
}

func (h *AgentHandler) listBinaries(w http.ResponseWriter, r *http.Request) {
	binaries, err := h.agents.ListBinaries(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, binaries)
}

type upgradeRequest struct {
	Version string `json:"version"`
}

type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *AgentHandler) setEnabled(w http.ResponseWriter, r *http.Request) {
	var req setEnabledRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.agents.SetEnabled(r.Context(), chi.URLParam(r, "id"), req.Enabled); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *AgentHandler) requestUpgrade(w http.ResponseWriter, r *http.Request) {
	var req upgradeRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := h.agents.RequestUpgrade(r.Context(), chi.URLParam(r, "id"), req.Version); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

type agentResponse struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Status             string  `json:"status"`
	Enabled            bool    `json:"enabled"`
	LastSeenAt         *string `json:"last_seen_at"`
	Version            string  `json:"version"`
	OS                 string  `json:"os"`
	CreatedAt          string  `json:"created_at"`
	UpgradeVersion     string  `json:"upgrade_version"`
	UpgradeSHA256      string  `json:"upgrade_sha256"`
	UpgradeRequestedAt *string `json:"upgrade_requested_at"`
}

func toAgentResponse(a *domain.Agent) agentResponse {
	out := agentResponse{
		ID:             a.ID,
		Name:           a.Name,
		Status:         a.Status,
		Enabled:        a.Enabled,
		Version:        a.Version,
		OS:             a.OS,
		CreatedAt:      a.CreatedAt.Format(time.RFC3339),
		UpgradeVersion: a.UpgradeVersion,
		UpgradeSHA256:  a.UpgradeSHA256,
	}
	if a.LastSeenAt != nil {
		ts := a.LastSeenAt.Format(time.RFC3339)
		out.LastSeenAt = &ts
	}
	if a.UpgradeRequestedAt != nil {
		ts := a.UpgradeRequestedAt.Format(time.RFC3339)
		out.UpgradeRequestedAt = &ts
	}
	return out
}

type createAgentRequest struct {
	Name string `json:"name"`
}

type createAgentResponse struct {
	agentResponse
	Token string `json:"token"` // 明文仅一次
}

func (h *AgentHandler) list(w http.ResponseWriter, r *http.Request) {
	agents, err := h.agents.ListAgents(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]agentResponse, 0, len(agents))
	for _, a := range agents {
		out = append(out, toAgentResponse(a))
	}
	writeOK(w, r, out)
}

// get 查询单个 Agent
func (h *AgentHandler) get(w http.ResponseWriter, r *http.Request) {
	agent, err := h.agents.GetAgent(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toAgentResponse(agent))
}

type renameAgentRequest struct {
	Name string `json:"name"`
}

// rename 重命名 Agent
func (h *AgentHandler) rename(w http.ResponseWriter, r *http.Request) {
	var req renameAgentRequest
	if !readJSON(w, r, &req) {
		return
	}
	agent, err := h.agents.RenameAgent(r.Context(), chi.URLParam(r, "id"), req.Name)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toAgentResponse(agent))
}

func (h *AgentHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if !readJSON(w, r, &req) {
		return
	}
	agent, token, err := h.agents.CreateAgent(r.Context(), req.Name)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, createAgentResponse{
		agentResponse: toAgentResponse(agent),
		Token:         token,
	})
}

func (h *AgentHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.agents.DeleteAgent(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
