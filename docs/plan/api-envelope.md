# API 统一响应信封（方案 A）实施计划

> 状态：已确认，待实施
> 目的：统一所有 JSON 接口响应格式，支持分页元信息、结构化错误码、请求链路追踪（trace_id）

## 1. 背景与问题

当前响应格式不统一：

| 场景 | 现状 | 问题 |
|------|------|------|
| 成功 | 裸数据（数组/对象） | 无统一包装 |
| 错误 | `{"error": "..."}` | 无结构化错误码 |
| 分页 | 裸数组 | 无 total/limit/offset，前端靠数组长度猜下一页 |
| 特殊（422 签发失败） | `{"error":"...","certificate":{...}}` | 混合结构 |

## 2. 目标信封规范

### 成功

```json
{
  "code": 0,
  "message": "ok",
  "trace_id": "req_4f2c91ab...",
  "timestamp": "2026-08-16T21:40:00.123Z",
  "data": <原数据>
}
```

### 错误

```json
{
  "code": "conflict",
  "message": "渠道名称已存在",
  "trace_id": "req_4f2c91ab...",
  "timestamp": "2026-08-16T21:40:00.123Z",
  "data": null
}
```

### 分页（data 内）

```json
{
  "code": 0,
  "message": "ok",
  "trace_id": "...",
  "timestamp": "...",
  "data": { "items": [...], "total": 137, "limit": 50, "offset": 0 }
}
```

### 422 签发失败

```json
{ "code": "unprocessable", "message": "签发失败: ...", "trace_id": "...", "timestamp": "...",
  "data": { "certificate": { ... } } }
```

## 3. 错误码（由最终 HTTP 状态推导）

| HTTP 状态 | code |
|-----------|------|
| 400 | `invalid_argument` |
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 409 | `conflict` |
| 422 | `unprocessable` |
| 429 | `too_many_requests` |
| 501 | `not_implemented` |
| 500 | `internal_error` |
| 成功 | `0` |

## 4. trace_id

- 来源：chi `middleware.RequestID`（请求链已挂载，`middleware.GetReqID(ctx)`）
- 成功/错误信封均携带；响应头 `X-Request-Id` 随 chi 透出
- **5xx 错误自动落服务端日志**（含 trace_id + method + path + err），4xx 不记

## 5. 豁免清单（保持裸格式，不包信封）

| 端点 | 理由 |
|------|------|
| `/api/v1/agent/*`（AgentRoutes 出站协议） | 新旧 agent 二进制兼容（X-Agent-Token 协议） |
| `/healthz` | 运维探活标准 |
| 证书/二进制下载、CSV 导出、`/metrics`、`/openapi.yaml` | 非 JSON 响应 |

实现：`AgentRoutes` 与 `healthz` 改用独立 `writeRawJSON/writeRawError`。

## 6. 实施步骤

| 步骤 | 内容 | 验证 |
|------|------|------|
| 1 | `respond.go`：envelope 结构 + `writeJSON` 统一包装 + `statusToCode` + 5xx 日志；新增 `writeRawJSON/writeRawError`；AgentRoutes/healthz 迁移 | 构建 + httpapi 测试 |
| 2 | 分页信封：`audit-logs`/`task-logs`/`notification-send-logs` 补 total（COUNT）→ `{items,total,limit,offset}` | 接口冒烟 |
| 3 | 前端：拦截器成功解包 `res.data.data`（blob 跳过）；`getErrorMessage` 取 `message`；3 个分页页改 items/total | tsc + vite build |
| 4 | CLI 远程：`remoteClient.do` 成功解包 data、错误解析 message | `cert list --server` 实测 |
| 5 | httpapi 测试：`decodeData`/`decodeErr` helper + 迁移全部断言 | `go test ./...` |
| 6 | OpenAPI：Envelope/ErrorEnvelope/Page schema、错误码枚举、豁免说明 | 文档一致性 |
| 7 | 全量回归 + 实机验证 | 见第 7 节 |

## 7. 验证清单

- [ ] `go vet ./...` / `go test ./...`（含 Pebble/PG 集成）全绿
- [ ] 前端 `tsc -b && vite build` 通过
- [ ] 登录：响应 `data.access_token` 解包正常
- [ ] 列表/创建/更新：信封结构正确
- [ ] 分页：total 与实际条数一致，前端翻页正确
- [ ] 404/400/409：错误信封含 code/message/trace_id
- [ ] 5xx：服务端日志含 trace_id
- [ ] agent 出站端点：裸格式不变（register/tasks/upgrade 解析正常）
- [ ] `/healthz` 裸格式不变
- [ ] CLI 远程模式：`cert list`/`issue` 正常工作
- [ ] 下载/CSV/metrics/openapi 不受影响

## 8. 风险与缓解

| 风险 | 缓解 |
|------|------|
| httpapi 测试批量适配遗漏 | helper 统一入口 + 全量测试兜底 |
| agent 兼容 | 出站端点豁免，新旧 agent 均正常 |
| 前端解析遗漏 | 解包集中在响应拦截器，页面零改动（分页 3 页除外） |

## 9. 影响面统计

- 后端：`writeOK/writeError/writeJSON` 约 178 处（信封层统一生效，handler 零改动）
- 豁免迁移：`agent_handler.go`（7 处）、`healthz`（1 处）
- 前端：`api.ts` 拦截器 + 3 分页页；其余页面零改动
- 测试：httpapi 包约 200 处 JSON 断言迁移
- 文档：`docs/openapi.yaml` schema 更新
