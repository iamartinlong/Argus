import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { CircleAlertIcon, GlobeIcon, Pencil, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, getErrorMessage } from "@/lib/api"
import { RefreshButton } from "@/components/refresh-button"
import { TableRefreshOverlay } from "@/components/table-refresh-overlay"
import { useTableRefresh } from "@/hooks/use-table-refresh"
import type { DomainCoverage, DomainLedgerItem, DomainExpectedStatus } from "@/lib/types"

const domainSchema = z.object({
  name: z.string().min(1, "请输入域名"),
  expected_status: z.enum(["must_have_cert", "monitor_only"]),
  tags: z.string().optional(),
  note: z.string().optional(),
})

type DomainForm = z.infer<typeof domainSchema>

const EXPECTED_LABELS: Record<DomainExpectedStatus, string> = {
  must_have_cert: "必须有证书",
  monitor_only: "仅关注",
}

const COVERAGE_LABELS: Record<DomainCoverage, string> = {
  missing: "缺少证书",
  expiring: "即将到期",
  expired: "已过期",
  covered: "已覆盖",
  monitor: "仅关注",
}

const COVERAGE_CLASSES: Record<DomainCoverage, string> = {
  missing: "bg-red-500/15 text-red-600 dark:text-red-400",
  expiring: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  expired: "bg-red-500/15 text-red-600 dark:text-red-400",
  covered: "bg-green-500/15 text-green-600 dark:text-green-400",
  monitor: "bg-zinc-500/15 text-zinc-600 dark:text-zinc-400",
}

const FILTERS: { value: "all" | DomainCoverage; label: string }[] = [
  { value: "all", label: "全部状态" },
  { value: "missing", label: "缺少证书" },
  { value: "expiring", label: "即将到期" },
  { value: "expired", label: "已过期" },
  { value: "covered", label: "已覆盖" },
  { value: "monitor", label: "仅关注" },
]

// 期望状态选项：items 与下拉项共用，保证触发器显示的是 label 而非 value
const EXPECTED_OPTIONS: { value: DomainExpectedStatus; label: string }[] = [
  { value: "must_have_cert", label: "必须有证书（缺失时标红）" },
  { value: "monitor_only", label: "仅关注（不要求证书）" },
]

const DEFAULT_VALUES: DomainForm = {
  name: "",
  expected_status: "must_have_cert",
  tags: "",
  note: "",
}

