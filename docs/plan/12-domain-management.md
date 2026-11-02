# 域名管理（域名台账）需求与方案

> 状态：**待评审**（未实施）。评审通过后按本文「实施计划」分批落地。

## 1. 背景与目标

### 1.1 背景

当前系统以**证书**为核心实体，域名只是证书的一个属性（`certificates.domains` JSON 列 + `primary_domain`）：

- 没有独立的域名视角，"我关心的域名清单"无法维护
- 删除证书后域名信息从证书域**完全消失**（仅剩审计文本、agent_tasks 快照、通知日志 cert_id 三类不可反查的残留）
- 无法回答三个运维高频问题：
  1. **哪些域名应该有证书但现在没有？**（漏签/证书被删/新业务上线未签）
  2. **哪些域名的证书即将到期？**（当前只能翻证书列表按到期排序）
  3. **某个域名现在挂的是哪张证书、什么时候到期？**（多证书/通配符场景需人工比对 SAN）

### 1.2 目标

建立**域名台账**：用户维护一份"我关心的域名"清单，每行实时展示该域名的证书覆盖情况。

- 域名与证书 **1:N**（一个域名可被多张证书覆盖：续期前后重叠、通配符 + 具体域）
- 台账独立于证书生命周期：删证书不影响台账（反而让"缺失"显形）
- 覆盖状态由系统实时计算，无需人工维护关联

### 1.3 非目标（明确不做）

| 不做 | 原因 |
|---|---|
| 域名解析管理（增删改 DNS 记录） | 需 DNS 写权限，超出证书平台定位 |
| 域名注册/续费管理 | 属于注册商范畴 |
| 域名监控拨测（HTTP/TCP 探测） | 属于监控系统范畴 |
| 域名级独立通知（MVP） | 需改造通知防重模型（见 §7.4），价值/成本比低，列后续增强 |

## 2. 名词与状态定义

| 名词 | 定义 |
|---|---|
| 域名台账项（Domain） | 用户登记的一个域名（规范化后唯一）+ 备注/标签/期望状态 |
| 期望状态（expected_status） | `must_have_cert`（必须有证书，缺失即异常）/ `monitor_only`（仅关注，不要求证书） |
| 来源（source） | `manual`（手工创建）/ `auto`（签发/导入时自动登记） |
| 覆盖状态（coverage） | **计算字段**，不落库，见 §4.2 |
| 当前证书 | 该域名名下 `not_after` 最大且非空的证书（见 §4.3） |

### 覆盖状态枚举（计算）

| 值 | 含义 | 徽章色 |
|---|---|---|
| `missing` | `must_have_cert` 且无任何有效证书 | 红 |
| `expiring` | 有证书，且距到期 ≤ `renew_days_before`（默认 30 天） | 琥珀 |
| `expired` | 当前证书已过期 | 红 |
| `covered` | 有证书且到期日尚远 | 绿 |
| `monitor` | `monitor_only` 且无证书（仅关注） | 灰 |

## 3. 现状事实（设计依据）

来自代码调研的关键约束，直接决定本方案形态：

1. **域名无独立存储**：`certificates.domains` 是 JSON 文本列、无索引、无 `DomainStore`（`domain/store.go` 无此接口）
2. **全仓不使用 SQL JSON 函数**（`json_each` 等），"按域名 JOIN/WHERE" 无先例；`CertificateStore.List()` 是全量加载（`ORDER BY created_at DESC`），调度器 `RunScan` 同样是全量 + 内存处理 → **内存聚合与现有架构风格一致**
3. **域名规范化有三处重复实现**：`normalize`（`certificate_handler.go:702-711`）、`Import`（`certificate_service.go:456-463`）、`validImportDomain`（`certificate_service.go:558-573`，未导出）→ 建表前必须**下沉到 domain 包单一实现**，否则大小写/通配符判断不一致
4. **到期算法已有纯函数雏形**：`Certificate.UpdateStatus(now)`（30 天硬编码）+ 调度器 `daysLeft = ceil(NotAfter - now)`（`scheduler_service.go:197-200`）
5. **迁移机制**：`migrations[]{id, script}` + `schema_migrations` 幂等应用；sqlite 与 postgres **双写同步**，当前最大 id = **18** → 本功能用 **19**
6. **通知防重键是 `(cert_id, rule_id, days_before)`**（`notification_logs` 唯一索引）→ 域名级通知需改造，故 MVP 不做
7. **删除证书会级联/清空**：`deploy_records` 显式清空、`orders` 外键级联；`agent_tasks`/通知日志/审计为不可反查残留
8. **独立表 + 独立页面有成熟范式**：DNS 验证账户（`dns_provider_accounts`）为最佳实践样板（实体/store/service/handler/迁移/前端全套）

