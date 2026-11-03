# 前端组件实践知识库

> 状态：**生效中**。本文件是前端组件与交互的**唯一实践规范**——新增/修改组件前先查这里；
> 踩到新坑请回填对应章节（尤其是 §6 坑位清单）。
> 覆盖范围：`web/src/`（React 19 + TypeScript + Tailwind v4 + base-ui 自建 shadcn 风格组件 + TanStack Query v5 + react-hook-form + zod + sonner）。

## 1. 总则

1. **组件在 `components/ui/`，业务页面在 `features/<域>/`，跨功能复用的业务组件放 `components/`**（如 `domain-input.tsx`、`empty-select-hint.tsx`、`refresh-button.tsx`）。
2. **展示组件不碰导航**：需要跳转时由页面层传回调（见 `TargetFields.onGoToAgents`），保持组件可测试、可复用。
3. **样式用 `cn()` 合并**（`lib/utils.ts`），允许调用方 `className` 覆盖；颜色用语义 token（`bg-background`、`text-muted-foreground`），不写死色值（徽章配色除外，见 §2.6）。
4. **中文文案进组件，英文进标识符**；用户可见的错误提示用中文，`console`/日志用英文。
5. 任何"选中后展示什么"的逻辑，**单一来源**：不要让触发器与列表各写一份 label（见 §2.1 的 `items` 规则）。

---

## 2. 组件约定

### 2.1 Select（base-ui）— 高频，坑最多

| 规则 | 说明 |
|---|---|
| **value ≠ label 时必须传 `items`** | base-ui 的 `Select` 支持 `items={[{value,label}]}`；传入后 `<SelectValue />` **自动渲染选中项 label**。裸用 `<SelectValue />` 会在选中后显示原始 value（如 `must_have_cert`），是本期修掉的 5 处 bug 的根因 |
| **`items` 与下拉项共用同一数组** | `const OPTS=[...]` → `<Select items={OPTS}>` + `<SelectContent>{OPTS.map(...)}</SelectContent>`，label 不可能漂移 |
| **表单内必须 `className="w-full"`** | `SelectTrigger` 默认 `w-fit`：有值时贴合文字，**无值时塌成一条细线**（只有箭头） |
| **`onValueChange` 参数可能为 `null`** | 一律 `(v) => setX(v ?? 默认值)` |
| **空选项 = 死路时用引导块** | 见 §2.2；不要留空下拉或禁用项 |
| 动态选项（对象列表） | `items={(list ?? []).map(x => ({ value: x.id, label: x.name }))}`，或继续用 `<SelectValue>{(v) => map[v] ?? v}</SelectValue>` 函数子（两者都可，但别混用同一次选择的两种来源） |

```tsx
// 标准写法（静态选项）
const STATUS_OPTIONS = [
  { value: "all", label: "全部" },
  { value: "success", label: "成功" },
]
<Select value={filter} onValueChange={(v) => setFilter(v ?? "all")} items={STATUS_OPTIONS}>
  <SelectTrigger className="w-36"><SelectValue placeholder="状态" /></SelectTrigger>
  <SelectContent>
    {STATUS_OPTIONS.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
  </SelectContent>
</Select>
```

### 2.2 空选项引导 `EmptySelectHint`

选项为空且用户**必须先去别处创建**时，用 `components/empty-select-hint.tsx`：

```tsx
{items && items.length === 0 ? (
  <EmptySelectHint
    text="尚未配置渠道账户。签发证书需要先配置 ACME 或云厂商渠道。"
    actionLabel="去创建渠道账户"
    onAction={() => { onOpenChange(false); navigate("/acme-accounts") }}
  />
) : (<Select …>…</Select>)}
```

已应用：渠道账户、申请人、批量部署目标、部署目标的 Agent。
**不适用**：选项为空但有兜底路径的（如 DNS 验证账户有"手动输入凭据"）——补宽度即可。

### 2.3 Combobox / 域名录入 `DomainInput`

基于 base-ui `combobox` 的"输入或选择"组件（`components/domain-input.tsx`），两种模式：

