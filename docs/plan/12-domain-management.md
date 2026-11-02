# 域名管理（域名台账）需求与方案

> 状态：**待评审**（未实施）。评审通过后按本文「实施计划」分批落地。
> 修订记录：v2 — 按评审确认调整关联模型（不建映射表，主数据 + 证照副本）与签发表单形态（combobox）。

## 1. 背景与目标

### 1.1 背景

当前系统以**证书**为核心实体，域名只是证书的一个属性（`certificates.domains` JSON 列 + `primary_domain`）：

- 没有独立的域名视角，"我关心的域名清单"无法维护
- 删除证书后域名信息从证书域**完全消失**（仅剩审计文本、agent_tasks 快照、通知日志 cert_id 三类不可反查的残留）
- 无法回答三个运维高频问题：
  1. **哪些域名应该有证书但现在没有？**（漏签/证书被删/新业务上线未签）
  2. **哪些域名的证书即将到期？**（当前只能翻证书列表按到期排序）
  3. **某个域名现在挂的是哪张证书、什么时候到期？**（多证书/通配符场景需人工比对 SAN）

### 1.2 目标（评审确认版）

建立**域名台账**：用户维护一份"我关心的域名"**主数据**清单，签发证书时从清单中选择，证书保存所选域名的**副本**；台账页每行实时展示该域名的证书覆盖情况。

- 域名与证书 **不建映射表**：关联通过证书上的 `domain_id` 引用列 + `domain_name` 副本实现（主数据 → 快照模式）
- 台账独立于证书生命周期：删证书不影响台账（反而让"缺失"显形）
- 覆盖状态由系统实时计算，无需人工维护关联

### 1.3 非目标（明确不做）

| 不做 | 原因 |
|---|---|
| 域名解析管理（增删改 DNS 记录） | 需 DNS 写权限，超出证书平台定位 |
| 域名注册/续费管理 | 属于注册商范畴 |
| 域名监控拨测（HTTP/TCP 探测） | 属于监控系统范畴 |
| 域名级独立通知 | 需改造通知防重模型（见 §9），价值/成本比低，列后续增强 |
| 签发时自动登记域名 | 评审确认：台账纯手工主数据，不做反向自动创建 |

## 2. 名词与状态定义

| 名词 | 定义 |
|---|---|
| 域名台账项（Domain） | 用户登记的一个域名（规范化后唯一）+ 备注/标签/期望状态 |
| 期望状态（expected_status） | `must_have_cert`（必须有证书，缺失即异常）/ `monitor_only`（仅关注，不要求证书） |
| 来源（source） | 评审确认后**仅 `manual`**（纯手工登记；保留字段备扩展） |
| 覆盖状态（coverage） | **计算字段**，不落库，见 §4.2 |
| 当前证书 | 该域名关联到的 `not_after` 最大且非空的证书（见 §4.3） |
| 域名副本 | 签发时把所选台账项的名称快照到证书的 `domain_name` 列；台账后续改名/删除不影响历史证书展示 |

### 覆盖状态枚举（计算）

| 值 | 含义 | 徽章色 |
|---|---|---|
| `missing` | `must_have_cert` 且无任何关联证书 | 红 |
| `expiring` | 有关联证书，且距到期 ≤ `renew_days_before`（默认 30 天） | 琥珀 |
| `expired` | 当前证书已过期 | 红 |
| `covered` | 有关联证书且到期日尚远 | 绿 |
| `monitor` | `monitor_only` 且无证书（仅关注） | 灰 |

## 3. 现状事实（设计依据）

来自代码调研的关键约束，直接决定本方案形态：

