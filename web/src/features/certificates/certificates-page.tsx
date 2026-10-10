import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import { Plus, RefreshCcw, Rocket, Upload, ShieldCheck } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
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
import { api, getErrorMessage } from "@/lib/api"
import type { Certificate } from "@/lib/types"
import { CertStatusBadge } from "./status-badge"
import { IssueDialog } from "./issue-dialog"
import { ImportDialog } from "./import-dialog"

interface DeployTarget {
  id: string
  name: string
  type: string
}

interface BatchResult {
  success: number
  failed: { id: string; error: string }[]
}

export function CertificatesPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [issueOpen, setIssueOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [deployTarget, setDeployTarget] = useState("")
  const [selected, setSelected] = useState<string[]>([])
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Certificate | null>(null)
  const [renewOpen, setRenewOpen] = useState(false)

  const { data: certs, isLoading } = useQuery({
    queryKey: ["certificates"],
    queryFn: async () => {
      const res = await api.get<Certificate[]>("/certificates")
      return res.data
    },
  })

  const { data: targets } = useQuery({
    queryKey: ["deploy-targets"],
    queryFn: async () => {
      const res = await api.get<DeployTarget[]>("/deploy-targets")
      return res.data
    },
  })

  const batchRenew = useMutation({
    mutationFn: async () => {
      // 排除导入证书（无签发渠道，无法续期）
      const renewableIds = (certs ?? [])
        .filter((c) => selected.includes(c.id) && c.provider !== "import")
        .map((c) => c.id)
      const res = await api.post<BatchResult>("/certificates/batch-renew", { ids: renewableIds })
      return res.data
    },
    onSuccess: (result) => {
      toast.success(
        `批量续期完成：成功 ${result.success} 个${result.failed.length > 0 ? `，失败 ${result.failed.length} 个` : ""}`,
      )
      setSelected([])
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
      queryClient.invalidateQueries({ queryKey: ["task-logs"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const batchDeploy = useMutation({
    mutationFn: async () => {
      const res = await api.post<BatchResult>("/certificates/batch-deploy", {
        ids: selected,
        target_id: deployTarget,
      })
      return res.data
    },
    onSuccess: (result) => {
      toast.success(
        `批量部署完成：成功 ${result.success} 个${result.failed.length > 0 ? `，失败 ${result.failed.length} 个` : ""}`,
      )
      setSelected([])
      setDeployOpen(false)
      setDeployTarget("")
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const toggleSelect = (id: string) => {
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }

  const toggleSelectAll = () => {
    setSelected((prev) => (prev.length === (certs?.length ?? 0) ? [] : (certs?.map((c) => c.id) ?? [])))
  }

  const deleteCert = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/certificates/${id}`)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["certificates"] }),
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>证书管理</CardTitle>
        <div className="flex gap-2">
          {selected.length > 0 && (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  const renewable = (certs ?? []).filter(
                    (c) => selected.includes(c.id) && c.provider !== "import",
                  ).length
                  if (renewable === 0) {
                    toast.error("所选证书均为导入证书，无法续期")
                    return
                  }
                  setRenewOpen(true)
                }}
                disabled={batchRenew.isPending}
              >
                <RefreshCcw className="mr-2 size-4" />
                批量续期 ({selected.length})
              </Button>
              <Button
                variant="outline"
                onClick={() => setDeployOpen(true)}
                disabled={batchDeploy.isPending}
              >
                <Rocket className="mr-2 size-4" />
                批量部署 ({selected.length})
              </Button>
            </>
          )}
          <Button variant="outline" onClick={() => setImportOpen(true)}>
            <Upload className="mr-2 size-4" />
            导入证书
          </Button>
          <Button onClick={() => setIssueOpen(true)}>
            <Plus className="mr-2 size-4" />
            签发证书
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">
                <input
                  type="checkbox"
                  className="size-4 accent-primary"
                  checked={certs != null && certs.length > 0 && selected.length === certs.length}
                  onChange={toggleSelectAll}
                />
              </TableHead>
              <TableHead>域名</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>渠道</TableHead>
              <TableHead>签发时间</TableHead>
              <TableHead>到期时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">
                  加载中...
                </TableCell>
              </TableRow>
            )}
            {!isLoading && (!certs || certs.length === 0) && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground py-8">
                  <ShieldCheck className="mx-auto mb-2 size-8 opacity-50" />
                  暂无证书，点击"签发证书"开始
                </TableCell>
              </TableRow>
            )}
            {certs?.map((c) => (
              <TableRow
                key={c.id}
                className={`cursor-pointer ${selected.includes(c.id) ? "bg-primary/5" : ""}`}
                onClick={() => navigate(`/certificates/${c.id}`)}
              >
                <TableCell className="w-10" onClick={(e) => e.stopPropagation()}>
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    checked={selected.includes(c.id)}
                    onChange={() => toggleSelect(c.id)}
                  />
                </TableCell>
                <TableCell className="font-medium">
                  {c.primary_domain}
                  {c.domains.length > 1 && (
                    <span className="ml-1 text-xs text-muted-foreground">+{c.domains.length - 1}</span>
                  )}
                </TableCell>
                <TableCell>
                  <CertStatusBadge status={c.status} />
                </TableCell>
                <TableCell className="text-muted-foreground">{c.provider === "import" ? "导入" : c.provider}</TableCell>
                <TableCell>{c.not_before ? new Date(c.not_before).toLocaleDateString() : "-"}</TableCell>
                <TableCell className={c.days_to_expire < 30 && c.status === "valid" ? "text-amber-600 font-medium" : ""}>
                  {c.not_after ? new Date(c.not_after).toLocaleDateString() : "-"}
                  {c.days_to_expire > 0 && c.status !== "revoked" && c.status !== "expired" && (
                    <span className="ml-1 text-xs text-muted-foreground">({c.days_to_expire}天)</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={(e) => {
                      e.stopPropagation()
                      setDeleteTarget(c)
                      setDeleteOpen(true)
                    }}
                  >
                    删除
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
      <Dialog open={deployOpen} onOpenChange={setDeployOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>批量部署（{selected.length} 个证书）</DialogTitle>
            <DialogDescription>将选中的证书部署到指定目标</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Select value={deployTarget} onValueChange={(v) => setDeployTarget(v ?? "")}>
              <SelectTrigger>
                <SelectValue>
                  {(v) => targets?.find((t) => t.id === v)?.name ?? (v ? v : "选择部署目标")}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {targets?.map((t) => (
                  <SelectItem key={t.id} value={t.id}>{t.name}（{t.type}）</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeployOpen(false)}>
              取消
            </Button>
            <Button
              type="button"
              disabled={!deployTarget || batchDeploy.isPending}
              onClick={() => batchDeploy.mutate()}
            >
              {batchDeploy.isPending ? "部署中..." : "开始部署"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <IssueDialog open={issueOpen} onOpenChange={setIssueOpen} />
      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除证书？</DialogTitle>
            <DialogDescription>删除证书 {deleteTarget?.primary_domain} 后无法恢复，相关部署与订单将保留。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button>
            <Button
              variant="destructive"
              disabled={deleteCert.isPending}
              onClick={() => deleteTarget && deleteCert.mutate(deleteTarget.id, { onSuccess: () => setDeleteOpen(false) })}
            >
              {deleteCert.isPending ? "删除中..." : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={renewOpen} onOpenChange={setRenewOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认批量续期？</DialogTitle>
            <DialogDescription>
              将续期选中的 {(certs ?? []).filter((c) => selected.includes(c.id) && c.provider !== "import").length} 个证书（已排除导入证书）。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenewOpen(false)}>取消</Button>
            <Button
              variant="default"
              disabled={batchRenew.isPending}
              onClick={() => batchRenew.mutate(undefined, { onSuccess: () => setRenewOpen(false) })}
            >
              {batchRenew.isPending ? "续期中..." : "确认续期"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