## 4. 方案设计

### 4.1 数据模型

新增 `domains` 表（迁移 id=19，sqlite + postgres 同步）：

```sql
CREATE TABLE IF NOT EXISTS domains (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,              -- 规范化后域名（小写、去首尾空格、去尾点）
    expected_status TEXT NOT NULL DEFAULT 'must_have_cert',  -- must_have_cert | monitor_only
    source          TEXT NOT NULL DEFAULT 'manual',          -- manual | auto
    note            TEXT NOT NULL DEFAULT '',
    tags            TEXT NOT NULL DEFAULT '[]',  -- JSON 数组（本仓 JSON 列惯例）
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name ON domains(name);
```

**不建 `domain_certificates` 映射表**——关联关系在读取时计算（见 4.3）。理由：

| 维度 | 内存计算 | 映射表 |
|---|---|---|
| 一致性 | 无双写，永不失真 | 证书签发/导入/删除都要同步，漏一步就错 |
| 复杂度 | 0 张额外表、0 个钩子维护 | 1 表 + 3 个生命周期钩子 + 对账 |
| 性能 | 全量证书内存建索引；百级证书 <1ms | 索引快但收益为零 |
| 量级假设 | 单实例个人/中小企业，证书数百张上限 | — |

> 与 `RunScan`（`scheduler_service.go:72` 全量 List + 内存处理）架构风格一致。若未来证书量到万级，再升级为映射表（届时作为独立重构，不影响 API）。

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
//   certs: 该域名匹配到的证书（已按 NotAfter 降序）
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

### 4.3 域名匹配（含通配符）

```go
// MatchesDomain 证书 SAN 是否覆盖指定域名：精确匹配，或通配符父域匹配
//   certDomains=["*.example.com"], name="www.example.com" → true
//   certDomains=["www.example.com"], name="example.com" → false（子域不覆盖父域）
func MatchesDomain(certDomains []string, name string) bool
```

**通配符父域算覆盖**（MVP 即支持）：一张 `*.example.com` 证书应让 `www.example.com`、`api.example.com` 等台账项显示 `covered`。这是最常见场景，成本只是去掉首个标签。

### 4.4 台账视图 API 形态

列表接口返回**富化行**（后端计算，前端零逻辑）：

```json
{
  "id": "d1",
  "name": "www.example.com",
  "expected_status": "must_have_cert",
  "source": "auto",
  "note": "主站",
  "tags": ["prod", "web"],
  "coverage": "expiring",              // 计算
  "days_to_expire": 12,                // 计算（无证书为 null）
  "cert_count": 2,                     // 计算（匹配到的证书总数，含历史）
  "current_cert": {                    // 计算（无证书为 null）
    "id": "c9", "primary_domain": "*.example.com",
    "status": "expiring", "provider": "acme",
    "not_after": "2027-01-10T23:59:59Z"
  },
  "created_at": "...", "updated_at": "..."
}
```

服务层 `DomainService.Ledger(ctx)`：`domains.List()` + `certs.List()` 全量 → Go 侧按域名分组 `map[string][]*Certificate` → 逐域名算覆盖。与 `RunScan` 同款全量内存模式。

### 4.5 自动登记（生命周期钩子）

| 时机 | 行为 |
|---|---|
| `Issue` 成功 | 对 `cert.Domains` 逐个规范化后 **upsert-if-absent**（`source=auto`，`expected_status=must_have_cert`） |
| `ImportPFX` / `Import` 成功 | 同上 |
| `Renew` 成功 | 不动（域名已存在；不存在说明用户删过台账，尊重用户意图） |
| `Delete`（证书） | **不动**——台账独立留存，"缺失"由此显形（本功能核心价值） |