1. **域名无独立存储**：`certificates.domains` 是 JSON 文本列、无索引、无 `DomainStore`（`domain/store.go` 无此接口）
2. **全仓不使用 SQL JSON 函数**（`json_each` 等），"按域名 JOIN/WHERE" 无先例；`CertificateStore.List()` 是全量加载（`ORDER BY created_at DESC`），调度器 `RunScan` 同样是全量 + 内存处理 → **内存聚合与现有架构风格一致**
3. **域名规范化有三处重复实现**：`normalize`（`certificate_handler.go:702-711`）、`Import`（`certificate_service.go:456-463`）、`validImportDomain`（`certificate_service.go:558-573`，未导出）→ 建表前必须**下沉到 domain 包单一实现**，否则大小写/通配符判断不一致
4. **到期算法已有纯函数雏形**：`Certificate.UpdateStatus(now)`（30 天硬编码）+ 调度器 `daysLeft = ceil(NotAfter - now)`（`scheduler_service.go:197-200`）
5. **迁移机制**：`migrations[]{id, script}` + `schema_migrations` 幂等应用；sqlite 与 postgres **双写同步**，当前最大 id = **18** → 本功能用 **19**（新表 + certificates 两列同一个脚本）
6. **通知防重键是 `(cert_id, rule_id, days_before)`**（`notification_logs` 唯一索引）→ 域名级通知需改造，故本期不做
7. **删除证书会级联/清空**：`deploy_records` 显式清空、`orders` 外键级联；`agent_tasks`/通知日志/审计为不可反查残留
8. **独立表 + 独立页面有成熟范式**：DNS 验证账户（`dns_provider_accounts`）为最佳实践样板（实体/store/service/handler/迁移/前端全套）
9. **base-ui 提供 combobox 原语**（`@base-ui/react/combobox`：Root/Input/Trigger/Popup/List/Item/Chips/Chip/ChipRemove/Empty 等），本仓 `ui/` 组件即基于该底座 → 签发表单的"输入或选择"组件有官方原语可依

## 4. 方案设计

### 4.1 数据模型

**新增 `domains` 表 + `certificates` 两列**（迁移 id=19，sqlite + postgres 同步，同一脚本）：

```sql
CREATE TABLE IF NOT EXISTS domains (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,              -- 规范化后域名（小写、去首尾空格、去尾点）
    expected_status TEXT NOT NULL DEFAULT 'must_have_cert',  -- must_have_cert | monitor_only
    source          TEXT NOT NULL DEFAULT 'manual',          -- manual（预留 auto）
    note            TEXT NOT NULL DEFAULT '',
    tags            TEXT NOT NULL DEFAULT '[]',  -- JSON 数组（本仓 JSON 列惯例）
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name ON domains(name);

ALTER TABLE certificates ADD COLUMN domain_id   TEXT NOT NULL DEFAULT '';  -- 台账项引用（快照指针）
ALTER TABLE certificates ADD COLUMN domain_name TEXT NOT NULL DEFAULT '';  -- 签发时的域名副本
```

**不建 `domain_certificates` 映射表**——关联 = 证书上的引用列 + 读取时计算。理由：

| 维度 | 引用列 + 副本 | 映射表 |
|---|---|---|
| 一致性 | 单点写入（签发时一次），无同步钩子 | 证书签发/导入/删除都要同步，漏一步就错 |
| 历史保真 | 副本保留签发时点信息，台账改名不影响历史 | 改名即影响全部关联展示 |
| 复杂度 | 2 列 + 0 张表 | 1 表 + 3 个生命周期钩子 + 对账 |
| 性能 | 全量证书内存分组；百级证书 <1ms | 索引快但收益为零 |

> 与 `RunScan`（`scheduler_service.go:72` 全量 List + 内存处理）架构风格一致。若未来证书量到万级，再评估物化（API 不变）。

### 4.2 覆盖状态计算（纯函数，domain 包）

```go
// Coverage 域名覆盖状态（计算字段）
type Coverage string
const (
    CoverageMissing  Coverage = "missing"
    CoverageExpiring Coverage = "expiring"
    CoverageExpired  Coverage = "expired"
    CoverageCovered  Coverage = "covered"
    CoverageMonitor  Coverage = "monitor"
)

// ComputeCoverage 计算单个域名的覆盖状态
//   certs: 关联到该域名的证书（已按 NotAfter 降序）
//   expected: 期望状态；now: 当前时间；renewDays: renew_days_before
func ComputeCoverage(certs []*Certificate, expected string, now time.Time, renewDays int) Coverage
```

规则：
1. `expected == monitor_only` 且无证书 → `monitor`
2. 无证书且 `must_have_cert` → `missing`
3. 当前证书（NotAfter 最大）已过期 → `expired`
4. 当前证书 ≤ `now + renewDays` 到期 → `expiring`
5. 否则 → `covered`

"当前证书"选取：过滤 `NotAfter` 非零（排除 pending），取 `NotAfter` 最大者；并列时取 `UpdatedAt` 更新者。复用 `Renew` 的 `NotAfter.After()` 比较思路（`certificate_service.go:297-300`）。

### 4.3 关联判定（domain_id ∪ 名称匹配）

