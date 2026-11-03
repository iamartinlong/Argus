import * as React from "react"

import { cn } from "@/lib/utils"

interface LabelProps extends React.ComponentProps<"label"> {
  /** 必填字段：在文案后渲染红色星号（表单可发现性） */
  required?: boolean
}

function Label({ className, required, children, ...props }: LabelProps) {
  return (
    <label
      data-slot="label"
      className={cn(
        "flex items-center gap-2 text-sm leading-none font-medium select-none group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
        className
      )}
      {...props}
    >
      {children}
      {required && (
        <span className="text-destructive" aria-hidden>
          {" *"}
        </span>
      )}
    </label>
  )
}

export { Label }