- **多域名**（签发证书）：chips 累加，值契约是逗号/空格分隔 string
- **单域名**（`single`，IIS SNI 主机名）：无 chips，输入框直接展示当前值，面板选择即替换

**五条硬规则**（每条都是一个修过的 bug）：

1. **面板只能由右侧 ▾ 按钮开启**。可输入 combobox 弹层开启时，base-ui 会把弹层外部内容置 `aria-hidden`（`FloatingFocusManager` 的 `ariaHidden: modal || isUntrappedTypeableCombobox` 对可输入 combobox 恒真）——在签发弹窗内会"盖住"其余字段（屏幕阅读器读不到；鼠标键盘虽可用但体验差）。因此 `onOpenChange` 要**忽略非按钮来源的打开请求**（用 ref 标记按钮点击）。
   - 附带结论：`modal={false}` 在这个布局下**无效**（`focusManagerModal = !inputInsidePopup || modal`，输入框在 popup 外时恒为 true），别依赖它。
2. **面板打开期间的输入是"过滤词"，不是待录入域名**。关闭时：多域名模式丢弃、单域名模式恢复显示当前值。否则过滤词会变成 chip 并**覆盖掉刚选中的项**（选中后 base-ui 会自动关面板 → 关闭回调误提交）。
3. **Esc 自行处理**（关闭面板 + 恢复输入显示）。base-ui 在面板未 `mounted` 时会把 Esc 当作"清空选中值"，且 `mounted` 在 jsdom 与真实浏览器表现不一致——不要依赖它。单域名模式下还要在 `onValueChange` 里**忽略置空变更**（用户清空只走清除按钮 / Backspace）。
4. **单域名模式提交时必须同步设置输入框展示值**。若提交值与现值相同，`onChange` 不触发、依赖 `[value]` 的同步 effect 也不跑，输入框会停在空文本——即"选完域名失焦后文字消失、只剩清除图标"。
5. **trigger 点击与 base-ui 内部开关会双触发**导致面板重开：按钮点击时先置 `openByButton.current = true` 再 toggle，`onOpenChange` 里消费掉该标记。

**chip 布局**：逐个移除的 × 在 chip 文字右侧（标准 chip 模式）；「清空全部」× 统一放在**输入框右侧、紧邻 ▾**（两种模式一致，别插在输入框前面）。

### 2.4 Dialog 表单

| 规则 | 原因 |
|---|---|
| **打开时 reset + 关闭时 reset**（`onOpenChange` 里 `if (!o) { reset(DEFAULT); … }`） | Dialog 是受控 `open` 常驻挂载，RHF 状态跨开合保留；Esc/遮罩/X 都会走 `onOpenChange`，只处理"取消"按钮会漏 |
| **mutation 错误也要 reset**（`openCreate/openEdit/关闭` 均调 `save.reset()`） | 否则上次失败的内联 Alert 会在下次打开时串场 |
| **失败 = 内联 Alert（详细错误，常驻）+ 简短 toast** | 只 toast 会转瞬即逝被漏看；只 Alert 在长表单里可能不在视口 |
| **条件隐藏的字段要清空** | 如勾选 CSR 模式时清 `domains`——否则残留值被一起提交，证书记录与真实 SAN 不符 |
| `useMutation` 返回值**每次渲染都是新对象** | effect 依赖它会把 `reset()` 变成渲染循环（测试直接挂起）。**必须解构出绑定的 `reset()`** 再进 deps |
| **必填项一律标红星** | 用 `<Label required>`（`components/ui/label.tsx` 的 `required` prop，内部渲染 `<span className="text-destructive"> *</span>`）。不要手写星号——全仓曾仅 1 处手写、66 处必填字段漏标 |
| **条件必填按渲染期可知的条件传** | 如 `required={!csrMode}`；跨字段矩阵（部署目标的 type × scene）抽成纯函数，**zod superRefine 与 Label 红星共用同一函数**，并配防漂移测试（见 §4） |

### 2.5 列表页四件套（v1.2.0 起强制）

