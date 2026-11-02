import { RefreshCcwIcon } from "lucide-react"

import { cn } from "@/lib/utils"

interface TableRefreshOverlayProps {
  /** 是否显示遮罩（通常传 useTableRefresh 返回的 refreshing） */
  active: boolean
  /** 提示文案，默认「刷新中...」 */
  label?: string
  className?: string
}

/**
 * 表格刷新遮罩：手动刷新时盖在表格上方（半透明底 + 旋转图标 + 文案），
 * 旧数据保持可见不闪空，松手即恢复。放在 position:relative 的容器内，
 * 通常直接包在 <Table> 外层：
 *
 *   <div className="relative">
 *     <TableRefreshOverlay active={refreshing} />
 *     <Table>...</Table>
 *   </div>
 */
export function TableRefreshOverlay({
  active,
  label = "刷新中...",
  className,
}: TableRefreshOverlayProps) {
  if (!active) return null
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "absolute inset-0 z-10 flex animate-in items-center justify-center gap-2",
        "rounded-md bg-background/60 text-xs text-muted-foreground backdrop-blur-[1px]",
        "fade-in duration-200",
        className,
      )}
    >
      <RefreshCcwIcon className="size-4 animate-spin" />
      {label}
    </div>
  )
}