`upsert-if-absent` 语义：`GetByName` 不存在才创建。用户把 auto 项改成 `monitor_only` 后不会被后续签发覆盖。

## 5. API 设计

| 方法 | 路径 | 角色 | 说明 |
|---|---|---|---|
| GET | `/api/v1/domains` | 全部（含 readonly） | 台账列表（富化行） |
| POST | `/api/v1/domains` | admin/operator | 创建；名称规范化后查重，重复返回 409 |
| GET | `/api/v1/domains/{id}` | 全部 | 详情（富化行 + 该域名全部匹配证书） |
| PUT | `/api/v1/domains/{id}` | admin/operator | 更新 name/note/tags/expected_status；改名查重 |
| DELETE | `/api/v1/domains/{id}` | admin/operator | 删除台账项（不影响证书）；有证书时返回提示信息仍允许删 |

- 请求/响应走既有信封（`writeOK`/`writeError`/`mapError`），创建返回 201
- `requiredScope` 加 case：`/api/v1/domains` → `certificates` 读/写 scope（复用语义，不新增 scope 枚举）
- 审计 action **新增常量** `domain.create/update/delete`（不复用 `account.*`——dns-accounts 的复用时前端标签无法区分，是既有技术债）
- 审计带 actor（抄 `contact_service.go:98-111` 模式，handler 用 `userFrom(r.Context())` 传 principal）
- OpenAPI：paths 三条 + `Domain`/`DomainLedger` schema（照 `/dns-accounts` 条目写法）

## 6. 前端设计

### 6.1 新增文件

| 文件 | 内容 |
|---|---|
| `web/src/lib/types.ts` | 追加 `Domain`、`DomainLedger` interface（snake_case 直通） |
| `web/src/features/domains/domains-page.tsx` | 台账页（照 §6.3 模板） |
| `web/src/router.tsx` | `{ path: "domains", element: <DomainsPage /> }` |
| `web/src/lib/nav.ts` | 「证书」分组追加 `{ title: "域名管理", url: "/domains", icon: GlobeIcon }` |

### 6.2 页面结构

- **顶部统计条**（前端从富化列表计算，无需新接口）：总数 / 缺失证书（红）/ 30 天内到期（琥珀）/ 仅关注（灰）
- **表格列**：域名（font-medium，点击可复制？不必）｜覆盖状态徽章（色见 §2）｜当前证书（primary_domain + provider 徽章）｜到期时间 + 剩余天数｜期望状态｜标签（Badge 组）｜备注（截断）｜操作（编辑/删除）
- **工具栏**：刷新按钮 + 「登记域名」（照 v1.2.0 刷新遮罩四件套模板）
- **筛选**（工具栏 Select）：全部 / 缺失证书 / 即将到期 / 已过期 / 仅关注——按 coverage 客户端过滤（数据量小）

### 6.3 新建/编辑弹窗

| 字段 | 控件 | 说明 |
|---|---|---|
| 域名 | Input | placeholder「example.com 或 www.example.com」；helper 文案说明自动规范化（小写、去尾点）；通配符 `*.example.com` 允许登记 |
| 期望状态 | Select | `must_have_cert`（必须有证书，缺失时标红）/ `monitor_only`（仅关注） |
| 标签 | Input | **逗号分隔**（本仓先例：notification-rules 提前天数同款输入法）+ Badge 展示 |
| 备注 | Textarea | 选填 |

- 弹窗遵守 v1.2.0 新约定：`onOpenChange` 关闭即清空（覆盖 Esc/遮罩/X）+ 失败内联 Alert + toast 简短提醒
- 删除确认用原生 `confirm()`（全仓统一），文案带该域名当前证书数提示

### 6.4 前端测试

`web/src/features/domains/domains-page.test.tsx`：列表渲染 + 覆盖状态徽章 + 刷新按钮/遮罩（照 `certificates-page.test.tsx` 断言模式）。