```tsx
const { data, isLoading, isFetching, refetch } = useQuery({ queryKey: ["xxx"], … })
const { refreshing, refresh } = useTableRefresh(refetch)
…
<CardHeader …>
  <RefreshButton fetching={isFetching} onClick={refresh} />   {/* 放按钮组第一位 */}
</CardHeader>
<CardContent>
  <div className="relative">                    {/* 遮罩锚点，必须 */}
    <TableRefreshOverlay active={refreshing} />
    <Table>…</Table>
  </div>
</CardContent>
```

- 遮罩只包**用户主动点击**的刷新：`isFetching` 在带 `refetchInterval` 的页面（任务日志/Agent，5s）周期性为真，直接驱动遮罩会让表格每 5 秒闪一次。
- `useTableRefresh` 内置**最短展示 400ms**（本地 API 几毫秒返回，遮罩一闪而过用户无感）与 `try/catch/finally`（失败也收遮罩，且**不向上抛**——`onClick` 场景会是未处理 rejection）。
- 加载/空态/错误态都在 `TableBody` 内用 `colSpan` 行表达（空态可加 lucide 图标）；`colSpan` 必须等于列数。

### 2.6 其他速查

- **Badge 状态色**：`variant="outline"` + 语义 class（成功 `bg-green-500/15 text-green-600 dark:text-green-400`；警告 amber；错误 red；中性 zinc；运行中 blue）。
- **Alert**：`components/ui/alert.tsx`，`variant="destructive"` 用于错误；自带 `role="alert"`，测试直接 `findByRole("alert")`。
- **toast（sonner）**：成功/简短提醒；错误详细文案走内联 Alert，toast 只留一句摘要。
- **删除确认**：全仓统一原生 `confirm()`（`alert-dialog.tsx` 存在但无业务引用）；提示文案带上影响面（如"N 张证书引用了该域名"）。
- **时间**：接口给 ISO string，组件内 `new Date(x).toLocaleDateString()/toLocaleString()`；倒计时类用 `whitespace-nowrap`。

### 2.7 长文本 / URL 输入

**URL、签名串、证书 PEM 等长内容一律用 `Textarea`，不要用单行 `Input`**——单行显示不全、无法查看全文、粘贴后难以核对。

- `Textarea` 自带 `field-sizing-content`（随内容自动增高，最小 `min-h-16`），无需手写 rows
- URL 类加 `className="font-mono text-xs"`：等宽更易辨认参数分隔
- **提交时必须 trim**：textarea 会带入粘贴来的换行/首尾空白，URL 带换行直接导致请求失败。在 `buildConfig` 之类"提交时读值"的逻辑里按字段类型 trim（密钥类字段不 trim，避免改变用户输入）
- 已应用：通知渠道 4 个 Webhook URL（webhook/wecom/dingtalk/feishu）、部署目标 Webhook URL。字段驱动型表单（`fieldsByType`）加一个 `type: "textarea"` 分支即可复用

---

## 3. 数据层与表单

### 3.1 TanStack Query

- **queryKey = 复数 kebab-case，与 REST 路径同名**（`["dns-accounts"]`、`["domains"]`）；分页/筛选作为附加 key 元素（`["task-logs", page]`）。
- `api` 响应拦截器已解包信封，业务直接 `res.data`；错误文案用 `getErrorMessage(err)`（读信封 `message`，有 fallback）。
- **invalidateQueries 用精确 key**，不要全量 invalidate；跨页联动才列多个 key。
- 版本类"不变"数据可 `staleTime: Number.POSITIVE_INFINITY`（如 `["version"]`）；可能失败的查询配 `retry: false`。

### 3.2 react-hook-form + zod

- schema 命名 `XxxForm`（`z.infer` 同步）；跨字段校验用 `superRefine`；动态字段用 `catchall(z.unknown())` + `superRefine`（`channels-page.tsx` 模式）。
- **`DEFAULT_VALUES` 常量**：`useForm` 的 `defaultValues` 与所有 `reset()` 共用一处，避免两份漂移。
- **受控组件（watch + setValue）要想清楚"提交语义"**：何时落值（回车/失焦/面板关闭）、空值代表什么（清空 or 保持不变）。`DomainInput` 的失焦消失 bug 就源于此。
- 提交时读值优先 `getValues`，避免渲染期 `watch` 循环（`channels-page.tsx` 的 `buildConfig`）。

