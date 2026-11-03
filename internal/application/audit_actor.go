package application

import (
	"context"

	"github.com/iamartinlong/Argus/internal/domain"
)

// resolveActor 解析审计操作者：显式 actor 优先，否则取上下文中的
// （HTTP 中间件 / MCP 传输在认证后已注入），都没有则 nil。
//
// nil 的语义是"无人格触发源"（调度器、CLI 本地模式），审计记 "system"。
// 在 T11 之前，Certificate/Account/Contact/Deploy 四个服务的 auditAction
// 一律收 nil 参数，导致 REST 与 MCP 通道的操作全部记成 "system"，丢失归属。
func resolveActor(ctx context.Context, explicit *domain.User) *domain.User {
	if explicit != nil {
		return explicit
	}
	return domain.ActorFrom(ctx)
}

// auditFields 由操作者生成审计的 UserID/Username（nil → system）。
func auditFields(actor *domain.User) (userID, username string) {
	if actor != nil {
		return actor.ID, actor.Username
	}
	return "", "system"
}