## 7. 实施计划（分批，可独立验收）

| 批次 | 内容 | 出口标准 |
|---|---|---|
| **D1 后端核心** | ① 域名规范化/匹配纯函数下沉 domain 包（统一三处重复实现，含 `validImportDomain` 导出）；② `domain.Domain` 实体 + `DomainStore` 接口 + sqlite 实现；③ 迁移 id=19（sqlite + **postgres 双写**）；④ `DomainService`（CRUD + 查重 + 审计带 actor）；⑤ handler + 路由 + scope + openapi | `go test ./...` 全绿；新增 service/handler/stores 三层测试；PG 迁移与 sqlite id 对齐 |
| **D2 台账视图** | ① `ComputeCoverage`/`MatchesDomain` 纯函数 + 单测；② `DomainService.Ledger()` 富化列表 + 详情；③ 域名规范化全链路回归（签发/导入不受影响） | 纯函数表驱动测试；handler 测试覆盖 missing/expiring/expired/covered/monitor 五态 |
| **D3 前端** | types + 页面 + 路由 + nav + 测试 | `tsc`/vitest/oxlint 全绿；页面符合 v1.2.0 刷新遮罩四件套 |
| **D4 钩子与收尾** | ① `Issue`/`Import` 成功后自动登记；② `06-milestones.md` 设计备忘；③ CHANGELOG `[未发布]` | 自动登记集成测试；发布前 guard 通过 |

每批独立可评审、可回滚；D1→D4 顺序依赖，批内可并行。

## 8. 风险与边界

| 风险 | 缓解 |
|---|---|
| 规范化下沉触及签发/导入主路径 | 纯函数搬移 + 全量测试兜底；签发/导入集成测试必须覆盖大小写/尾点/通配符输入 |
| sqlite/PG 迁移 id 不一致 | 出口标准强制双写核对；`TestMigrationIdempotent` 已覆盖幂等 |
| 全量证书内存聚合的性能 | 文档注明量级假设（数百张）；万级时升级映射表（API 不变） |
| 自动登记产生噪音数据 | `source=auto` 可筛选；upsert-if-absent 不覆盖用户修改 |
| 与"证书列表按域名搜索"需求重叠 | 二者互补：证书列表回答"这张证书是什么"，台账回答"这个域名有没有证书"；MVP 不改造证书列表 |
| 通配符覆盖误判 | `MatchesDomain` 明确语义（仅通配符父域覆盖子域，子域不覆盖父域）+ 表驱动单测锁定 |

## 9. 后续增强（本期不做，已登记）

| 项 | 说明 |
|---|---|
| 域名级到期通知 | 需新事件 `domain_expiring` + 防重键从 `cert_id` 改为 `(domain_id, rule_id, days_before)`；模板变量加 `{domain_name}` |
| 仪表盘域名卡片 | `/api/v1/stats` 增加 `domains_total`/`domains_missing_cert`；Prometheus 同步 |
| 域名维度证书历史 | 详情页展示该域名全部历史证书（续期链） |
| 批量导入域名 | CSV/粘贴批量登记台账 |

## 10. 顺带发现的既有缺陷（建议单独修，不混入本功能）

**导入证书在默认配置下收不到到期提醒**：`scheduler_service.go:107-110` 对 import 证书 `continue` 跳过续期的同时，也跳过了同一轮的到期通知；而 `renew_days_before`（默认 30）≥ `expiryDays` 最大档位（30），导致 import 证书**任何档位都发不出提醒**。修法：把 `continue` 改为"跳过续期但继续走通知分支"。建议作为独立小版本修复，避免与本功能范围纠缠。

---

## 附：待评审确认项

1. **关联模型**：内存计算（本方案，推荐）vs 建 `domain_certificates` 映射表
2. **自动登记**：Issue/Import 成功时自动入台账（推荐）vs 纯手工登记
3. **通配符覆盖**：`*.example.com` 算覆盖子域（推荐）vs 仅精确匹配
4. **域名级通知**：MVP 不做、列后续（推荐）vs 本期一并做
5. **import 证书提醒缺口**：单独修（推荐）vs 暂不处理
