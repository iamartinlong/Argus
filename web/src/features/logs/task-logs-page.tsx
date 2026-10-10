import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { Download, Eye } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, getErrorMessageAsync, type Page } from "@/lib/api"

interface ScanSummary {
  scanned: number
  expiring?: { domain: string; days_left: number; status: string; provider: string }[]
  renew_triggered?: number
  renew_success?: string[]
  renew_failed?: { domain: string; error: string }[]
  renew_skipped?: { domain: string; reason: string }[]
  notify_sent?: number
  deploy_retried?: number
  deploy_retried_ok?: number
  agent_cleaned?: number
  duration_ms?: number
}

interface TaskLog {
  id: string
  type: string
  status: string
  detail: string
  summary?: string
  started_at: string
  finished_at: string
}

const typeNames: Record<string, string> = {
  scan_certificates: "证书扫描",
  auto_renew: "自动续期",
  expiry_notify: "到期提醒",
  notify_retry: "通知重试",
}

function parseSummary(s?: string): ScanSummary | null {
  if (!s) return null
  try {
    const v = JSON.parse(s) as ScanSummary
    if (typeof v.scanned === "number") return v
    return null
  } catch {
    return null
  }
}

function summaryBadge(log: TaskLog): string {
  const sum = parseSummary(log.summary)
  if (!sum) return log.detail || "-"
  if (log.type === "scan_certificates") {
    const parts: string[] = [`扫描${sum.scanned}`]
    if (sum.expiring?.length) parts.push(`到期${sum.expiring.length}`)
    if (sum.renew_triggered) parts.push(`续期${sum.renew_success?.length ?? 0}/${sum.renew_failed?.length ?? 0}`)
    if (sum.notify_sent) parts.push(`通知${sum.notify_sent}`)
    return parts.join(" | ")
  }
  return log.detail || "-"
}

