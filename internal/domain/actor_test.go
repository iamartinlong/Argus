package domain

import (
	"context"
	"testing"
)

// WithActor / ActorFrom 往返：注入后能取回同一用户
func TestWithActorRoundTrip(t *testing.T) {
	ctx := context.Background()
	if ActorFrom(ctx) != nil {
		t.Fatal("未注入时应为 nil")
	}
	u := &User{ID: "u1", Username: "alice", Role: RoleAdmin}
	got := ActorFrom(WithActor(ctx, u))
	if got == nil || got.ID != "u1" || got.Username != "alice" {
		t.Fatalf("round-trip 失败: %+v", got)
	}
}

// 显式注入 nil 与"未注入"等价（都取不到 actor）
func TestWithActorNil(t *testing.T) {
	if ActorFrom(WithActor(context.Background(), nil)) != nil {
		t.Fatal("注入 nil 应读回 nil")
	}
}

// 派生上下文不丢失 actor（context.WithValue 的继承性）
func TestActorInheritedByChildContext(t *testing.T) {
	parent := WithActor(context.Background(), &User{ID: "u1", Username: "bob"})
	child, cancel := context.WithCancel(parent)
	defer cancel()
	if ActorFrom(child) == nil {
		t.Fatal("子上下文应继承 actor")
	}
}

// 键不冲突：其他包用相同 string 键不会串
func TestActorKeyIsPrivate(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "x")
	if ActorFrom(ctx) != nil {
		t.Fatal("不应读到其他包的 context 值")
	}
}
