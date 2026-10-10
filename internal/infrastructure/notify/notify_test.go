package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
)

func TestWebhookChannel(t *testing.T) {
	var received map[string]any
	var signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		signature = r.Header.Get("X-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := NewWebhook(server.URL, "secret-key")
	if err := hook.Send(domain.NotificationMessage{
		Title: "证书即将到期：example.com",
		Body:  "7 天后到期",
		Details: map[string]string{"domain": "example.com", "days_left": "7"},
	}); err != nil {
		t.Fatalf("webhook send: %v", err)
	}
	if received["title"] != "证书即将到期：example.com" {
		t.Fatalf("unexpected payload: %v", received)
	}
	if signature == "" {
		t.Fatal("expected X-Signature header")
	}
	details, _ := received["details"].(map[string]any)
	if details["domain"] != "example.com" {
		t.Fatalf("unexpected details: %v", received)
	}
}

func TestWebhookErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	hook := NewWebhook(server.URL, "")
	if err := hook.Send(domain.NotificationMessage{Title: "t"}); err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestWeComPayload(t *testing.T) {
	var payload struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := NewWeCom(server.URL, "key").Send(domain.NotificationMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if payload.MsgType != "markdown" || payload.Markdown.Content == "" {
		t.Fatalf("unexpected wecom payload: %+v", payload)
	}
}

func TestDingTalkSignedURL(t *testing.T) {
	var target string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target = r.URL.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := NewDingTalk(server.URL, "ding-secret")
	if err := hook.Send(domain.NotificationMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if target == "" || !containsStr(target, "timestamp=") || !containsStr(target, "sign=") {
		t.Fatalf("expected signed URL, got %q", target)
	}
}

func TestFeishuPayload(t *testing.T) {
	var payload struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := NewFeishu(server.URL).Send(domain.NotificationMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if payload.MsgType != "text" || payload.Content.Text == "" {
		t.Fatalf("unexpected feishu payload: %+v", payload)
	}
}

func TestRegistryFiltering(t *testing.T) {
	cfg := config.NotifyConfig{
		Webhook: config.WebhookConfig{URL: "http://example.com/hook"},
		IM: config.IMConfig{
			WeCom: config.IMWebhook{WebhookURL: "http://example.com/wecom"},
		},
	}
	r := NewRegistry(cfg)
	if _, ok := r.Get("webhook"); !ok {
		t.Fatal("webhook should be registered")
	}
	if _, ok := r.Get("wecom"); !ok {
		t.Fatal("wecom should be registered")
	}
	if _, ok := r.Get("dingtalk"); ok {
		t.Fatal("dingtalk should NOT be registered (unconfigured)")
	}
	if _, ok := r.Get("smtp"); ok {
		t.Fatal("smtp should NOT be registered (unconfigured)")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
