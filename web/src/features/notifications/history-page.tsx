import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { ChevronLeft, ChevronRight, Download, Search, SendHorizonal } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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

interface SendLog {
  id: string
  cert_id: string
  rule_id: string
  rule_name: string
  channel_id: string
  channel_name: string
  event: string
  status: string
  detail: string
  sent_at: string
}

const PAGE_SIZE = 50

const eventNames: Record<string, string> = {
  cert_expiring: "到期提醒",
  renew_success: "续期成功",
  renew_failed: "续期失败",
  issue_success: "签发成功",
  issue_failed: "签发失败",
  deploy_success: "部署成功",
  deploy_failed: "部署失败",
}

export function NotificationHistoryPage() {
  const [status, setStatus] = useState("all")
  const [keyword, setKeyword] = useState("")
  const [applied, setApplied] = useState({ status: "all", keyword: "" })
  const [page, setPage] = useState(0)

  const { data, isLoading } = useQuery({
    queryKey: ["notification-send-logs", applied, page],
    queryFn: async () => {
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(page * PAGE_SIZE) })
      if (applied.status !== "all") params.set("status", applied.status)
      if (applied.keyword) params.set("keyword", applied.keyword)
      const res = await api.get<Page<SendLog>>(`/notification-send-logs?${params.toString()}`)
      return res.data
    },
  })
  const logs = data?.items

  const search = () => {
    setPage(0)
    setApplied({ status, keyword: keyword.trim() })
  }

  const exportCSV = async () => {
    try {
      const params = new URLSearchParams({ limit: "10000" })
      if (applied.status !== "all") params.set("status", applied.status)
      if (applied.keyword) params.set("keyword", applied.keyword)
      const res = await api.get<Blob>(`/notification-send-logs/export?${params.toString()}`, { responseType: "blob" })
      const url = URL.createObjectURL(res.data)
      const a = document.createElement("a")
      a.href = url
      a.download = "notification-send-logs.csv"
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(await getErrorMessageAsync(err))
    }
  }

  const formatTime = (iso: string) => {
    const d = new Date(iso)
    return d.toLocaleString("zh-CN", { hour12: false })
  }

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>通知发送历史</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs text-muted-foreground">状态</Label>
            <Select value={status} onValueChange={(v) => setStatus(v ?? "all")}>
              <SelectTrigger className="w-40">
                <SelectValue>{(v) => (v === "all" ? "全部状态" : v === "success" ? "成功" : "失败")}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="success">成功</SelectItem>
                <SelectItem value="failed">失败</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs text-muted-foreground">关键字</Label>
            <Input
              className="w-52"
              placeholder="规则/渠道/证书搜索"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && search()}
            />
          </div>
          <div className="flex gap-2">
            <Button onClick={search}>
              <Search className="mr-2 size-4" />
              搜索
            </Button>
            <Button variant="outline" onClick={exportCSV}>
              <Download className="mr-2 size-4" />
              导出 CSV
            </Button>
          </div>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-40">时间</TableHead>
              <TableHead className="w-28">事件</TableHead>
              <TableHead className="w-36">规则</TableHead>
              <TableHead className="w-36">渠道</TableHead>
              <TableHead>证书</TableHead>
              <TableHead className="w-24">状态</TableHead>
              <TableHead className="w-48">详情</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">加载中...</TableCell>
              </TableRow>
            )}
            {!isLoading && (!logs || logs.length === 0) && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground py-10">
                  <SendHorizonal className="mx-auto mb-2 size-8 opacity-50" />
                  暂无发送记录
                </TableCell>
              </TableRow>
            )}
            {logs?.map((l) => (
              <TableRow key={l.id}>
                <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(l.sent_at)}</TableCell>
                <TableCell>
                  <Badge variant="outline">{eventNames[l.event] ?? (l.event || "-")}</Badge>
                </TableCell>
                <TableCell>{l.rule_name || "-"}</TableCell>
                <TableCell>{l.channel_name || "-"}</TableCell>
                <TableCell className="max-w-48 truncate text-muted-foreground" title={l.cert_id}>
                  {l.cert_id || "-"}
                </TableCell>
                <TableCell>
                  {l.status === "success" ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">成功</Badge>
                  ) : (
                    <Badge variant="destructive">失败</Badge>
                  )}
                </TableCell>
                <TableCell className="max-w-48 truncate text-muted-foreground" title={l.detail}>
                  {l.detail || "-"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">
            {data && data.total > 0
              ? `共 ${data.total} 条 · 第 ${page * PAGE_SIZE + 1}-${Math.min((page + 1) * PAGE_SIZE, data.total)} 条`
              : "共 0 条"}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))}>
              <ChevronLeft className="mr-1 size-4" />
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!data || (page + 1) * PAGE_SIZE >= data.total}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
              <ChevronRight className="ml-1 size-4" />
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
