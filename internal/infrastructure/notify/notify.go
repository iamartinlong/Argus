package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
)

// Notifier 通知渠道
type Notifier interface {
	Channel() string
	Send(msg domain.NotificationMessage) error
}

// Registry 渠道注册表
type Registry struct {
	channels map[string]domain.MessageSender
}

func NewRegistry(cfg config.NotifyConfig) *Registry {
	r := &Registry{channels: map[string]domain.MessageSender{}}
	if cfg.SMTP.Host != "" {
		r.channels["smtp"] = NewSMTP(cfg.SMTP)
	}
	if cfg.Webhook.URL != "" {
		r.channels["webhook"] = NewWebhook(cfg.Webhook.URL, cfg.Webhook.Secret)
	}
	if cfg.IM.WeCom.WebhookURL != "" {
		r.channels["wecom"] = NewWeCom(cfg.IM.WeCom.WebhookURL, cfg.IM.WeCom.Key)
	}
	if cfg.IM.DingTalk.WebhookURL != "" {
		r.channels["dingtalk"] = NewDingTalk(cfg.IM.DingTalk.WebhookURL, cfg.IM.DingTalk.Key)
	}
	if cfg.IM.Feishu.WebhookURL != "" {
		r.channels["feishu"] = NewFeishu(cfg.IM.Feishu.WebhookURL)
	}
	return r
}

func (r *Registry) Get(name string) (domain.MessageSender, bool) {
	n, ok := r.channels[name]
	return n, ok
}

// ---------------- SMTP ----------------

type SMTP struct {
	cfg config.SMTPConfig
}

func NewSMTP(cfg config.SMTPConfig) *SMTP { return &SMTP{cfg: cfg} }

func (s *SMTP) Channel() string { return "smtp" }

func (s *SMTP) Send(msg domain.NotificationMessage) error {
	if s.cfg.From == "" || s.cfg.Host == "" {
		return fmt.Errorf("SMTP 未配置")
	}
	to := s.cfg.Username
	body := msg.Body
	if len(msg.Details) > 0 {
		var sb strings.Builder
		sb.WriteString(msg.Body)
		sb.WriteString("\n\n")
		for k, v := range msg.Details {
			sb.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
		body = sb.String()
	}
	content := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, to, msg.Title, body)

	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.password(), s.cfg.Host)
	}
	if s.cfg.UseTLS {
		// 隐式 TLS（465）：smtp.SendMail 依赖服务端 STARTTLS 支持，
		// 无法保证强制加密。改为显式 tls.Dial 连接，杜绝明文降级。
		return s.sendImplicitTLS(addr, auth, to, content)
	}
	// STARTTLS（显式升级，失败即报错，不回退明文）
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("SMTP 服务器不支持 STARTTLS，拒绝明文发送")
	}
	if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
		return err
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return w.Close()
}

// sendImplicitTLS 显式 TLS 直连（465），强制加密
func (s *SMTP) sendImplicitTLS(addr string, auth smtp.Auth, to, content string) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.cfg.Host})
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return w.Close()
}

func (s *SMTP) password() string {
	// 界面配置的密码优先；兼容 config.yaml 的 password_env 环境变量引用
	if s.cfg.Password != "" {
		return s.cfg.Password
	}
	if s.cfg.PasswordEnv != "" {
		if v := os.Getenv(s.cfg.PasswordEnv); v != "" {
			return v
		}
	}
	return ""
}

// ---------------- Webhook ----------------

type Webhook struct {
	url    string
	secret string
	client *http.Client
}

func NewWebhook(url, secret string) *Webhook {
	return &Webhook{url: url, secret: secret, client: &http.Client{Timeout: 15 * time.Second}}
}

func (w *Webhook) Channel() string { return "webhook" }

func (w *Webhook) Send(msg domain.NotificationMessage) error {
	payload := map[string]any{
		"event":   msg.Title,
		"title":   msg.Title,
		"body":    msg.Body,
		"details": msg.Details,
		"time":    time.Now().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, w.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		mac.Write(data)
		req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook 返回 %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ---------------- 企业微信机器人 ----------------

type WeCom struct {
	url string
	key string
}

func NewWeCom(url, key string) *WeCom { return &WeCom{url: url, key: key} }

func (w *WeCom) Channel() string { return "wecom" }

func (w *WeCom) Send(msg domain.NotificationMessage) error {
	text := msg.Body
	if len(msg.Details) > 0 {
		for k, v := range msg.Details {
			text += fmt.Sprintf("\n>%s: %s", k, v)
		}
	}
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": fmt.Sprintf("### %s\n%s", msg.Title, text),
		},
	}
	return postJSONChecked(w.url, payload, func(body []byte) error {
		var r struct {
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil // 非标准响应（自建端点），以 HTTP 状态为准
		}
		if r.Errcode != 0 {
			return fmt.Errorf("企业微信返回 errcode=%d: %s", r.Errcode, r.Errmsg)
		}
		return nil
	})
}

// ---------------- 钉钉机器人 ----------------

type DingTalk struct {
	url    string
	secret string
}

func NewDingTalk(url, secret string) *DingTalk { return &DingTalk{url: url, secret: secret} }

func (d *DingTalk) Channel() string { return "dingtalk" }

func (d *DingTalk) Send(msg domain.NotificationMessage) error {
	timestamp := time.Now().UnixMilli()
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": msg.Title,
			"text":  fmt.Sprintf("### %s\n%s", msg.Title, msg.Body),
		},
	}
	target := d.url
	if d.secret != "" {
		strToSign := fmt.Sprintf("%d\n%s", timestamp, d.secret)
		mac := hmac.New(sha256.New, []byte(d.secret))
		mac.Write([]byte(strToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		sep := "?"
		if strings.Contains(d.url, "?") {
			sep = "&"
		}
		target = fmt.Sprintf("%s%stimestamp=%d&sign=%s", d.url, sep, timestamp, sign)
	}
	return postJSONChecked(target, payload, func(body []byte) error {
		var r struct {
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil
		}
		if r.Errcode != 0 {
			return fmt.Errorf("钉钉返回 errcode=%d: %s", r.Errcode, r.Errmsg)
		}
		return nil
	})
}

// ---------------- 飞书机器人 ----------------

type Feishu struct {
	url string
}

func NewFeishu(url string) *Feishu { return &Feishu{url: url} }

func (f *Feishu) Channel() string { return "feishu" }

func (f *Feishu) Send(msg domain.NotificationMessage) error {
	payload := map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": fmt.Sprintf("%s\n%s", msg.Title, msg.Body),
		},
	}
	return postJSONChecked(f.url, payload, func(body []byte) error {
		var r struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil
		}
		if r.Code != 0 {
			return fmt.Errorf("飞书返回 code=%d: %s", r.Code, r.Msg)
		}
		return nil
	})
}

// ---------------- 公共 ----------------

// httpClient IM 机器人共用客户端（15s 超时，避免发送挂起）
var httpClient = &http.Client{Timeout: 15 * time.Second}

// postJSONChecked 发送并校验：HTTP >=400 或响应体 errcode/code 非 0 视为失败
func postJSONChecked(url string, payload any, check func(body []byte) error) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	if check != nil {
		if err := check(body); err != nil {
			return err
		}
	}
	return nil
}

func postJSON(url string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
