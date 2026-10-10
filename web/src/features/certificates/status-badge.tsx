import { Badge } from "@/components/ui/badge"

const statusMap: Record<string, { label: string; className: string }> = {
  pending: { label: "签发中", className: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  valid: { label: "有效", className: "bg-green-500/15 text-green-600 dark:text-green-400" },
  expiring: { label: "即将到期", className: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  expired: { label: "已过期", className: "bg-red-500/15 text-red-600 dark:text-red-400" },
  revoked: { label: "已吊销", className: "bg-zinc-500/15 text-zinc-600 dark:text-zinc-400" },
  failed: { label: "失败", className: "bg-red-500/15 text-red-600 dark:text-red-400" },
}

export function CertStatusBadge({ status }: { status: string }) {
  const meta = statusMap[status] ?? { label: status, className: "bg-muted text-muted-foreground" }
  return <Badge variant="outline" className={meta.className}>{meta.label}</Badge>
}

export function OrderStatusBadge({ status }: { status: string }) {
  if (status === "success") return <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">成功</Badge>
  if (status === "failed") return <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">失败</Badge>
  return <Badge variant="outline" className="bg-blue-500/15 text-blue-600 dark:text-blue-400">进行中</Badge>
}
