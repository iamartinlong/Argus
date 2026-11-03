import { useEffect, useMemo, useRef, useState } from "react"
import { Combobox } from "@base-ui/react/combobox"
import { useQuery } from "@tanstack/react-query"
import { Check, ChevronDown, X } from "lucide-react"

import { api } from "@/lib/api"
import type { DomainLedgerItem } from "@/lib/types"
import { cn } from "@/lib/utils"

/** 与后端一致的域名规范化：小写、去首尾空白、去尾点 */
function normalize(token: string): string {
  return token.trim().toLowerCase().replace(/\.$/, "")
}

/**
 * 解析输入文本为域名 token：逗号/空格分隔，规范化，去重保序。
 * 导出供测试与表单值解析复用。
 */
export function parseDomainTokens(input: string): string[] {
  if (!input) return []
  const out: string[] = []
  for (const raw of input.split(/[\s,]+/)) {
    const d = normalize(raw)
    if (d && !out.includes(d)) out.push(d)
  }
  return out
}

interface DomainInputProps {
  /** 域名字符串：多域名模式为逗号/空格分隔；单域名模式为单个域名 */
  value: string
  onChange: (value: string) => void
  /**
   * 单域名模式（如 IIS SNI 主机名）：不显示 chips，输入框直接展示当前值，
   * 从面板选择即替换；多域名模式（签发证书）显示 chips 可累加。
   */
  single?: boolean
  placeholder?: string
  className?: string
}

/**
 * 域名录入组件（"输入或选择"）：
 * 左侧输入框自由输入，右侧按钮弹出域名台账多选面板，选中即录入。
 * 基于 base-ui combobox 原语；多域名模式对外契约是逗号分隔 string，
 * 单域名模式是单个域名 string，均不侵入表单结构。
 */