---

## 4. 测试实践（vitest + @testing-library）

| 场景 | 做法 |
|---|---|
| mock api | `vi.mock("@/lib/api", …)`；**`getErrorMessage` 要透传真实 message**（`(e) => e?.message ?? fallback`），否则内联 Alert 的断言是"假强"（断言到的是 mock 常量） |
| 组件用了 `useNavigate` | 必须包 `<MemoryRouter>`；验证跳转用 `MemoryRouter initialEntries + <Routes><Route/></Routes>` 占位元素，**不要断言 `window.location`**（MemoryRouter 不改写它） |
| 遮罩断言 | `role="status"`（`TableRefreshOverlay` 自带） |
| Alert 断言 | `role="alert"` + `toHaveTextContent(详细文案)` |
| 受控组件 | 用**有状态 harness**（`useState` 包一层）验证 chip/值渲染；只断言 `onChange` 会漏掉展示 bug |
| `findByText` 撞车 | 多个匹配会抛错——用 `getAllByText(...).length` 或更精确的 `getByRole("button", { name: "移除 xxx" })` |
| 弹层打开后外部元素被 `aria-hidden` | `getByRole` 找不到 trigger——用 `container.querySelector('[aria-label="…"]')` 点击 |
| 异步查询 | 先 `await screen.findByText(对话框标题)`，再 `await findBy…` 等数据驱动的 UI（`getByText` 太早会漏） |
| 删除确认 | `vi.spyOn(window, "confirm").mockReturnValue(true)`，并可断言提示文案包含影响面 |
| 查询类 mock 挂起 | 需要可控 Promise：`new Promise(resolve => { release = resolve })`，配合 `act` |

---

## 5. 代码模板

### 5.1 新 CRUD 页骨架

```
features/<域>/<name>-page.tsx
  ├─ useQuery + useTableRefresh            （§2.5 四件套）
  ├─ zod schema + DEFAULT_VALUES           （§3.2）
  ├─ openCreate/openEdit：reset(DEFAULT) + save.reset()
  ├─ save mutation：onSuccess invalidate + reset + 关窗；onError 仅 toast（简单表单）
  │    或 onError 仅 toast.error("…失败") + 表单顶部内联 Alert（长/耗时表单）
  ├─ 统计条（可选，前端从列表计算）
  ├─ Table：relative 包裹 + 遮罩 + 加载/空/错误三态
  ├─ Dialog：onOpenChange 关闭即 reset（覆盖 Esc/遮罩/X）
  └─ 删除：confirm(含影响面) + mutate
路由：router.tsx 追加 `{ path: "<name>", element: <XxxPage /> }`
导航：lib/nav.ts 分组内追加（面包屑由 app-layout 按 nav 最长前缀自动派生，无需手动注册）
```

### 5.2 新 Select 检查单

- [ ] `items` 已传（或函数子映射存在），选中后显示的是中文标签
- [ ] 表单内 `className="w-full"`
- [ ] `onValueChange` 的 `v ?? 默认值`
- [ ] 选项可能为空 → `EmptySelectHint` 或至少 placeholder 可读

### 5.3 新表单检查单

- [ ] 每个必填字段的 Label 带 `required`（红星）；条件必填按渲染期条件传
- [ ] 跨字段必填矩阵：`isXxxRequired()` 纯函数 + zod 与 Label 共用 + 防漂移测试
- [ ] `DEFAULT_VALUES` 常量被 `defaultValues` 与所有 `reset()` 共用
- [ ] 打开时 reset（含 mutation 错误）；`onOpenChange` 关闭时 reset（覆盖 Esc/遮罩/X）
- [ ] 失败有内联 Alert（长/耗时表单）或 toast（简单表单）
- [ ] 条件隐藏的字段在隐藏时清空
- [ ] 长文本/URL 用 Textarea（§2.7）

---

## 6. 坑位清单（踩过的，按现象检索）

