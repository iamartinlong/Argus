import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useParams } from "react-router-dom"
import { Download, RefreshCw, Ban, ArrowLeft, Rocket, Loader2, Eye, Copy, Check, TriangleAlert } from "lucide-react"
import { toast } from "sonner"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { api, getErrorMessage, getErrorMessageAsync } from "@/lib/api"
import { NOTIFICATION_EVENTS, type Certificate, type Order } from "@/lib/types"
import { CertStatusBadge, OrderStatusBadge } from "./status-badge"

const eventNames: Record<string, string> = Object.fromEntries(
  NOTIFICATION_EVENTS.map((e) => [e.value, e.label]),
)

interface DeployRecord {
  id: string
  target_id: string
  status: string
  checksum: string
  message: string
  retries: number
  deployed_at: string
  created_at: string
}

export function CertificateDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [downloadOpen, setDownloadOpen] = useState(false)
  const [downloading, setDownloading] = useState<string | null>(null)
  const [caOpen, setCaOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [targetsOpen, setTargetsOpen] = useState(false)
  const [selectedTargets, setSelectedTargets] = useState<string[]>([])
  const [deployConfirmOpen, setDeployConfirmOpen] = useState(false)
  const [renewConfirmOpen, setRenewConfirmOpen] = useState(false)
  const [revokeOpen, setRevokeOpen] = useState(false)
  const [notifyOpen, setNotifyOpen] = useState(false)
  const [selectedNotifyEvents, setSelectedNotifyEvents] = useState<string[]>([])

  const { data: cert, isLoading } = useQuery({
    queryKey: ["certificate", id],
    queryFn: async () => {
      const res = await api.get<Certificate>(`/certificates/${id}`)
      return res.data
    },
  })

  const { data: orders } = useQuery({
    queryKey: ["certificate-orders", id],
    queryFn: async () => {
      const res = await api.get<Order[]>(`/certificates/${id}/orders`)
      return res.data
    },
  })

  const { data: deployRecords } = useQuery({
    queryKey: ["deploy-records", id],
    queryFn: async () => {
      const res = await api.get<DeployRecord[]>(`/certificates/${id}/deploy-records`)
      return res.data
    },
  })

  const { data: targets } = useQuery({
    queryKey: ["deploy-targets"],
    queryFn: async () => {
      const res = await api.get<{ id: string; name: string }[]>("/deploy-targets")
      return res.data
    },
  })

  const deploy = useMutation({
    mutationFn: async () => {
      await api.post(`/certificates/${id}/deploy`)
    },
    onSuccess: () => {
      setDeployConfirmOpen(false)
      toast.success("部署请求已提交")
      queryClient.invalidateQueries({ queryKey: ["deploy-records", id] })
    },
    onError: (err) => {
      setDeployConfirmOpen(false)
      toast.error(getErrorMessage(err))
    },
  })

  const renew = useMutation({
    mutationFn: async () => {
      await api.post(`/certificates/${id}/renew`)
    },
    onSuccess: () => {
      setRenewConfirmOpen(false)
      toast.success("续期请求已提交")
      queryClient.invalidateQueries({ queryKey: ["certificate", id] })
      queryClient.invalidateQueries({ queryKey: ["certificate-orders", id] })
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
    },
    onError: (err) => {
      setRenewConfirmOpen(false)
      toast.error(getErrorMessage(err))
    },
  })

  const revoke = useMutation({
    mutationFn: async () => {
      await api.post(`/certificates/${id}/revoke`)
    },
    onSuccess: () => {
      setRevokeOpen(false)
      toast.success("已吊销")
      queryClient.invalidateQueries({ queryKey: ["certificate", id] })
      queryClient.invalidateQueries({ queryKey: ["certificate-orders", id] })
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const updateTargets = useMutation({
    mutationFn: async (ids: string[]) => {
      await api.put(`/certificates/${id}/targets`, { deploy_target_ids: ids })
    },
    onSuccess: () => {
      toast.success("部署目标已更新")
      queryClient.invalidateQueries({ queryKey: ["certificate", id] })
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
      setTargetsOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const updateNotifyEvents = useMutation({
    mutationFn: async (events: string[]) => {
      await api.put(`/certificates/${id}/notify-events`, { notify_events: events })
    },
    onSuccess: () => {
      toast.success("通知事件已更新")
      queryClient.invalidateQueries({ queryKey: ["certificate", id] })
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
      setNotifyOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  if (isLoading || !cert) {
    return <div className="text-muted-foreground">加载中...</div>
  }

  // axios blob 下载：自动携带 Bearer 认证（window.open 导航不会带 Authorization 头）
  const download = async (format: string, filename: string) => {
    setDownloading(format)
    try {
      const res = await api.get<Blob>(`/certificates/${cert!.id}/download?format=${format}`, {
        responseType: "blob",
      })
      const url = URL.createObjectURL(res.data)
      const a = document.createElement("a")
      a.href = url
      a.download = `${cert!.primary_domain}-${filename}.zip`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(await getErrorMessageAsync(err))
    } finally {
      setDownloading(null)
    }
  }

  const downloadFormats = [
    { format: "nginx", label: "Nginx", files: "pem/key", desc: "fullchain.pem + privkey.pem", enabled: true },
    { format: "apache", label: "Apache", files: "crt/key", desc: "cert.crt + privkey.key", enabled: true },
    { format: "iis", label: "IIS", files: "pfx", desc: "cert.pfx + password.txt（随机密码）", enabled: true },
    { format: "tomcat", label: "Tomcat", files: "pfx", desc: "cert.pfx + password.txt（随机密码）", enabled: true },
    { format: "other", label: "其他", files: "pem/key", desc: "cert.pem + privkey.pem", enabled: true },
    { format: "jks", label: "JKS", files: "jks", desc: "即将支持", enabled: false },
    { format: "root", label: "根证书", files: "crt/cer", desc: "即将支持", enabled: false },
  ]

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20 text-muted-foreground">
        加载中...
      </div>
    )
  }
  if (!cert) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-20">
        <p className="text-muted-foreground">证书不存在</p>
        <Button variant="outline" size="sm" onClick={() => navigate("/certificates")}>
          <ArrowLeft className="mr-1 size-4" />
          返回列表
        </Button>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => navigate("/certificates")}>
          <ArrowLeft className="mr-1 size-4" />
          返回
        </Button>
      </div>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <CardTitle className="flex items-center gap-2">
            {cert.primary_domain}
            <CertStatusBadge status={cert.status} />
          </CardTitle>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDeployConfirmOpen(true)}
              disabled={deploy.isPending || cert.deploy_target_ids.length === 0}
              title={cert.deploy_target_ids.length === 0 ? "该证书未绑定部署目标" : ""}
            >
              <Rocket className="mr-1 size-4" />
              部署
            </Button>
            <Button variant="outline" size="sm" onClick={() => setDownloadOpen(true)}>
              <Download className="mr-1 size-4" />
              下载
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRenewConfirmOpen(true)}
              disabled={renew.isPending || cert.status === "revoked" || cert.provider === "import"}
              title={cert.provider === "import" ? "导入的证书无法续期" : ""}
            >
              <RefreshCw className="mr-1 size-4" />
              续期
            </Button>
            <Button
              variant="destructive"
              size="sm"
              disabled={cert.status === "revoked" || cert.status === "pending" || cert.provider === "import"}
              title={cert.provider === "import" ? "导入的证书无法吊销" : ""}
              onClick={() => setRevokeOpen(true)}
            >
              <Ban className="mr-1 size-4" />
              吊销
            </Button>
            <Dialog open={revokeOpen} onOpenChange={setRevokeOpen}>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>确认吊销证书？</DialogTitle>
                  <DialogDescription>吊销后证书将立即失效，相关服务需尽快更换证书。</DialogDescription>
                </DialogHeader>
                {cert.provider !== "acme" && (
                  <Alert className="border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400">
                    <TriangleAlert className="size-4 text-amber-500" />
                    <AlertTitle>提示：云厂商渠道不支持吊销</AlertTitle>
                    <AlertDescription>
                      当前证书来自云厂商渠道（{cert.provider}），云端无法真正吊销。
                      吊销将仅在系统内标记为「已吊销」，云端证书仍有效至到期日，请自行在云控制台处理。
                    </AlertDescription>
                  </Alert>
                )}
                <DialogFooter>
                  <Button variant="outline" onClick={() => setRevokeOpen(false)} disabled={revoke.isPending}>
                    取消
                  </Button>
                  <Button variant="destructive" disabled={revoke.isPending} onClick={() => revoke.mutate()}>
                    {revoke.isPending ? "吊销中..." : "确认吊销"}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2 text-sm">
          <div className="space-y-1">
            <p className="text-muted-foreground">SAN 域名</p>
            <p className="font-mono break-all">{cert.domains.join(", ")}</p>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">签发渠道</p>
            <p>{cert.provider}</p>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">签发 CA</p>
            {cert.issuer_ca.includes("-----BEGIN") ? (
              <Button variant="outline" size="sm" onClick={() => setCaOpen(true)}>
                <Eye className="mr-1 size-4" />
                查看
              </Button>
            ) : (
              <p className="font-mono break-all">{cert.issuer_ca || "-"}</p>
            )}
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">验证方式</p>
            <p>{cert.verification || "-"}</p>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">部署目标</p>
            <div className="flex gap-1 flex-wrap items-center">
              {cert.deploy_target_ids.length > 0 ? (
                cert.deploy_target_ids.map((tid) => (
                  <Badge key={tid} variant="outline">
                    {targets?.find((t) => t.id === tid)?.name ?? tid}
                  </Badge>
                ))
              ) : (
                <span className="text-muted-foreground">未绑定</span>
              )}
              <Button
                variant="outline"
                size="sm"
                className="h-6 px-2 text-xs"
                onClick={() => {
                  setSelectedTargets(cert.deploy_target_ids)
                  setTargetsOpen(true)
                }}
              >
                编辑
              </Button>
            </div>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">有效期</p>
            <p>
              {cert.not_before ? new Date(cert.not_before).toLocaleString() : "-"}
              {" 至 "}
              {cert.not_after ? new Date(cert.not_after).toLocaleString() : "-"}
            </p>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">通知事件</p>
            <div className="flex gap-1 flex-wrap items-center">
              {(cert.notify_events ?? []).length > 0 ? (
                (cert.notify_events ?? []).map((ev) => (
                  <Badge key={ev} variant="outline">
                    {eventNames[ev] ?? ev}
                  </Badge>
                ))
              ) : (
                <span className="text-muted-foreground">全部事件</span>
              )}
              <Button
                variant="outline"
                size="sm"
                className="h-6 px-2 text-xs"
                aria-label="编辑通知事件"
                onClick={() => {
                  setSelectedNotifyEvents(cert.notify_events)
                  setNotifyOpen(true)
                }}
              >
                编辑
              </Button>
            </div>
          </div>
          <div className="space-y-1">
            <p className="text-muted-foreground">创建时间</p>
            <p>{new Date(cert.created_at).toLocaleString()}</p>
          </div>
          {cert.last_error && (
            <div className="space-y-1 md:col-span-2">
              <p className="text-muted-foreground">最近错误</p>
              <p className="font-mono text-xs text-destructive break-all">{cert.last_error}</p>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">部署记录</CardTitle>
        </CardHeader>
        <CardContent>
          {deployRecords && deployRecords.length > 0 ? (
            <div className="space-y-3">
              {deployRecords.map((r) => (
                <div key={r.id} className="flex items-center gap-3 text-sm">
                  {r.status === "success" ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">成功</Badge>
                  ) : r.status === "running" ? (
                    <Badge variant="outline" className="bg-blue-500/15 text-blue-600 dark:text-blue-400">进行中</Badge>
                  ) : (
                    <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">失败</Badge>
                  )}
                  <span className="text-muted-foreground">
                    {targets?.find((t) => t.id === r.target_id)?.name ?? r.target_id}
                  </span>
                  <span className="text-muted-foreground">
                    {new Date(r.created_at).toLocaleString()}
                  </span>
                  <span className="font-mono text-xs text-muted-foreground truncate flex-1">
                    {r.message || "-"}
                  </span>
                  {r.checksum && (
                    <span className="font-mono text-xs text-muted-foreground">sha256:{r.checksum.slice(0, 12)}</span>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">暂无部署记录</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">任务记录</CardTitle>
        </CardHeader>
        <CardContent>
          {orders && orders.length > 0 ? (
            <div className="space-y-3">
              {orders.map((o) => (
                <div key={o.id} className="flex items-center gap-3 text-sm">
                  <OrderStatusBadge status={o.status} />
                  <span className="font-medium w-16">{o.type}</span>
                  <span className="text-muted-foreground">
                    {new Date(o.started_at).toLocaleString()}
                  </span>
                  {o.error && (
                    <span className="font-mono text-xs text-destructive truncate flex-1">
                      {o.error}
                    </span>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">暂无任务记录</p>
          )}
        </CardContent>
      </Card>

      <Dialog open={caOpen} onOpenChange={setCaOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>签发 CA</DialogTitle>
            <DialogDescription>证书签发者（CA）证书内容</DialogDescription>
          </DialogHeader>
          <div className="relative">
            <pre className="max-h-[60vh] overflow-auto rounded-lg border bg-muted/50 p-4 font-mono text-xs whitespace-pre-wrap break-all">
              {cert.issuer_ca}
            </pre>
            <Button
              variant="outline"
              size="sm"
              className="absolute top-2 right-2"
              onClick={async () => {
                await navigator.clipboard.writeText(cert.issuer_ca)
                setCopied(true)
                setTimeout(() => setCopied(false), 1500)
              }}
            >
              {copied ? (
                <Check className="mr-1 size-4 text-green-500" />
              ) : (
                <Copy className="mr-1 size-4" />
              )}
              {copied ? "已复制" : "复制"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={downloadOpen} onOpenChange={setDownloadOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>下载证书</DialogTitle>
            <DialogDescription>选择服务器类型下载对应格式，含私钥的包需要操作权限。</DialogDescription>
          </DialogHeader>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>服务器类型</TableHead>
                <TableHead>证书格式</TableHead>
                <TableHead>文件</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {downloadFormats.map((f) => (
                <TableRow key={f.format}>
                  <TableCell className="font-medium">{f.label}</TableCell>
                  <TableCell>{f.files}</TableCell>
                  <TableCell className="text-muted-foreground text-xs">{f.desc}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!f.enabled || downloading !== null}
                      onClick={() => download(f.format, f.format)}
                    >
                      {downloading === f.format ? (
                        <Loader2 className="mr-1 size-4 animate-spin" />
                      ) : (
                        <Download className="mr-1 size-4" />
                      )}
                      下载
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </DialogContent>
      </Dialog>

      <Dialog open={targetsOpen} onOpenChange={setTargetsOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑部署目标</DialogTitle>
            <DialogDescription>选择签发/续期成功后自动部署的目标（可多选）</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            {targets && targets.length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {targets.map((t) => {
                  const selected = selectedTargets.includes(t.id)
                  return (
                    <Button
                      key={t.id}
                      type="button"
                      size="sm"
                      variant={selected ? "default" : "outline"}
                      onClick={() => {
                        setSelectedTargets(
                          selected
                            ? selectedTargets.filter((x) => x !== t.id)
                            : [...selectedTargets, t.id],
                        )
                      }}
                    >
                      {t.name}
                    </Button>
                  )
                })}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                暂无部署目标，请先到「部署目标」页面创建。
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setTargetsOpen(false)}>
              取消
            </Button>
            <Button
              type="button"
              disabled={updateTargets.isPending}
              onClick={() => updateTargets.mutate(selectedTargets)}
            >
              {updateTargets.isPending ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={notifyOpen} onOpenChange={setNotifyOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑通知事件</DialogTitle>
            <DialogDescription>
              勾选的事件触发该证书的通知；不勾选任何事件则全部事件均可通知。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-1 rounded-md border p-3">
            {NOTIFICATION_EVENTS.map((e) => {
              const selected = selectedNotifyEvents.includes(e.value)
              return (
                <label
                  key={e.value}
                  className="flex cursor-pointer items-center justify-between gap-2 text-sm"
                >
                  <span>{e.label}</span>
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    checked={selected}
                    onChange={() =>
                      setSelectedNotifyEvents(
                        selected
                          ? selectedNotifyEvents.filter((x) => x !== e.value)
                          : [...selectedNotifyEvents, e.value],
                      )
                    }
                  />
                </label>
              )
            })}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setNotifyOpen(false)}>
              取消
            </Button>
            <Button
              type="button"
              disabled={updateNotifyEvents.isPending}
              onClick={() => updateNotifyEvents.mutate(selectedNotifyEvents)}
            >
              {updateNotifyEvents.isPending ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deployConfirmOpen} onOpenChange={setDeployConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认部署证书？</DialogTitle>
            <DialogDescription>将部署到当前绑定的 {cert.deploy_target_ids.length} 个目标。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeployConfirmOpen(false)} disabled={deploy.isPending}>
              取消
            </Button>
            <Button disabled={deploy.isPending} onClick={() => deploy.mutate()}>
              {deploy.isPending && <Loader2 className="mr-1 size-4 animate-spin" />}
              {deploy.isPending ? "部署中..." : "确认部署"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={renewConfirmOpen} onOpenChange={setRenewConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认续期证书？</DialogTitle>
            <DialogDescription>
              {cert.days_to_expire > 30 ? (
                <>
                  该证书剩余有效期还有 <b>{cert.days_to_expire}</b> 天，尚未临近到期。
                  <br />
                  提前续期将重新向 CA 签发新证书，确认继续？
                </>
              ) : (
                <>续期后将重新向 CA 签发新证书，原证书有效期将被覆盖。</>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenewConfirmOpen(false)} disabled={renew.isPending}>
              取消
            </Button>
            <Button disabled={renew.isPending} onClick={() => renew.mutate()}>
              {renew.isPending && <Loader2 className="mr-1 size-4 animate-spin" />}
              {renew.isPending ? "续期中..." : "确认续期"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
