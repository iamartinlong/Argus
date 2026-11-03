package domain

import "context"

// actorCtxKey 上下文中当前操作者的键（私有，避免外部误用）。
type actorCtxKey struct{}

// WithActor 把当前操作者注入上下文。
//
// 用途：审计归属。application 层服务的 auditAction 在拿不到显式 actor 时
// 回退到上下文中的操作者——delivery 层（HTTP 中间件 / MCP 传输）在认证成功后
// 调用本函数，从而让 REST 与 MCP 两个通道的证书/账户/联系人/部署操作都能
// 记录到真实用户，而不是一律 "system"。
//
// 无人格触发源（调度器、CLI 本地模式）不注入，此时审计仍记 "system"。
func WithActor(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, u)
}

// ActorFrom 取出上下文中的操作者；无则返回 nil。
func ActorFrom(ctx context.Context) *User {
	u, _ := ctx.Value(actorCtxKey{}).(*User)
	return u
}
