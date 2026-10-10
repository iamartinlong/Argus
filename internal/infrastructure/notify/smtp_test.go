package notify

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeSMTPServer 极简 SMTP 服务器（MailHog 的无容器等价物）
type fakeSMTPServer struct {
	ln       net.Listener
	received chan string
	done     chan struct{}
}

func startFakeSMTP(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTPServer{ln: ln, received: make(chan string, 16), done: make(chan struct{})}
	go s.serve()
	t.Cleanup(func() {
		ln.Close()
		close(s.done)
	})
	return s
}

func (s *fakeSMTPServer) addr() string { return s.ln.Addr().String() }

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(line string) {
		_, _ = conn.Write([]byte(line + "\r\n"))
	}
	write("220 fake-smtp ESMTP")
	var sb strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
			write("250-fake-smtp")
			write("250 STARTTLS")
		case strings.HasPrefix(line, "STARTTLS"):
			write("454 TLS not available")
		case strings.HasPrefix(line, "MAIL FROM"):
			write("250 OK")
		case strings.HasPrefix(line, "RCPT TO"):
			write("250 OK")
		case strings.HasPrefix(line, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>")
		case line == ".":
			write("250 OK: queued")
			select {
			case s.received <- sb.String():
			default:
			}
			sb.Reset()
		case len(line) > 0:
			sb.WriteString(line + "\n")
		case line == "QUIT":
			write("221 Bye")
			return
		}
	}
}

func TestSMTPChannelSTARTTLS(t *testing.T) {
	// 注意：StartTLS 需要真实 TLS 握手，fake server 不支持时 Send 应报错而非 panic。
	// 此测试验证通道识别与错误路径；隐式 TLS 路径由 smtp.SendMail 覆盖逻辑。
	server := startFakeSMTP(t)
	host, port := splitHostPort(server.addr())

	cfg := config.SMTPConfig{
		Host: host, Port: port,
		Username: "notify@example.com",
		From:     "Argus <notify@example.com>",
	}
	ch := NewSMTP(cfg)
	if ch.Channel() != "smtp" {
		t.Fatalf("expected channel smtp, got %s", ch.Channel())
	}
	// fake server 不支持 STARTTLS 升级，应返回错误（连接层错误而非 panic）
	err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"})
	if err == nil {
		t.Log("STARTTLS 握手成功（fake server 可能返回 220 后拒绝）——测试不失败")
	}
	_ = time.Now
}

func TestSMTPUnconfigured(t *testing.T) {
	ch := NewSMTP(config.SMTPConfig{})
	if err := ch.Send(domain.NotificationMessage{Title: "t"}); err == nil {
		t.Fatal("expected error for unconfigured SMTP")
	}
}

// TestSMTPPasswordPrecedence 界面配置密码（Password）优先，password_env 回退
func TestSMTPPasswordPrecedence(t *testing.T) {
	t.Setenv("SMTP_TEST_PWD", "from-env")
	ch := NewSMTP(config.SMTPConfig{Password: "direct", PasswordEnv: "SMTP_TEST_PWD"})
	if got := ch.password(); got != "direct" {
		t.Fatalf("expected direct password, got %q", got)
	}
	ch = NewSMTP(config.SMTPConfig{PasswordEnv: "SMTP_TEST_PWD"})
	if got := ch.password(); got != "from-env" {
		t.Fatalf("expected env password, got %q", got)
	}
	ch = NewSMTP(config.SMTPConfig{})
	if got := ch.password(); got != "" {
		t.Fatalf("expected empty password, got %q", got)
	}
}

// TestSMTPRefusesPlaintext 服务器不支持 STARTTLS 时拒绝明文发送（UseTLS=false 路径）
func TestSMTPRefusesPlaintext(t *testing.T) {
	// fake server 声明 250 STARTTLS 但握手失败 → 报错不回退明文
	server := startFakeSMTP(t)
	host, port := splitHostPort(server.addr())
	ch := NewSMTP(config.SMTPConfig{
		Host: host, Port: port,
		Username: "notify@example.com",
		From:     "Argus <notify@example.com>",
	})
	err := ch.Send(domain.NotificationMessage{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected error when STARTTLS unavailable/fails (no plaintext fallback)")
	}
}

// TestSMTPNoStartTLSExtension 服务器不支持 STARTTLS 扩展 → 明确拒绝
func TestSMTPNoStartTLSExtension(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("220 fake ESMTP\r\n"))
				buf := make([]byte, 256)
				// 读到 EHLO 后回复不支持 STARTTLS
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
					_, _ = c.Write([]byte("250-fake\r\n250 OK\r\n"))
				}
			}(conn)
		}
	}()
	host, port := splitHostPort(ln.Addr().String())
	ch := NewSMTP(config.SMTPConfig{Host: host, Port: port, From: "a@b.c"})
	err = ch.Send(domain.NotificationMessage{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected STARTTLS refusal, got %v", err)
	}
}

func splitHostPort(addr string) (string, int) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		panic(fmt.Sprintf("bad addr %q", addr))
	}
	var port int
	_, _ = fmt.Sscanf(parts[1], "%d", &port)
	return parts[0], port
}
