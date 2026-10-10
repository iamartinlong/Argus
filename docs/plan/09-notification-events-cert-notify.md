# 实施计划：新增签发/部署通知事件 + 证书级通知事件勾选

## 背景与目标

- 当前通知规则仅支持 3 种事件：`cert_expiring`（到期提醒）、`renew_success`（续期成功）、`renew_failed`（续期失败）。
- 签发（issue）与部署（deploy）成功/失败不会产生任何通知；签发失败当前复用 `renew_failed` 事件（语义混淆）。
- 目标：
  1. 新增 4 种通知事件：`issue_success`、`issue_failed`、`deploy_success`、`deploy_failed`。
  2. 在签发/导入表单中提供「通知事件」复选框，允许按证书粒度选择订阅哪些事件。
  3. 证书详情页支持后续修改勾选。

## 设计决策

| # | 决策 | 理由 |
|---|---|---|
| N1 | 新增 4 种事件：issue_success / issue_failed / deploy_success / deploy_failed | 与现有 3 种共存；签发失败改用独立 `issue_failed`，不再复用 `renew_failed` |
| N2 | 证书级过滤：`certificate.notify_events` 非空则仅对勾选事件发通知；为空（旧证书）保持现状全部通知 | 向后兼容，默认全选即与现状等价 |
| N3 | 过滤作用于全部 7 种事件（含到期提醒、续期） | 一处配置全场景生效 |
| N4 | 通知通过 EventBus 事件订阅解耦（与 DeployService 订阅模式一致） | 不新增服务间直接依赖，保持 Clean Architecture |
| N5 | Agent 异步部署回执完成时也发布 deploy 事件 | 保证 agent 通道部署成功/失败同样可通知 |
| N6 | 新增 `PUT /certificates/{id}/notify-events` 支持详情页修改 | 避免订阅调整需删除重发 |

## 数据模型

`certificates` 表新增列（TEXT，JSON 数组）：

```sql
ALTER TABLE certificates ADD COLUMN notify_events TEXT NOT NULL DEFAULT '[]';
```

- sqlite 迁移 id：15（`internal/infrastructure/storage/sqlite/db.go`）
- postgres 迁移 id：15（`internal/infrastructure/storage/postgres/migrate.go`）

## 改动清单

### 后端

| 文件 | 改动 |
|---|---|
| `internal/domain/notification.go` | 新增 4 个事件常量 + 更新 `Valid()` |
| `internal/domain/certificate.go` | `Certificate` 增加 `NotifyEvents []NotificationEvent` |
| `internal/application/events.go` | 新增 `certificate.issue.failed`、`deploy.success`、`deploy.failed` 事件名；定义 `DeployEvent{Cert, TargetName, Error}` 载荷 |
| `internal/application/certificate_service.go` | `IssueInput`/`Import`/`ImportPFX` 携带 `NotifyEvents`（校验合法事件）；签发失败两处改发 `issue.failed` |
| `internal/application/deploy_service.go` | `executeDeploy` 成功/失败发布 deploy 事件；新增 `UpdateNotifyEvents` 服务方法 |
| `internal/application/agent_service.go` | 构造器加 `bus *EventBus`；`SubmitResult` 部署记录转终态时发布 deploy 事件 |
| `internal/application/notification_service.go` | `NotifyEvent`/`NotifyExpiry` 加证书级过滤；`defaultTemplate` 补 4 分支（含 `{target}` 变量）；新增 `AttachEventBus` 订阅 |
| `cmd/argus/cmd/serve.go` | notifySvc 调 `AttachEventBus(eventBus)`；AgentService 构造器传 bus |
| `internal/infrastructure/storage/sqlite/db.go` | 迁移 id 15 |
| `internal/infrastructure/storage/postgres/migrate.go` | 迁移 id 15 |
| `internal/infrastructure/storage/sqlite/certificate_store.go` | `certColumns`/`scan`/`Create`/`Update` 增补列 |
| `internal/delivery/httpapi/certificate_handler.go` | `issueRequest`/`importRequest` 加 `notify_events`；`certResponse` 回传；新增 `PUT /{id}/notify-events` |
| `docs/openapi.yaml` | 通知规则 event 枚举 3 处；证书签发/导入/Certificate schema 补 `notify_events` |

### 前端

| 文件 | 改动 |
|---|---|
| `web/src/lib/types.ts` | `Certificate.notify_events: string[]` |
| `web/src/features/notifications/notification-rules-page.tsx` | `eventNames`/zod enum/事件下拉/模板列表补 4 事件 |
| `web/src/features/certificates/issue-dialog.tsx` | 「通知事件」复选框组（7 项，默认全选），随 body 提交 |
| `web/src/features/certificates/import-dialog.tsx` | 「通知事件」复选框组（5 项，默认全选，不含签发事件） |
| `web/src/features/certificates/certificate-detail-page.tsx` | 通知事件编辑复选框组（`PUT /certificates/{id}/notify-events`） |

## 关键实现细节