| 现象 | 根因 | 规则 |
|---|---|---|
| 下拉选中后显示 `must_have_cert` 这类原始值 | `SelectValue` 未映射 label | 传 `items`（§2.1） |
| 长 URL 在单行 Input 里显示不全 | 该用 Textarea 的场景用了 Input | URL/PEM/长串用 Textarea + 提交 trim（§2.7） |
| 空下拉塌成一条细线 | `SelectTrigger` 默认 `w-fit` | 表单内 `w-full`（§2.1） |
| 选完域名失焦后文字消失、只剩 × | 单域名提交清了输入框但值没变、同步 effect 不跑 | 提交时同步设置展示值（§2.3 规则 4） |
| 面板内输入过滤词后点选项，选中项丢失、多了个垃圾 chip | 关闭回调把过滤词当域名提交 | 过滤词关闭时丢弃/恢复（§2.3 规则 2） |
| Esc 把已填主机名清空了 | base-ui 在 `!mounted` 时把 Esc 当清空选中值 | 自行处理 Esc + 忽略置空变更（§2.3 规则 3） |
| 弹窗内键入长内容时整个表单"点不动/读不到" | combobox 弹层把外部置 `aria-hidden` | 面板只由按钮开启（§2.3 规则 1） |
| 再次点 ▾ 面板不关闭 | trigger 点击与 base-ui 内部开关双触发 | ref 标记按钮来源（§2.3 规则 5） |
| 弹窗重开还显示上次的失败 Alert | mutation 错误没 reset | 打开/关闭都 `save.reset()`（§2.4） |
| 测试整体挂起（无输出、超时） | effect 依赖整个 `useMutation` 返回值形成渲染循环 | deps 用绑定的 `reset()`（§2.4） |
| 表格每 5 秒闪一次遮罩 | `isFetching` 被轮询置真 | 遮罩只包主动刷新（§2.5） |
| 刷新遮罩一闪而过看不到 | 本地 API 毫秒级返回 | 最短展示 400ms（§2.5） |
| 刷新失败时控制台 unhandled rejection | hook 把 refetch 的 rejection 抛到 onClick | catch 吞掉、finally 收遮罩（§2.5） |
| 测试断言"文档已更新"失败但实际已更新 | **PowerShell 控制台编码损坏中文检索字面量**，`.Contains("中文")` 恒 false | 校验文件内容用**落盘 + 哈希/SHA256 比对**或 `[System.IO.File]::ReadAllText(path, UTF8)`，**不要用内联中文 grep/Contains** |
| 必填字段没标红星，用户不知道该填什么 | 手写 Label 忘加星号（曾 66 处漏标） | 用 `<Label required>`；跨字段矩阵抽纯函数供 zod 与 Label 共用（§2.4） |
| zod 报错字段与 Label 红星不一致（漂移） | 两处各写一份规则 | 共用 `isXxxRequired()` + 防漂移测试（矩阵遍历 type×scene，比对 safeParse 报错字段） |
| PS 5.1 跑脚本报解析错误 | 缺 BOM/CRLF | 写脚本后转 UTF8 **with BOM** + CRLF 再 `powershell -File` |
| jsdom 里行为与浏览器不一致（base-ui mounted/过渡） | 过渡状态依赖动画/RAF | 涉及第三方内部状态的交互，**自行兜底**（如 Esc），别依赖其内部标志 |
| `tsc` 报 unused import/变量 | 重构后残留 | `noUnusedLocals` 开着，提交前跑 `tsc -b` |
| edit 工具匹配含中文的长段失败 | 编码/不可见字符 | 缩短 `oldString`、加更多上文，或用 PowerShell 字节级替换 |

---

## 7. 变更记录

| 版本 | 变更 |
|---|---|
| v1（2026-11-03） | 首版。沉淀自 v1.2.0 列表页体验优化、域名管理（D1–D4）+ 独立审计修复、IIS SNI 域名选择器及其 3 个回归修复、空下拉引导、Select label 修复 |
| v1.1（2026-11-03） | 新增 §2.7 长文本/URL 输入约定（Textarea + 提交 trim + font-mono），坑位清单补对应条目 |
| v1.2（2026-11-03） | 新增必填标识约定（`<Label required>` + 跨字段矩阵共用纯函数 + 防漂移测试）；新增 §5.3 新表单检查单；坑位清单补 2 条 |