```go
// MatchesDomain 证书 SAN 是否覆盖指定域名：精确匹配，或通配符父域匹配
//   certDomains=["*.example.com"], name="www.example.com" → true
//   certDomains=["www.example.com"], name="example.com" → false（子域不覆盖父域）
func MatchesDomain(certDomains []string, name string) bool
```

**关联某台账项的证书集合 = `cert.domain_id == domain.id`  ∪  `MatchesDomain(cert.Domains, domain.name)`**

- `domain_id` 子句：用户在签发时**显式选择**过该台账项（含证书 SAN 与台账名不一致的场景，如台账 `example.com` + 证书 `*.example.com`——通配符不覆盖顶级域，此时只有显式关联能正确计数）
- 名称匹配子句：覆盖**历史证书**（`domain_id` 为空）与**多 SAN**（一张证书覆盖多个台账项）
- 通配符父域算覆盖（评审确认）：一张 `*.example.com` 让 `www/api.example.com` 等台账项显示 `covered`

### 4.4 台账视图 API 形态

列表接口返回**富化行**（后端计算，前端零逻辑）：

```json
{
  "id": "d1",
  "name": "www.example.com",
  "expected_status": "must_have_cert",
  "source": "manual",
  "note": "主站",
  "tags": ["prod", "web"],
  "coverage": "expiring",              // 计算
  "days_to_expire": 12,                // 计算（无证书为 null）
  "cert_count": 2,                     // 计算（关联证书总数，含历史）
  "current_cert": {                    // 计算（无证书为 null）
    "id": "c9", "primary_domain": "*.example.com",
    "status": "expiring", "provider": "acme",
    "not_after": "2027-01-10T23:59:59Z"
  },
  "created_at": "...", "updated_at": "..."
}
```

服务层 `DomainService.Ledger(ctx)`：`domains.List()` + `certs.List()` 全量 → Go 侧按关联规则分组 → 逐域名算覆盖。与 `RunScan` 同款全量内存模式。

### 4.5 签发时关联（快照，替代自动登记）

| 时机 | 行为 |
|---|---|
| `Issue` 提交 | 签发表单的域名录入组件（§6.2）从台账选择/输入；**提交时用规范化后的 primary domain 反查台账**，命中则证书记 `domain_id` + `domain_name` 副本；未命中（临时域名）两列留空，覆盖走名称匹配 |
| `ImportPFX` / `Import` | 本期**不改造**导入表单；导入证书的 `domain_id` 为空，覆盖走名称匹配（功能不受影响） |
| `Renew` | 继承原证书的 `domain_id`/`domain_name`（`applyIssued` 不触碰这两列即可） |
| `Delete`（证书） | 不动台账——台账独立留存，"缺失"由此显形（本功能核心价值） |
| `Delete`（台账项） | 允许删除；已签发证书保留副本（`domain_name`），其覆盖统计改走名称匹配。删除确认框提示"N 张证书引用了该域名" |

## 5. API 设计

| 方法 | 路径 | 角色 | 说明 |
|---|---|---|---|
| GET | `/api/v1/domains` | 全部（含 readonly） | 台账列表（富化行） |
| POST | `/api/v1/domains` | admin/operator | 创建；名称规范化后查重，重复返回 409 |
| GET | `/api/v1/domains/{id}` | 全部 | 详情（富化行 + 该域名全部关联证书） |
| PUT | `/api/v1/domains/{id}` | admin/operator | 更新 name/note/tags/expected_status；改名查重 |
| DELETE | `/api/v1/domains/{id}` | admin/operator | 删除台账项（不影响证书，副本保留） |

- 请求/响应走既有信封（`writeOK`/`writeError`/`mapError`），创建返回 201
- `requiredScope` 加 case：`/api/v1/domains` → `certificates` 读/写 scope（复用语义，不新增 scope 枚举）
- 审计 action **新增常量** `domain.create/update/delete`（不复用 `account.*`——dns-accounts 的复用时前端标签无法区分，是既有技术债）
- 审计带 actor（抄 `contact_service.go:98-111` 模式，handler 用 `userFrom(r.Context())` 传 principal）
- OpenAPI：paths 三条 + `Domain`/`DomainLedger` schema（照 `/dns-accounts` 条目写法）
- 前端签发表单另需台账数据源：直接复用 `GET /api/v1/domains`（无需新端点）

## 6. 前端设计

### 6.1 新增/修改文件

