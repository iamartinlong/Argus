package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// zipFile zip 包内单文件
type zipFile struct {
	Name string
	Data []byte
}

func buildZip(files []zipFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// randomPassword 生成 length 位字母数字随机密码（crypto/rand）
func randomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		out[i] = charset[n.Int64()]
	}
	return string(out), nil
}

type certResponse struct {
	ID              string   `json:"id"`
	Domains         []string `json:"domains"`
	PrimaryDomain   string   `json:"primary_domain"`
	Provider        string   `json:"provider"`
	Status          string   `json:"status"`
	IssuerCA        string   `json:"issuer_ca"`
	NotBefore       string   `json:"not_before"`
	NotAfter        string   `json:"not_after"`
	Verification    string   `json:"verification"`
	DeployTargetIDs []string `json:"deploy_target_ids"`
	NotifyEvents    []string `json:"notify_events"`
	ContactName     string   `json:"contact_name"`
	ContactEmail    string   `json:"contact_email"`
	ContactPhone    string   `json:"contact_phone"`
	LastError       string   `json:"last_error"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	DaysToExpire    int64    `json:"days_to_expire"`
}

func toCertResponse(c *domain.Certificate) certResponse {
	days := int64(0)
	if !c.NotAfter.IsZero() {
		days = int64(time.Until(c.NotAfter).Hours() / 24)
	}
	return certResponse{
		ID:              c.ID,
		Domains:         c.Domains,
		PrimaryDomain:   c.PrimaryDomain,
		Provider:        c.Provider,
		Status:          string(c.Status),
		IssuerCA:        c.IssuerCA,
		NotBefore:       formatTime(c.NotBefore),
		NotAfter:        formatTime(c.NotAfter),
		Verification:    string(c.Verification),
		DeployTargetIDs: nonNilStrings(c.DeployTargetIDs),
		NotifyEvents:    notifyEventStrings(c.NotifyEvents),
		ContactName:     c.ContactName,
		ContactEmail:    c.ContactEmail,
		ContactPhone:    c.ContactPhone,
		LastError:       c.LastError,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339),
		DaysToExpire:    days,
	}
}

// notifyEventStrings 通知事件转字符串切片；nil → 空切片
func notifyEventStrings(events []domain.NotificationEvent) []string {
	if events == nil {
		return []string{}
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e))
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type orderResponse struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	Error         string `json:"error"`
	Attempts      int    `json:"attempts"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
}

func toOrderResponse(o *domain.Order) orderResponse {
	out := orderResponse{
		ID:        o.ID,
		Type:      string(o.Type),
		Status:    string(o.Status),
		Error:     o.Error,
		Attempts:  o.Attempts,
		StartedAt: o.StartedAt.Format(time.RFC3339),
	}
	if o.FinishedAt != nil {
		out.FinishedAt = o.FinishedAt.Format(time.RFC3339)
	}
	return out
}

type issueRequest struct {
	Domains              []string          `json:"domains"`
	AccountID            string            `json:"account_id"`
	ContactID            string            `json:"contact_id"`
	Verification         string            `json:"verification"`
	DNSConfig            map[string]string `json:"dns_config"`
	DNSProviderAccountID string            `json:"dns_provider_account_id"`
	DeployTargetIDs      []string          `json:"deploy_target_ids"`
	CSRPEM               string            `json:"csr_pem"`
	NotifyEvents         []string          `json:"notify_events"`
}

type importRequest struct {
	Domains         []string `json:"domains"`
	Certificate     string   `json:"certificate"`
	PrivateKey      string   `json:"private_key"`
	Verification    string   `json:"verification"`
	DeployTargetIDs []string `json:"deploy_target_ids"`
	PFX             string   `json:"pfx"`          // base64 编码的 PKCS#12 文件内容
	PFXPassword     string   `json:"pfx_password"` // PFX 密码
	NotifyEvents    []string `json:"notify_events"`
}

// notifyEventsFromStrings 字符串切片转通知事件切片
func notifyEventsFromStrings(in []string) []domain.NotificationEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.NotificationEvent, 0, len(in))
	for _, s := range in {
		out = append(out, domain.NotificationEvent(s))
	}
	return out
}

type CertificateHandler struct {
	certs    *application.CertificateService
	deploy   *application.DeployService
	cryptor  application.Cryptor
	audit    domain.AuditStore
}

func newCertificateHandler(certs *application.CertificateService, deploy *application.DeployService, cryptor application.Cryptor, audit domain.AuditStore) *CertificateHandler {
	return &CertificateHandler{certs: certs, deploy: deploy, cryptor: cryptor, audit: audit}
}

