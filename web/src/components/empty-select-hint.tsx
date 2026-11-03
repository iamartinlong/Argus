import { Plus } from "lucide-react"

import { Button } from "@/components/ui/button"

interface EmptySelectHintProps {
  /** 说明文案：为什么现在选不了 */
  text: string
  /** 按钮文字，如「去创建渠道账户」 */
  actionLabel: string
  /** 点击后跳转（通常为关闭当前弹窗 + navigate 到对应管理页） */
  onAction: () => void
}

/**
 * 下拉选项为空时的引导块：说明 + 跳转按钮。
 * 用于"没有可用选项、必须先去别处创建"的表单场景——比空下拉或禁用项更可操作。
 */
export function EmptySelectHint({ text, actionLabel, onAction }: EmptySelectHintProps) {
  return (
    <div className="rounded-md border border-dashed p-3">
      <p className="text-sm text-muted-foreground">{text}</p>
      <Button type="button" variant="outline" size="sm" className="mt-2" onClick={onAction}>
        <Plus className="mr-1 size-4" />
        {actionLabel}
      </Button>
    </div>
  )
}
