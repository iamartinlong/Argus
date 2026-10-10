package application

import (
	"sync"

	"github.com/iamartinlong/Argus/internal/domain"
)

// Event 领域事件（事件总线载荷）
type Event struct {
	Name    string
	Payload any
}

// EventName 事件名常量
const (
	EventCertificateIssued    = "certificate.issued"
	EventCertificateIssueFail = "certificate.issue.failed"
	EventCertificateRenewed   = "certificate.renewed"
	EventCertificateRenewFail = "certificate.renew.failed"
	EventCertificateRevoked   = "certificate.revoked"
	EventDeploySuccess        = "deploy.success"
	EventDeployFailed         = "deploy.failed"
)

// DeployEvent 部署事件载荷（供通知订阅使用）
type DeployEvent struct {
	Cert       *domain.Certificate
	TargetName string
	Error      string
}

type subscriber struct {
	id      int
	handler func(Event)
}

// EventBus 轻量进程内事件总线：签发/续期等完成后发布事件，
// 部署触发器（M4）、通知等通过订阅解耦联动。
type EventBus struct {
	mu      sync.RWMutex
	subs    map[string][]subscriber
	nextID  int
}

func NewEventBus() *EventBus {
	return &EventBus{subs: map[string][]subscriber{}}
}

func (b *EventBus) Publish(name string, payload any) {
	b.mu.RLock()
	handlers := make([]func(Event), 0, len(b.subs[name]))
	for _, s := range b.subs[name] {
		handlers = append(handlers, s.handler)
	}
	b.mu.RUnlock()
	e := Event{Name: name, Payload: payload}
	for _, h := range handlers {
		h(e)
	}
}

// Subscribe 订阅事件；返回取消订阅函数
func (b *EventBus) Subscribe(name string, handler func(Event)) func() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.subs[name] = append(b.subs[name], subscriber{id: id, handler: handler})
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[name]
		for i, s := range subs {
			if s.id == id {
				b.subs[name] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}
