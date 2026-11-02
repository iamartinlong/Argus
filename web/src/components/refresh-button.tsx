import { RefreshCcwIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface RefreshButtonProps {
  /** 正在请求中（TanStack Query 的 isFetching/refetching）：图标旋转并禁用按钮 */
  fetching?: boolean
  /** 点击刷新（通常传 useQuery 返回的 refetch） */
  onClick: () => void
  /** 无障碍标签，默认「刷新」 */
  label?: string
  className?: string
}

/**
 * 列表页通用刷新按钮：替代"刷浏览器"，点击重新请求当前查询。
 * fetching 期间图标旋转并禁用，避免重复点击。
 */
export function RefreshButton({
  fetching = false,
  onClick,
  label = "刷新",
  className,
}: RefreshButtonProps) {
  return (
    <Button
      type="button"
      variant="outline"
      size="icon"
      title={label}
      aria-label={label}
      disabled={fetching}
      onClick={onClick}
      className={cn(className)}
    >
      <RefreshCcwIcon className={cn("size-4", fetching && "animate-spin")} />
    </Button>
  )
}