func (h *CertificateHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{id}", h.get)
	r.Get("/{id}/orders", h.orders)
	r.Get("/{id}/deploy-records", h.deployRecords)
	r.Get("/{id}/download", h.download)
	// 写操作：仅 admin/operator
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Post("/", h.issue)
		r.Post("/import", h.importCert)
		r.Post("/batch-renew", h.batchRenew)
		r.Post("/batch-deploy", h.batchDeploy)
		r.Post("/{id}/renew", h.renew)
		r.Post("/{id}/revoke", h.revoke)
		r.Put("/{id}/targets", h.updateTargets)
		r.Put("/{id}/notify-events", h.updateNotifyEvents)
		r.Post("/{id}/deploy", h.deployCert)
		r.Delete("/{id}", h.delete)
	})
	return r
}

type batchRequest struct {
	IDs      []string `json:"ids"`
	TargetID string   `json:"target_id"`
}

type batchFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type batchResult struct {
	Success int            `json:"success"`
	Failed  []batchFailure `json:"failed"`
}

const maxBatchSize = 20

// dedupIDs 去重并保持顺序
func dedupIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (h *CertificateHandler) batchRenew(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if !readJSON(w, r, &req) {
		return
	}
	ids := dedupIDs(req.IDs)
	if len(ids) == 0 {
		writeError(w, r, http.StatusBadRequest, "请选择至少一个证书")
		return
	}
	if len(ids) > maxBatchSize {
		writeError(w, r, http.StatusBadRequest, "单次批量操作最多 20 个证书")
		return
	}
	result := batchResult{Failed: []batchFailure{}}
	for _, id := range ids {
		if _, err := h.certs.Renew(r.Context(), id, nil, ""); err != nil {
			result.Failed = append(result.Failed, batchFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Success++
	}
	writeOK(w, r, result)
}

func (h *CertificateHandler) batchDeploy(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if !readJSON(w, r, &req) {
		return
	}
	ids := dedupIDs(req.IDs)
	if len(ids) == 0 {
		writeError(w, r, http.StatusBadRequest, "请选择至少一个证书")
		return
	}
	if len(ids) > maxBatchSize {
		writeError(w, r, http.StatusBadRequest, "单次批量操作最多 20 个证书")
		return
	}
	if req.TargetID == "" {
		writeError(w, r, http.StatusBadRequest, "请指定部署目标")
		return
	}
	// 校验目标存在
	targets, err := h.deploy.ListTargets(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	targetOK := false
	for _, t := range targets {
		if t.ID == req.TargetID {
			targetOK = true
			break
		}
	}
	if !targetOK {
		writeError(w, r, http.StatusNotFound, "部署目标不存在")
		return
	}
	result := batchResult{Failed: []batchFailure{}}
	for _, id := range ids {
		if err := h.deploy.DeployToTargets(r.Context(), id, []string{req.TargetID}); err != nil {
			result.Failed = append(result.Failed, batchFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Success++
	}
	writeOK(w, r, result)
}

type deployRequest struct {
	TargetIDs []string `json:"target_ids"`
}

// updateTargets 更新证书的部署目标绑定（签发后可随时增删改）
func (h *CertificateHandler) updateTargets(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.certs.UpdateTargets(r.Context(), chi.URLParam(r, "id"), req.DeployTargetIDs); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	cert, err := h.certs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toCertResponse(cert))
}

// updateNotifyEvents 更新证书的通知事件订阅（签发后可随时调整）
func (h *CertificateHandler) updateNotifyEvents(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	cert, err := h.certs.UpdateNotifyEvents(r.Context(), chi.URLParam(r, "id"), notifyEventsFromStrings(req.NotifyEvents))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toCertResponse(cert))
}

func (h *CertificateHandler) deployCert(w http.ResponseWriter, r *http.Request) {
	var req deployRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	// 未指定目标时部署证书绑定的全部目标
	if len(req.TargetIDs) == 0 {
		cert, err := h.certs.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, mapError(err), err.Error())
			return
		}
		req.TargetIDs = cert.DeployTargetIDs
	}
	if len(req.TargetIDs) == 0 {
		writeError(w, r, http.StatusBadRequest, "该证书未绑定部署目标")
		return
	}
	if err := h.deploy.DeployToTargets(r.Context(), chi.URLParam(r, "id"), req.TargetIDs); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *CertificateHandler) deployRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.deploy.Records(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]recordResponse, 0, len(records))
	for _, rec := range records {
		out = append(out, toRecordResponse(rec))
	}
	writeOK(w, r, out)
}

