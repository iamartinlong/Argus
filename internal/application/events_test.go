package application

import (
	"sync"
	"testing"
)

func TestEventBusPublishSubscribe(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	got := map[string]int{}
	unsub := bus.Subscribe(EventCertificateRenewed, func(e Event) {
		mu.Lock()
		got[e.Name]++
		mu.Unlock()
	})
	bus.Subscribe(EventCertificateIssued, func(e Event) {
		mu.Lock()
		got[e.Name]++
		mu.Unlock()
	})

	bus.Publish(EventCertificateRenewed, "cert-1")
	bus.Publish(EventCertificateRenewed, "cert-2")
	bus.Publish(EventCertificateIssued, "cert-3")

	mu.Lock()
	if got[EventCertificateRenewed] != 2 {
		t.Fatalf("expected 2 renewed events, got %d", got[EventCertificateRenewed])
	}
	if got[EventCertificateIssued] != 1 {
		t.Fatalf("expected 1 issued event, got %d", got[EventCertificateIssued])
	}
	mu.Unlock()

	// 取消订阅后不再收到
	unsub()
	bus.Publish(EventCertificateRenewed, "cert-4")
	mu.Lock()
	if got[EventCertificateRenewed] != 2 {
		t.Fatalf("expected still 2 after unsubscribe, got %d", got[EventCertificateRenewed])
	}
	mu.Unlock()
}

func TestIssuePublishesEvent(t *testing.T) {
	svc, _, _, accounts := newTestCertService(nil)
	seedAccount(t, accounts, "acme")

	bus := svc.bus
	var mu sync.Mutex
	renewed := 0
	bus.Subscribe(EventCertificateRenewed, func(e Event) {
		mu.Lock()
		renewed++
		mu.Unlock()
	})

	cert, err := svc.Issue(t.Context(), IssueInput{
		Domains: []string{"event.example.com"}, AccountID: "acc-1", Verification: "http01",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cert
	mu.Lock()
	if renewed != 0 {
		t.Fatalf("issue should not emit renewed event, got %d", renewed)
	}
	mu.Unlock()

	if _, err := svc.Renew(t.Context(), cert.ID, nil, ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if renewed != 1 {
		t.Fatalf("renew should emit 1 renewed event, got %d", renewed)
	}
	mu.Unlock()
}