| 文件 | 动作 |
|---|---|
| `web/src/lib/types.ts` | 追加 `Domain`、`DomainLedger` interface（snake_case 直通） |
| `web/src/features/domains/domains-page.tsx` | **新增**台账页 |
| `web/src/features/certificates/domain-input.tsx` | **新增**域名录入组件（combobox，见 6.2） |
| `web/src/features/certificates/issue-dialog.tsx` | 域名输入改用 `DomainInput`；提交体加 `domain_id`（后端解析则不加，见 6.3） |
| `web/src/router.tsx` | `{ path: "domains", element: <DomainsPage /> }` |
| `web/src/lib/nav.ts` | 「证书」分组追加 `{ title: "域名管理", url: "/domains", icon: GlobeIcon }` |

### 6.2 域名录入组件 `DomainInput`（"输入或选择"）

**形态**（评审确认：类似 combobox，左输入框 + 右选择按钮）：

```
┌─────────────────────────────────────────────┐
│ [example.com ×] [www.example.com ×]  [输入…] [选择 ▾] │
└─────────────────────────────────────────────┘
         ↓ 点「选择」
┌───────────────────────────┐
│ 搜索域名…                  │
│ ☑ example.com     主站     │
│ ☐ www.example.com         │
│ ☐ api.example.com         │
│ （空态：暂无域名，去域名管理登记）│
└───────────────────────────┘
```

**技术底座**：`@base-ui/react/combobox` 原语（Root/Input/Trigger/Popup/Positioner/List/Item/ItemIndicator/Chips/Chip/ChipRemove/Empty），与 `ui/` 组件同源；封装为本 feature 内组件，不进 `ui/`（业务语义）。

**行为契约**：
- 对外接口 `value: string`（逗号/空格分隔，**与现有 zod schema 和提交逻辑零改动**）+ `onChange`；内部维护 chips = 规范化后的 token 列表
- **输入**：自由输入，逗号/空格/回车落为 chip；规范化（小写、去尾点）后展示；重复自动去重
- **选择**：Trigger 按钮弹出面板，面板内搜索过滤 + 台账多选（checkbox 样式，选中态用 ItemIndicator）；勾选即合并进 chips
- **移除**：chip 上 × 单个移除；`ComboboxClear` 一键清空
- 键盘：Enter 落 chip / ↓↑ 导航 / Esc 关面板（原语自带）
- 台账数据由组件内部 `useQuery(["domains"])` 拉取（父表单不传）

### 6.3 签发提交与 domain_id 归属

**`domain_id` 由后端解析**（不在前端传）：`CertificateService.Issue` 提交时，用规范化后的 `domains[0]` 查 `domains` 表，命中则写入 `domain_id` + `domain_name`（副本 = 该台账项当前名称）。好处：规则单一、前端无感、导入/CLI 等其他入口未来也自动受益。

### 6.4 台账页结构

- **顶部统计条**（前端从富化列表计算，无需新接口）：总数 / 缺失证书（红）/ 30 天内到期（琥珀）/ 仅关注（灰）
- **表格列**：域名（font-medium）｜覆盖状态徽章（色见 §2）｜当前证书（primary_domain + provider 徽章）｜到期时间 + 剩余天数｜期望状态｜标签（Badge 组）｜备注（截断）｜操作（编辑/删除）
- **工具栏**：刷新按钮 + 「登记域名」+ 覆盖状态筛选 Select（全部/缺失/即将到期/已过期/仅关注，客户端过滤）
- 遵守 v1.2.0 约定：`useTableRefresh` + `TableRefreshOverlay` 四件套、`onOpenChange` 关闭即清空、失败内联 Alert + 简短 toast
- **新建/编辑弹窗**：域名 Input（helper 说明自动规范化，允许 `*.example.com`）｜期望状态 Select｜标签 Input（逗号分隔 + Badge 展示）｜备注 Textarea
- 删除确认用原生 `confirm()`（全仓统一），文案带引用该域名的证书数

### 6.5 前端测试

| 文件 | 覆盖 |
|---|---|
| `web/src/features/domains/domains-page.test.tsx` | 列表渲染 + 覆盖徽章 + 刷新按钮/遮罩 + 筛选 |
| `web/src/features/certificates/domain-input.test.tsx` | 输入落 chip / 去重 / 从面板选择合并 / 移除 / 清空 |

## 7. 实施计划（分批，可独立验收）