func (h *CertificateHandler) list(w http.ResponseWriter, r *http.Request) {
	certs, err := h.certs.List(r.Context())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]certResponse, 0, len(certs))
	for _, c := range certs {
		out = append(out, toCertResponse(c))
	}
	writeOK(w, r, out)
}

func (h *CertificateHandler) issue(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if !readJSON(w, r, &req) {
		return
	}
	cert, err := h.certs.Issue(r.Context(), application.IssueInput{
		Domains:              normalize(req.Domains),
		AccountID:            req.AccountID,
		ContactID:            req.ContactID,
		Verification:         domain.VerificationType(req.Verification),
		DNSConfig:            req.DNSConfig,
		DNSProviderAccountID: req.DNSProviderAccountID,
		DeployTargetIDs:      req.DeployTargetIDs,
		CSRPEM:               req.CSRPEM,
		NotifyEvents:         notifyEventsFromStrings(req.NotifyEvents),
	})
	if err != nil {
		status := mapError(err)
		// 签发失败但证书记录已建立（cert 非 nil），返回 422 携带证书信息（错误信封）
		if status == http.StatusInternalServerError && cert != nil {
			writeErrorWithData(w, r, http.StatusUnprocessableEntity, err.Error(), map[string]any{
				"certificate": toCertResponse(cert),
			})
			return
		}
		writeError(w, r, status, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, toCertResponse(cert))
}

func (h *CertificateHandler) get(w http.ResponseWriter, r *http.Request) {
	cert, err := h.certs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toCertResponse(cert))
}

func (h *CertificateHandler) orders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.certs.Orders(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]orderResponse, 0, len(orders))
	for _, o := range orders {
		out = append(out, toOrderResponse(o))
	}
	writeOK(w, r, out)
}

type renewRequest struct {
	DNSConfig            map[string]string `json:"dns_config"`
	DNSProviderAccountID string            `json:"dns_provider_account_id"`
}

func (h *CertificateHandler) renew(w http.ResponseWriter, r *http.Request) {
	var req renewRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	cert, err := h.certs.Renew(r.Context(), chi.URLParam(r, "id"), req.DNSConfig, req.DNSProviderAccountID)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, toCertResponse(cert))
}

func (h *CertificateHandler) revoke(w http.ResponseWriter, r *http.Request) {
	if err := h.certs.Revoke(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *CertificateHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.certs.Delete(r.Context(), id); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	// 清理该证书的部署记录
	if h.deploy != nil {
		_ = h.deploy.ClearRecords(r.Context(), id)
	}
	writeOK(w, r, map[string]bool{"ok": true})
}

func (h *CertificateHandler) importCert(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if !readJSON(w, r, &req) {
		return
	}
	var cert *domain.Certificate
	var err error
	if req.PFX != "" {
		// PFX 导入：多域名从证书 SAN 自动提取
		pfxData, derr := base64.StdEncoding.DecodeString(req.PFX)
		if derr != nil {
			writeError(w, r, http.StatusBadRequest, "PFX 内容不是有效的 base64")
			return
		}
		cert, err = h.certs.ImportPFX(r.Context(), pfxData, req.PFXPassword, domain.VerificationType(req.Verification), notifyEventsFromStrings(req.NotifyEvents))
	} else {
		cert, err = h.certs.Import(r.Context(), normalize(req.Domains), req.Certificate, req.PrivateKey,
			domain.VerificationType(req.Verification), notifyEventsFromStrings(req.NotifyEvents))
	}
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	if len(req.DeployTargetIDs) > 0 {
		cert.DeployTargetIDs = req.DeployTargetIDs
		if err := h.certs.UpdateTargets(r.Context(), cert.ID, req.DeployTargetIDs); err != nil {
			writeError(w, r, mapError(err), err.Error())
			return
		}
	}
	writeJSON(w, r, http.StatusCreated, toCertResponse(cert))
}

func (h *CertificateHandler) download(w http.ResponseWriter, r *http.Request) {
	cert, err := h.certs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	// format 优先（zip 打包）；未传时兼容旧 kind 单文件下载
	if format := r.URL.Query().Get("format"); format != "" {
		h.downloadFormat(w, r, cert, format)
		return
	}
	h.downloadLegacy(w, r, cert, r.URL.Query().Get("kind"))
}

// downloadFormat 按服务器格式打包 zip 下载。含私钥的格式需操作权限 + 审计。
func (h *CertificateHandler) downloadFormat(w http.ResponseWriter, r *http.Request, cert *domain.Certificate, format string) {
	if format == "jks" || format == "root" {
		writeError(w, r, http.StatusNotImplemented, "该格式即将支持")
		return
	}
	priv := ""
	if cert.PrivateKeyEnc != "" {
		if dec, derr := h.cryptor.DecryptString(cert.PrivateKeyEnc); derr == nil {
			priv = dec
		}
	}
	if priv == "" {
		writeError(w, r, http.StatusNotFound, "无私钥")
		return
	}
	if !h.authorizeKeyAccess(w, r, cert, "format="+format) {
		return
	}

	var files []zipFile
	switch format {
	case "nginx", "other":
		leafName := "fullchain.pem"
		if format == "other" {
			leafName = "cert.pem"
		}
		files = []zipFile{
			{Name: leafName, Data: []byte(cert.CertificatePEM)},
			{Name: "privkey.pem", Data: []byte(priv)},
		}
	case "apache":
		leaf, _, err := deploy.SplitChain(cert.CertificatePEM)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "证书解析失败")
			return
		}
		files = []zipFile{
			{Name: "cert.crt", Data: []byte(leaf)},
			{Name: "privkey.key", Data: []byte(priv)},
		}
	case "iis", "tomcat":
		password, err := randomPassword(16)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "生成密码失败")
			return
		}
		pfx, err := deploy.BuildPFX(cert.CertificatePEM, priv, password)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "合成 PFX 失败")
			return
		}
		files = []zipFile{
			{Name: "cert.pfx", Data: pfx},
			{Name: "password.txt", Data: []byte(password)},
		}
	default:
		writeError(w, r, http.StatusBadRequest, "不支持的格式")
		return
	}

	data, err := buildZip(files)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "打包失败")
		return
	}
	filename := cert.PrimaryDomain + "-" + format + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	_, _ = w.Write(data)
}