export function TaskLogsPage() {
  const [typeFilter, setTypeFilter] = useState<string>("all")
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [page, setPage] = useState(0)
  const limit = 20
  const [detailLog, setDetailLog] = useState<TaskLog | null>(null)

  const { data } = useQuery({
    queryKey: ["task-logs", page],
    queryFn: async () => {
      const res = await api.get<Page<TaskLog>>(`/task-logs?limit=${limit}&offset=${page * limit}`)
      return res.data
    },
    refetchInterval: 5000,
  })
  const logs = data?.items ?? []
  const total = data?.total ?? 0
  const filtered = logs.filter((l) => {
    if (typeFilter !== "all" && l.type !== typeFilter) return false
    if (statusFilter !== "all" && l.status !== statusFilter) return false
    return true
  })

  const exportCSV = async () => {
    try {
      const res = await api.get<Blob>("/task-logs/export", { responseType: "blob" })
      const url = URL.createObjectURL(res.data)
      const a = document.createElement("a")
      a.href = url
      a.download = "task-logs.csv"
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(await getErrorMessageAsync(err))
    }
  }

  const detailSummary = detailLog ? parseSummary(detailLog.summary) : null

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>任务日志</CardTitle>
        <div className="flex gap-2 items-center">
          <Select value={typeFilter} onValueChange={(v) => setTypeFilter(v ?? "all")}>
            <SelectTrigger className="w-36"><SelectValue placeholder="类型" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              <SelectItem value="scan_certificates">证书扫描</SelectItem>
              <SelectItem value="auto_renew">自动续期</SelectItem>
              <SelectItem value="expiry_notify">到期提醒</SelectItem>
            </SelectContent>
          </Select>
          <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v ?? "all")}>
            <SelectTrigger className="w-28"><SelectValue placeholder="状态" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部</SelectItem>
              <SelectItem value="success">成功</SelectItem>
              <SelectItem value="failed">失败</SelectItem>
              <SelectItem value="running">运行中</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={exportCSV}>
            <Download className="mr-2 size-4" />
            导出 CSV
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>任务</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>摘要/详情</TableHead>
              <TableHead>开始时间</TableHead>
              <TableHead>完成时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {!filtered || filtered.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                  暂无任务记录（调度器将周期性运行）
                </TableCell>
              </TableRow>
            ) : (
              filtered.map((l) => (
                <TableRow key={l.id} className="cursor-pointer" onClick={() => setDetailLog(l)}>
                  <TableCell className="font-medium">{typeNames[l.type] ?? l.type}</TableCell>
                  <TableCell>
                    {l.status === "success" ? (
                      <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">成功</Badge>
                    ) : l.status === "failed" ? (
                      <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">失败</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-blue-500/15 text-blue-600 dark:text-blue-400">运行中</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground max-w-[420px] truncate" title={l.detail || l.summary || ""}>
                    {summaryBadge(l)}
                  </TableCell>
                  <TableCell>{new Date(l.started_at).toLocaleString()}</TableCell>
                  <TableCell>
                    {l.finished_at ? new Date(l.finished_at).toLocaleString() : "-"}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="sm" onClick={(e) => { e.stopPropagation(); setDetailLog(l) }}>
                      <Eye className="mr-1 size-4" />详情
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        {total > limit && (
          <div className="flex justify-between items-center mt-4 text-sm text-muted-foreground">
            <span>共 {total} 条</span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))}>上一页</Button>
              <Button variant="outline" size="sm" disabled={(page + 1) * limit >= total} onClick={() => setPage((p) => p + 1)}>下一页</Button>
            </div>
          </div>
        )}
      </CardContent>
      <Dialog open={!!detailLog} onOpenChange={(open) => { if (!open) setDetailLog(null) }}>
        <DialogContent className="sm:max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>任务详情 — {detailLog ? (typeNames[detailLog.type] ?? detailLog.type) : ""}</DialogTitle>
            <DialogDescription>
              {detailLog ? new Date(detailLog.started_at).toLocaleString() : ""} {detailLog?.status === "failed" ? "失败" : detailLog?.status === "success" ? "成功" : ""}
              {detailSummary?.duration_ms ? ` · 耗时 ${detailSummary.duration_ms}ms` : ""}
            </DialogDescription>
          </DialogHeader>
          {detailLog && (
            <div className="space-y-4 text-sm">
              {detailLog.detail && (
                <div>
                  <p className="font-medium">原始详情</p>
                  <p className="font-mono text-xs break-all bg-muted p-2 rounded">{detailLog.detail}</p>
                </div>
              )}
              {detailSummary ? (
                <>
                  <div className="grid grid-cols-3 gap-2 text-center">
                    <div className="border rounded p-2"><p className="text-muted-foreground">扫描</p><p className="text-lg font-bold">{detailSummary.scanned}</p></div>
                    <div className="border rounded p-2"><p className="text-muted-foreground">续期触发</p><p className="text-lg font-bold">{detailSummary.renew_triggered ?? 0}</p></div>
                    <div className="border rounded p-2"><p className="text-muted-foreground">通知</p><p className="text-lg font-bold">{detailSummary.notify_sent ?? 0}</p></div>
                  </div>
                  {detailSummary.expiring && detailSummary.expiring.length > 0 && (
                    <div>
                      <p className="font-medium">到期证书 ({detailSummary.expiring.length})</p>
                      <ul className="list-disc pl-4 space-y-1">
                        {detailSummary.expiring.map((e, i) => (
                          <li key={i} className="font-mono text-xs">{e.domain} — {e.days_left}天后到期 ({e.status}, {e.provider})</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {detailSummary.renew_success && detailSummary.renew_success.length > 0 && (
                    <div>
                      <p className="font-medium text-green-600">续期成功 ({detailSummary.renew_success.length})</p>
                      <ul className="list-disc pl-4">
                        {detailSummary.renew_success.map((d) => <li key={d} className="font-mono text-xs">{d}</li>)}
                      </ul>
                    </div>
                  )}
                  {detailSummary.renew_failed && detailSummary.renew_failed.length > 0 && (
                    <div>
                      <p className="font-medium text-red-600">续期失败 ({detailSummary.renew_failed.length})</p>
                      <ul className="list-disc pl-4 space-y-1">
                        {detailSummary.renew_failed.map((f, i) => (
                          <li key={i} className="font-mono text-xs">{f.domain}: {f.error}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {detailSummary.renew_skipped && detailSummary.renew_skipped.length > 0 && (
                    <div>
                      <p className="font-medium text-muted-foreground">跳过 ({detailSummary.renew_skipped.length})</p>
                      <ul className="list-disc pl-4">
                        {detailSummary.renew_skipped.map((s, i) => (
                          <li key={i} className="font-mono text-xs">{s.domain} ({s.reason})</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </>
              ) : (
                <p className="text-muted-foreground">无结构化摘要（旧记录或非扫描任务）</p>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