| 批次 | 内容 | 出口标准 |
|---|---|---|
| **D1 后端核心** | ① 域名规范化/匹配纯函数下沉 domain 包（统一三处重复实现，`validImportDomain` 导出）；② `domain.Domain` 实体 + `DomainStore` + sqlite 实现；③ 迁移 id=19（domains 表 + certificates 两列，**sqlite + postgres 双写**）；④ `DomainService`（CRUD + 查重 + 审计带 actor） | `go test ./...` 全绿；service/handler/stores 三层新测试；PG 与 sqlite 迁移 id 对齐 |
| **D2 台账视图** | ① `ComputeCoverage`/`MatchesDomain` 纯函数 + 表驱动单测；② `Issue` 时按 primary domain 解析 `domain_id`/`domain_name`；③ `DomainService.Ledger()` 富化列表 + 详情；④ handler + 路由 + scope + openapi | 五态（missing/expiring/expired/covered/monitor）handler 测试；签发关联集成测试 |
| **D3 前端** | ① types；② `DomainInput` combobox 组件 + 测试；③ `issue-dialog` 接入；④ 域名台账页 + 路由 + nav + 测试 | `tsc`/vitest/oxlint 全绿；页面符合 v1.2.0 四件套 |
| **D4 收尾** | ① `06-milestones.md` 设计备忘；② CHANGELOG `[未发布]`；③ 域名页与签发联调自测清单 | 发布前 guard 通过 |

每批独立可评审、可回滚；D1→D4 顺序依赖，批内可并行。

## 8. 风险与边界

| 风险 | 缓解 |
|---|---|
| 规范化下沉触及签发/导入主路径 | 纯函数搬移 + 全量测试兜底；签发/导入集成测试覆盖大小写/尾点/通配符输入 |
| sqlite/PG 迁移 id 不一致 | 出口标准强制双写核对；`TestMigrationIdempotent` 已覆盖幂等 |
| 全量证书内存聚合的性能 | 文档注明量级假设（数百张）；万级时再评估物化（API 不变） |
| `domain_id` 与名称匹配双重计数 | 关联判定用集合并（§4.3），cert_count 对去重后的证书集计数 |
| 台账项删除后历史证书展示 | 副本 `domain_name` 保留签发时点信息；覆盖统计自动降级为名称匹配 |
| combobox 组件复杂度 | 基于官方原语；对外契约保持 `string`（不碰现有 schema/提交逻辑）；专属测试覆盖 |
| 与"证书列表按域名搜索"需求重叠 | 二者互补：证书列表回答"这张证书是什么"，台账回答"这个域名有没有证书"；MVP 不改造证书列表 |

## 9. 后续增强（本期不做，已登记）

| 项 | 说明 |
|---|---|
| 域名级到期通知 | 需新事件 `domain_expiring` + 防重键从 `cert_id` 改为 `(domain_id, rule_id, days_before)`；模板变量加 `{domain_name}` |
| 仪表盘域名卡片 | `/api/v1/stats` 增加 `domains_total`/`domains_missing_cert`；Prometheus 同步 |
| 域名维度证书历史 | `GET /api/v1/domains/{id}`（富化详情 + 该域名全部历史证书/续期链）；本期列表已含当前证书与计数，故未实现此端点 |
| 批量导入域名 | CSV/粘贴批量登记台账 |
| 导入证书时关联台账选择器 | 导入表单接入 `DomainInput`（当前为后端按 primary domain 自动反查落副本） |

## 10. 顺带发现的既有缺陷（评审确认：单独修，不混入本功能）

**导入证书在默认配置下收不到到期提醒**：`scheduler_service.go:107-110` 对 import 证书 `continue` 跳过续期的同时，也跳过了同一轮的到期通知；而 `renew_days_before`（默认 30）≥ `expiryDays` 最大档位（30），导致 import 证书**任何档位都发不出提醒**。修法：把 `continue` 改为"跳过续期但继续走通知分支"。作为独立小版本修复。

---

## 附：评审确认结论（v2）

| # | 决策点 | 结论 |
|---|---|---|
| 1 | 关联模型 | **不建映射表**；域名纯主数据管理，证书存 `domain_id` 引用 + `domain_name` 副本 |
| 2 | 自动登记 | **不做**；签发时从台账选择（combobox：可输入可选择），提交时后端按 primary domain 反查落副本 |
| 3 | 通配符覆盖 | **算覆盖**（通配符父域覆盖子域） |
| 4 | 域名级通知 | **本期不做**，列后续增强 |
| 5 | import 提醒缺陷 | **单独修**，不混入本功能 |