### 事件总线事件名

```go
EventCertificateIssued    = "certificate.issued"        // 签发成功（现有，沿用）
EventCertificateIssueFail = "certificate.issue.failed"  // 新增：签发失败
EventDeploySuccess        = "deploy.success"            // 新增
EventDeployFailed         = "deploy.failed"             // 新增
```

### 通知过滤逻辑（`notification_service.go`）

```go
func (s *NotificationService) eventSubscribed(cert *domain.Certificate, event domain.NotificationEvent) bool {
    if len(cert.NotifyEvents) == 0 {
        return true // 旧证书未配置：保持现状全部通知
    }
    for _, e := range cert.NotifyEvents {
        if e == event {
            return true
        }
    }
    return false
}
```

在 `NotifyEvent` 与 `NotifyExpiry` 开头调用，`!eventSubscribed` 则直接返回。

### 默认文案（defaultTemplate 新增分支）

| 事件 | 标题 | 正文 |
|---|---|---|
| issue_success | `证书签发成功：{domain}` | 证书已成功签发，有效期至 {expires} |
| issue_failed | `证书签发失败：{domain}` | 签发失败，错误：{error} |
| deploy_success | `证书部署成功：{domain}` | 已成功部署到 {target} |
| deploy_failed | `证书部署失败：{domain}` | 部署到 {target} 失败，错误：{error} |

## 验证

- 后端：`go build ./...`、`go vet ./...`、`go test ./...` — 已通过
- 前端：`npm run lint`（改动文件 0 错误）、`npm run test`（21 用例通过）、`npm run build`（在 `web/` 下）— 已通过
- 新增测试覆盖：
  - 通知服务：证书级过滤（配置/未配置事件）、默认文案、EventBus 订阅、规则事件合法性
  - 证书服务：签发非法事件拒绝、导入携带事件、签发失败发布 issue.failed、更新 notify-events
  - 部署服务：成功/失败/记录失败发布 deploy 事件
  - 前端：签发/导入表单提交体、详情页 notify-events 编辑保存

## 实现状态

- [x] 领域层：4 个新事件常量与 Valid()
- [x] 事件总线：issue.failed / deploy.success / deploy.failed（含 DeployEvent 载荷）
- [x] 证书服务：Issue/Import/ImportPFX 携带并校验 NotifyEvents，签发失败改发 issue.failed
- [x] 部署服务与 Agent 异步部署：发布 deploy 事件
- [x] 通知服务：证书级过滤、defaultTemplate 新事件分支、AttachEventBus
- [x] 存储迁移 id 15（sqlite/postgres）与列读写
- [x] HTTP：签发/导入请求字段、PUT /certificates/{id}/notify-events
- [x] OpenAPI 文档与前端类型/页面/表单

## 审计结论（实现后复审）

按计划逐项核对，全部落实。审计中发现并修复 2 个问题，另有若干遗留次要问题。

### 审计发现并已修复

1. **默认部署文案占位符 Bug**：`defaultTemplate` 的 deploy 分支原先返回字面 `{target}`/`{error}`，
   但 `render()` 仅对用户自定义模板执行 `renderTemplate`，默认模板原样发送 → 通知会显示占位符。
   已改为直接用 `details["target"]`/`details["error"]` 拼接（与续期/签发分支一致），测试同步修正
   （`internal/application/notification_service.go`、`notification_service_test.go`）。
2. **OpenAPI 运行态遗漏**：Swagger UI 实际加载的是 `webassets/openapi.yaml`（`go:embed` 注入 `/openapi.yaml`），
   此前只更新了 `docs/openapi.yaml`。已补齐 webassets：4 处 `notify_events` 字段 + 3 处事件枚举 +
   `/certificates/{id}/notify-events` 端点。

### 遗留次要问题（本次未处理）

- **到期提醒默认模板同类占位符**：预存在的 `cert_expiring` 默认文案 `证书将在 {days_left} 天后到期（{expires}）`
  存在同类不替换问题（非本次引入）。建议后续在 `render()` 中对默认模板也统一执行 `renderTemplate`。
- **Agent 超时无部署失败通知**：`ReconcileStaleAgentTasks → MarkTimedOut` 将部署记录置为失败但不发布
  `deploy.failed`。计划 N5 仅覆盖「回执完成」，超时属边缘场景，可考虑补发。
- **计划文档位置笔误**：`UpdateNotifyEvents` 实际实现在 `certificate_service.go`（更合理），
  计划改动清单误列于 `deploy_service.go` 行。
- **`encodeDomains(nil)` 存 `"null"`**：与 `dedupNotifyEvents` 注释所述 `"[]"` 不符（功能无害，
  scan 统一为 nil → 空切片 → 全部事件）。
- **site-docs 手册未提及 `notify_events`**：用户手册/API 文档未同步本次字段（计划未含）。

## 不做项（可选后续）

- CLI `argus issue` 命令支持传 `notify_events`（当前走同一 HTTP API，字段可透传但 CLI 未暴露参数）