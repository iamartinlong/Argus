package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// envelope 统一响应信封：成功 code=0，错误 code 为字符串错误码
type envelope struct {
	Code      any    `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
}

func newEnvelope(code any, message string, r *http.Request, data any) envelope {
	return envelope{
		Code:      code,
		Message:   message,
		TraceID:   middleware.GetReqID(r.Context()),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
}

// writeJSON 统一 JSON 出口：全部包信封（除豁免端点走 writeRawJSON）
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(newEnvelope(0, "ok", r, v))
}

// writeRawJSON 裸 JSON（豁免端点：agent 出站协议、healthz）
func writeRawJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// statusToCode HTTP 状态 → 结构化错误码
func statusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_argument"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "unprocessable"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusNotImplemented:
		return "not_implemented"
	default:
		return "internal_error"
	}
}

// writeError 错误响应（信封 + 5xx 自动落日志）
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeErrorWithData(w, r, status, msg, nil)
}

// writeErrorWithData 错误响应（携带业务数据，如 422 签发失败的 certificate）。
// 5xx 不透出内部错误细节：完整原因落服务端日志（含 trace_id 便于与响应关联），
// 客户端仅见通用文案；4xx 维持原有具体文案。
func writeErrorWithData(w http.ResponseWriter, r *http.Request, status int, msg string, data any) {
	if status >= 500 {
		log.Printf("API %d %s %s trace=%s: %s", status, r.Method, r.URL.Path,
			middleware.GetReqID(r.Context()), msg)
		msg = "服务器内部错误"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(newEnvelope(statusToCode(status), msg, r, data))
}

// writeRawError 裸错误响应（豁免端点）
func writeRawError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeOK(w http.ResponseWriter, r *http.Request, v any) {
	writeJSON(w, r, http.StatusOK, v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	// 请求体上限 1MB（防内存耗尽）
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return false
	}
	return true
}

// decodeOptionalJSON 允许空请求体（用于可选 body 的请求）
func decodeOptionalJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && err.Error() != "EOF" {
		return err
	}
	return nil
}