// downloadLegacy 兼容旧 ?kind= 单文件下载
func (h *CertificateHandler) downloadLegacy(w http.ResponseWriter, r *http.Request, cert *domain.Certificate, kind string) {
	var body string
	var filename, contentType string
	sensitive := false
	switch kind {
	case "key":
		sensitive = true
		if cert.PrivateKeyEnc == "" {
			writeError(w, r, http.StatusNotFound, "无私钥")
			return
		}
		priv, err := h.cryptor.DecryptString(cert.PrivateKeyEnc)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "私钥解密失败")
			return
		}
		body = priv
		filename = cert.PrimaryDomain + ".key"
		contentType = "application/x-pem-file"
	case "pem":
		body = cert.CertificatePEM
		filename = cert.PrimaryDomain + ".pem"
		contentType = "application/x-pem-file"
	case "fullchain":
		sensitive = true // 含私钥
		priv := ""
		if cert.PrivateKeyEnc != "" {
			if dec, err := h.cryptor.DecryptString(cert.PrivateKeyEnc); err == nil {
				priv = dec
			}
		}
		body = cert.CertificatePEM + "\n" + priv
		filename = cert.PrimaryDomain + "-bundle.pem"
		contentType = "application/x-pem-file"
	default:
		body = cert.CertificatePEM
		filename = cert.PrimaryDomain + ".crt"
		contentType = "application/x-x509-ca-cert"
	}
	// 含私钥的下载：需要操作权限 + 审计（07-auth §7.6）
	if sensitive {
		if !h.authorizeKeyAccess(w, r, cert, "kind="+kind) {
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	_, _ = w.Write([]byte(body))
}

// authorizeKeyAccess 校验含私钥下载权限并写审计；通过返回 true
func (h *CertificateHandler) authorizeKeyAccess(w http.ResponseWriter, r *http.Request, cert *domain.Certificate, detail string) bool {
	p := userFrom(r.Context())
	if p == nil || p.User.Role == domain.RoleReadonly {
		writeError(w, r, http.StatusForbidden, "无权限下载私钥")
		return false
	}
	_ = h.audit.Append(r.Context(), &domain.AuditLog{
		UserID:    p.User.ID,
		Username:  p.User.Username,
		Action:    domain.AuditCertKeyAccess,
		Resource:  cert.PrimaryDomain,
		Detail:    detail,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		CreatedAt: time.Now(),
	})
	return true
}

func normalize(d []string) []string {
	out := make([]string, 0, len(d))
	for _, x := range d {
		x = strings.TrimSpace(strings.ToLower(x))
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
