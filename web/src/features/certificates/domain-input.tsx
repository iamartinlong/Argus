import { useMemo, useState } from "react"
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
  /** 逗号/空格分隔的域名字符串（与现有表单字段同构， zod schema 无需改动） */
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
}

/**
 * 域名录入组件（"输入或选择"）：
 * 左侧输入框自由输入（逗号/空格/回车落 chip），右侧按钮弹出域名台账多选面板。
 * 基于 base-ui combobox 原语；对外契约是 string，不侵入表单结构。
 */
export function DomainInput({
  value,
  onChange,
  placeholder = "example.com, www.example.com",
  className,
}: DomainInputProps) {
  const tokens = useMemo(() => parseDomainTokens(value), [value])
  const [inputValue, setInputValue] = useState("")
  const [open, setOpen] = useState(false)
  const [highlighted, setHighlighted] = useState<string | null>(null)

  const { data: ledger } = useQuery({
    queryKey: ["domains"],
    queryFn: async () => {
      const res = await api.get<DomainLedgerItem[]>("/domains")
      return res.data
    },
  })
  const items = useMemo(() => (ledger ?? []).map((d) => d.name), [ledger])

  const emit = (next: string[]) => onChange(next.join(", "))

  /** 把输入框里的文本落为 chip（回车/面板关闭时触发） */
  const commitInput = () => {
    const additions = parseDomainTokens(inputValue)
    setInputValue("")
    if (additions.length === 0) return
    const merged = [...tokens]
    for (const a of additions) {
      if (!merged.includes(a)) merged.push(a)
    }
    emit(merged)
  }

  return (
    <Combobox.Root
      multiple
      // 非 modal：默认 modal 会把外层对话框置为 inert（弹层一开整个表单点不动），
      // 本组件常驻在签发弹窗内，必须关闭 modal 行为
      modal={false}
      items={items}
      value={tokens}
      onValueChange={(next) => {
        emit((next as string[]) ?? [])
        setInputValue("")
      }}
      inputValue={inputValue}
      onInputValueChange={setInputValue}
      open={open}
      onOpenChange={(next) => {
        // 只接受"关闭"请求（Esc/点外部/选中）；输入与聚焦不弹面板——
        // base-ui 对可输入 combobox 会在弹层开启时把外部内容置 aria-hidden，
        // 在签发弹窗内会盖住表单其余部分。面板只由右侧「选择」按钮开启。
        if (next) return
        setOpen(false)
        setHighlighted(null)
        commitInput()
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
        {tokens.map((t) => (
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
        <Combobox.Input
          className="min-w-28 flex-1 bg-transparent py-0.5 outline-none placeholder:text-muted-foreground"
          placeholder={tokens.length === 0 ? placeholder : ""}
          onBlur={() => {
            // 失焦即提交（面板未开时）：避免"只输入不回车就点提交"丢字
            if (!open) commitInput()
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              // 有高亮项时交给 combobox 选中；否则把输入文本落为 chip
              if (highlighted != null) return
              e.preventDefault() // 阻止表单提交
              commitInput()
              return
            }
            // 空输入框按 Backspace 删除最后一个 chip
            if (e.key === "Backspace" && inputValue === "" && tokens.length > 0) {
              e.preventDefault()
              emit(tokens.slice(0, -1))
            }
          }}
        />
        <Combobox.Trigger
          type="button"
          aria-label="从域名台账选择"
          title="从域名台账选择"
          onClick={() => setOpen((o) => !o)}
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