export function DomainInput({
  value,
  onChange,
  single = false,
  placeholder = "example.com, www.example.com",
  className,
}: DomainInputProps) {
  const tokens = useMemo(() => parseDomainTokens(value), [value])
  const [inputValue, setInputValue] = useState("")
  const [open, setOpen] = useState(false)
  const [highlighted, setHighlighted] = useState<string | null>(null)
  // 标记本次开合请求是否来自右侧按钮：base-ui 在输入/聚焦时也会请求打开，
  // 那些请求要忽略（否则输入长域名时面板弹出会遮住表单）；按钮请求才放行。
  const openByButton = useRef(false)

  const { data: ledger } = useQuery({
    queryKey: ["domains"],
    queryFn: async () => {
      const res = await api.get<DomainLedgerItem[]>("/domains")
      return res.data
    },
  })
  const items = useMemo(() => (ledger ?? []).map((d) => d.name), [ledger])

  const emit = (next: string[]) => onChange(next.join(", "))

  /** 把输入框里的文本落为域名（回车/失焦时触发） */
  const commitInput = () => {
    const parsed = parseDomainTokens(inputValue)
    setInputValue("")
    if (single) {
      // 单域名：取第一个 token（无输入则不动，避免把已有值清掉）
      if (parsed.length > 0) onChange(parsed[0])
      return
    }
    if (parsed.length === 0) return
    const merged = [...tokens]
    for (const d of parsed) {
      if (!merged.includes(d)) merged.push(d)
    }
    emit(merged)
  }

  // 单域名模式：输入框内容即当前值（表单回填/外部重置时同步）
  useEffect(() => {
    if (single) setInputValue(value)
  }, [single, value])

  return (
    <Combobox.Root
      multiple={!single}
      items={items}
      value={single ? (tokens[0] ?? null) : tokens}
      onValueChange={(next) => {
        if (single) {
          // 单域名模式：忽略"置空"变更——base-ui 在面板未 mounted 时会把 Esc 当作
          // "清空选中值"，在表单字段里这会误删已填主机名。用户清空走清除按钮或
          // Backspace（均直接调用 onChange），不经过这里。
          if (next == null || next === "") return
          onChange(next as string)
        } else {
          emit((next as string[]) ?? [])
        }
        setInputValue("")
      }}
      inputValue={inputValue}
      onInputValueChange={setInputValue}
      open={open}
      onOpenChange={(next) => {
        // 输入/聚焦导致的"打开"请求一律忽略（否则输入长域名时面板弹出会遮住表单）；
        // 关闭请求（Esc/点外部/选中后面板自动关/再次点按钮）一律接受。
        if (next && !openByButton.current) return
        openByButton.current = false
        setOpen(next)
        setHighlighted(null)
        // 面板打开期间的输入是"过滤词"而非待录入域名：多域名模式关闭时丢弃；
        // 单域名模式恢复显示当前值（过滤词不应顶掉已选主机名）
        if (!next) setInputValue(single ? value : "")
      }}
      onItemHighlighted={(v) => setHighlighted((v as string | undefined) ?? null)}
    >
      <div
        className={cn(
          "flex min-h-8 w-full flex-wrap items-center gap-1 rounded-lg border border-input bg-transparent px-2 py-1 text-sm",
          "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
          className,
        )}
      >
        {!single &&
          tokens.map((t) => (
            <span
              key={t}
              className="inline-flex items-center gap-1 rounded-md bg-secondary px-1.5 py-0.5 text-xs text-secondary-foreground"
            >
              {t}
              <button
                type="button"
                aria-label={`移除 ${t}`}
                title="移除"
                className="rounded-sm text-muted-foreground hover:text-foreground"
                onClick={() => emit(tokens.filter((x) => x !== t))}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        {value !== "" && (
          <button
            type="button"
            aria-label="清空全部域名"
            title="清空"
            className="inline-flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
            onClick={() => {
              onChange("")
              setInputValue("")
            }}
          >
            <X className="size-3.5" />
          </button>
        )}
        <Combobox.Input
          className="min-w-28 flex-1 bg-transparent py-0.5 outline-none placeholder:text-muted-foreground"
          placeholder={value === "" ? placeholder : ""}
          onBlur={() => {
            // 失焦即提交（面板未开时）：避免"只输入不回车就点提交"丢字
            if (!open) commitInput()
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              // 自行处理 Esc：关闭面板 + 恢复输入显示，不依赖 base-ui 内部
              // mounted 状态（其在 jsdom/真实浏览器表现不一致，且会把 Esc 当作清空选中值）
              if (open) setOpen(false)
              setInputValue(single ? value : "")
              return
            }
            if (e.key === "Enter") {
              // 有高亮项时交给 combobox 选中；否则把输入文本落为域名
              if (highlighted != null) return
              e.preventDefault() // 阻止表单提交
              commitInput()
              return
            }
            // 空输入框按 Backspace：多域名删最后一个 chip，单域名清空
            if (e.key === "Backspace" && inputValue === "" && value !== "") {
              e.preventDefault()
              if (single) {
                onChange("")
              } else {
                emit(tokens.slice(0, -1))
              }
            }
          }}
        />
        <Combobox.Trigger
          type="button"
          aria-label="从域名台账选择"
          title="从域名台账选择"
          onClick={() => {
            openByButton.current = true
            setOpen((o) => !o)
          }}
          className="ml-auto inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <ChevronDown className="size-4" />
        </Combobox.Trigger>
      </div>
      <Combobox.Portal>
        <Combobox.Positioner sideOffset={4} className="z-50">
          <Combobox.Popup className="max-h-60 w-[var(--anchor-width)] overflow-y-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg">
            <Combobox.List>
              {(item: string) => (
                <Combobox.Item
                  key={item}
                  value={item}
                  className="flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 text-sm data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                >
                  <Combobox.ItemIndicator className="flex size-4 shrink-0 items-center justify-center">
                    <Check className="size-3.5" />
                  </Combobox.ItemIndicator>
                  <span className="truncate">{item}</span>
                </Combobox.Item>
              )}
            </Combobox.List>
            <Combobox.Empty className="px-2 py-3 text-center text-xs text-muted-foreground">
              暂无匹配域名，可先去「域名管理」登记
            </Combobox.Empty>
          </Combobox.Popup>
        </Combobox.Positioner>
      </Combobox.Portal>
    </Combobox.Root>
  )
}