export function DomainsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<DomainLedgerItem | null>(null)
  const [filter, setFilter] = useState<"all" | DomainCoverage>("all")

  const { data: items, isLoading, isFetching, refetch } = useQuery({
    queryKey: ["domains"],
    queryFn: async () => {
      const res = await api.get<DomainLedgerItem[]>("/domains")
      return res.data
    },
  })
  const { refreshing, refresh } = useTableRefresh(refetch)

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<DomainForm>({
    resolver: zodResolver(domainSchema),
    defaultValues: DEFAULT_VALUES,
  })

  const openCreate = () => {
    setEditing(null)
    reset(DEFAULT_VALUES)
    save.reset()
    setDialogOpen(true)
  }
  const openEdit = (item: DomainLedgerItem) => {
    setEditing(item)
    reset({
      name: item.name,
      expected_status: item.expected_status,
      tags: item.tags.join(", "),
      note: item.note,
    })
    save.reset()
    setDialogOpen(true)
  }

  const save = useMutation({
    mutationFn: async (values: DomainForm) => {
      const payload = {
        name: values.name,
        expected_status: values.expected_status,
        tags: (values.tags ?? "")
          .split(/[,，]/)
          .map((t) => t.trim())
          .filter(Boolean),
        note: (values.note ?? "").trim(),
      }
      if (editing) await api.put(`/domains/${editing.id}`, payload)
      else await api.post("/domains", payload)
    },
    onSuccess: () => {
      toast.success(editing ? "域名已更新" : "域名已登记")
      queryClient.invalidateQueries({ queryKey: ["domains"] })
      setDialogOpen(false)
      setEditing(null)
      reset(DEFAULT_VALUES)
    },
    onError: () => toast.error(editing ? "更新失败" : "登记失败"),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/domains/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["domains"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const rows = useMemo(
    () => (items ?? []).filter((d) => filter === "all" || d.coverage === filter),
    [items, filter],
  )
  const stats = useMemo(() => {
    const all = items ?? []
    return {
      total: all.length,
      missing: all.filter((d) => d.coverage === "missing").length,
      expiring: all.filter((d) => d.coverage === "expiring" || d.coverage === "expired").length,
      monitor: all.filter((d) => d.coverage === "monitor").length,
    }
  }, [items])

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>域名管理</CardTitle>
        <div className="flex gap-2">
          <RefreshButton fetching={isFetching} onClick={refresh} />
          <Select value={filter} onValueChange={(v) => setFilter((v ?? "all") as "all" | DomainCoverage)} items={FILTERS}>
            <SelectTrigger className="w-36">
              <SelectValue placeholder="覆盖状态" />
            </SelectTrigger>
            <SelectContent>
              {FILTERS.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button onClick={openCreate}>
            <Plus className="mr-2 size-4" />
            登记域名
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {/* 统计条：前端从富化列表计算 */}
        <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <StatTile label="域名总数" value={stats.total} />
          <StatTile label="缺少证书" value={stats.missing} tone="text-red-600 dark:text-red-400" />
          <StatTile label="临近/已过期" value={stats.expiring} tone="text-amber-600 dark:text-amber-400" />
          <StatTile label="仅关注" value={stats.monitor} />
        </div>
        <p className="mb-4 text-sm text-muted-foreground">
          域名台账是"我关心的域名"清单：登记后，签发证书时可从台账选择；覆盖状态由系统按关联证书实时计算
          （通配符证书覆盖其子域）。
        </p>
        <div className="relative">
          <TableRefreshOverlay active={refreshing} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>域名</TableHead>
                <TableHead>覆盖状态</TableHead>
                <TableHead>当前证书</TableHead>
                <TableHead>到期时间</TableHead>
                <TableHead>期望</TableHead>
                <TableHead>标签</TableHead>
                <TableHead>备注</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading && (
                <TableRow>
                  <TableCell colSpan={8} className="text-center text-muted-foreground">
                    加载中...
                  </TableCell>
                </TableRow>
              )}
              {!isLoading && rows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8} className="text-center text-muted-foreground py-8">
                    <GlobeIcon className="mx-auto mb-2 size-8 opacity-50" />
                    {filter === "all" ? "暂无域名，点击「登记域名」开始" : "该状态下暂无域名"}
                  </TableCell>
                </TableRow>
              )}
              {rows.map((d) => (
                <TableRow key={d.id}>
                  <TableCell className="font-medium">{d.name}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className={COVERAGE_CLASSES[d.coverage]}>
                      {COVERAGE_LABELS[d.coverage]}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {d.current_cert ? (
                      <span>
                        {d.current_cert.primary_domain}
                        <span className="ml-1 text-xs">（{d.current_cert.provider}）</span>
                      </span>
                    ) : (
                      "-"
                    )}
                  </TableCell>
                  <TableCell
                    className={
                      d.coverage === "expiring"
                        ? "whitespace-nowrap text-amber-600 font-medium"
                        : "whitespace-nowrap text-muted-foreground"
                    }
                  >
                    {d.current_cert ? (
                      <span>
                        {new Date(d.current_cert.not_after).toLocaleDateString()}
                        {d.days_to_expire != null && (
                          <span className="ml-1 text-xs">({d.days_to_expire}天)</span>
                        )}
                      </span>
                    ) : (
                      "-"
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {EXPECTED_LABELS[d.expected_status] ?? d.expected_status}
                  </TableCell>
                  <TableCell>
                    {d.tags.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {d.tags.map((t) => (
                          <Badge key={t} variant="outline">
                            {t}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      "-"
                    )}
                  </TableCell>
                  <TableCell className="max-w-[160px] truncate text-muted-foreground" title={d.note}>
                    {d.note || "-"}
                  </TableCell>
                  <TableCell className="text-right space-x-1">
                    <Button variant="outline" size="sm" onClick={() => openEdit(d)}>
                      <Pencil className="mr-1 size-4" />
                      编辑
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => {
                        const hint =
                          d.cert_count > 0 ? `（${d.cert_count} 张证书引用了该域名，将保留其上的域名副本）` : ""
                        if (confirm(`确认删除域名 ${d.name}？${hint}`)) remove.mutate(d.id)
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>

      <Dialog
        open={dialogOpen}
        onOpenChange={(o) => {
          // 关闭即清空（覆盖取消按钮/Esc/遮罩/X 所有路径）
          if (!o) {
            reset(DEFAULT_VALUES)
            setEditing(null)
            save.reset()
          }
          setDialogOpen(o)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑域名" : "登记域名"}</DialogTitle>
            <DialogDescription>
              {editing ? "修改域名的期望状态、标签与备注" : "登记一个我关心的域名，后续签发证书时可从台账选择"}
            </DialogDescription>
          </DialogHeader>
          {save.isError && (
            <Alert variant="destructive">
              <CircleAlertIcon className="size-4" />
              <AlertTitle>{editing ? "更新失败" : "登记失败"}</AlertTitle>
              <AlertDescription>{getErrorMessage(save.error)}</AlertDescription>
            </Alert>
          )}
          <form className="space-y-4" onSubmit={handleSubmit((v) => save.mutate(v))}>
            <div className="space-y-2">
              <Label>域名</Label>
              <Input placeholder="example.com 或 *.example.com" {...register("name")} />
              <p className="text-xs text-muted-foreground">自动规范化（小写、去尾点）；支持通配符</p>
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>期望状态</Label>
              <Select
                value={watch("expected_status")}
                onValueChange={(v) => setValue("expected_status", (v ?? "must_have_cert") as DomainExpectedStatus)}
                items={EXPECTED_OPTIONS}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EXPECTED_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>标签（逗号分隔，选填）</Label>
              <Input placeholder="prod, web" {...register("tags")} />
            </div>
            <div className="space-y-2">
              <Label>备注（选填）</Label>
              <Textarea placeholder="如：主站域名，负责人张三" {...register("note")} />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {save.isPending ? "保存中..." : editing ? "保存" : "登记"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function StatTile({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone?: string
}) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`mt-1 text-2xl font-semibold tabular-nums ${tone ?? ""}`}>{value}</p>
    </div>
  )
}
