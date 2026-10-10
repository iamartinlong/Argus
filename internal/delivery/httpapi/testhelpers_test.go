package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// decodeData 解信封 data 层并断言成功（code==0）
func decodeData(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	var env struct {
		Code any            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, rec.Body.String())
	}
	if code, ok := env.Code.(float64); !ok || code != 0 {
		t.Fatalf("unexpected code %v (status %d): %s", env.Code, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		t.Fatalf("decode data: %v (%s)", err, rec.Body.String())
	}
}

// decodeErr 解错误信封，返回 (code, message)
func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
	}
	decodeData(t, rec, &env)
	return env.Code, env.Message
}

// decodePage 解分页信封，返回 data 层原始字节
func decodePage(t *testing.T, rec *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	var env struct {
		Code any            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode page envelope: %v (%s)", err, rec.Body.String())
	}
	return env.Data
}
