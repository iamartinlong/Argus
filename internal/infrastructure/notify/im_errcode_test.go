package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// TestWeComErrcodeDetected 企微返回 HTTP 200 + errcode!=0 时必须报错（此前误报成功）
func TestWeComErrcodeDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":40008,"errmsg":"invalid message type"}`))
	}))
	defer server.Close()

	ch := NewWeCom(server.URL, "")
	err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected error for errcode!=0")
	}
	if err.Error() != "企业微信返回 errcode=40008: invalid message type" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWeComSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	ch := NewWeCom(server.URL, "")
	if err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDingTalkErrcodeDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"keywords not in content"}`))
	}))
	defer server.Close()

	ch := NewDingTalk(server.URL, "")
	err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"})
	if err == nil || err.Error() != "钉钉返回 errcode=310000: keywords not in content" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFeishuCodeDetected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":19001,"msg":"invalid webhook url"}`))
	}))
	defer server.Close()

	ch := NewFeishu(server.URL)
	err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"})
	if err == nil || err.Error() != "飞书返回 code=19001: invalid webhook url" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestIMNonStandardResponse 非标准响应（自建端点）以 HTTP 状态为准，不误报
func TestIMNonStandardResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ch := NewWeCom(server.URL, "")
	if err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("non-standard response should pass: %v", err)
	}
}

