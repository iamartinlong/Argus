import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useRef, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { NavLink } from "react-router-dom"
import { Plus, Trash2, MonitorSmartphone, Copy, Upload, Rocket, Ban, CircleCheck, History, Eye } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
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
import { api, getErrorMessage } from "@/lib/api"

interface Agent {
  id: string
  name: string
  status: string
  enabled: boolean
  last_seen_at: string
  version: string
  os: string
  created_at: string
}

interface Binary {
  version: string
  size: number
}

interface AgentTask {
  id: string
  certificate_id: string
  certificate_domain: string
  target_id: string
  status: string
  message: string
  created_at: string
  finished_at: string
}

const agentSchema = z.object({
  name: z.string().min(1, "请输入名称"),
})

type AgentForm = z.infer<typeof agentSchema>

export function AgentsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [versionTab, setVersionTab] = useState<"upload" | "history">("upload")
  const [upgradeTarget, setUpgradeTarget] = useState<Agent | null>(null)
  const [pendingUpgradeVersion, setPendingUpgradeVersion] = useState<string>("")
  const { data: freshUpgradeTarget } = useQuery({
    queryKey: ["agent", upgradeTarget?.id],
    queryFn: async () => {
      const res = await api.get<Agent>(`/agents/${upgradeTarget!.id}`)
      return res.data
    },
    enabled: !!upgradeTarget,
  })
  const effectiveUpgradeTarget = (freshUpgradeTarget as Agent | null) ?? upgradeTarget
  const [historyAgent, setHistoryAgent] = useState<Agent | null>(null)
  const [batchUpgradeOpen, setBatchUpgradeOpen] = useState(false)
  const [batchVersion, setBatchVersion] = useState("")
  const [selected, setSelected] = useState<string[]>([])
  const [editAgent, setEditAgent] = useState<Agent | null>(null)
  const [editName, setEditName] = useState("")
  const fileRef = useRef<HTMLInputElement>(null)

  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: async () => {
      const res = await api.get<Agent[]>("/agents")
      return res.data
    },
    refetchInterval: 5000, // 在线状态实时刷新
  })

  const { data: binaries } = useQuery({
    queryKey: ["agent-binaries"],
    queryFn: async () => {
      const res = await api.get<Binary[]>("/agents/binaries")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<AgentForm>({ resolver: zodResolver(agentSchema) })

  const create = useMutation({
    mutationFn: async (values: AgentForm) => {
      const res = await api.post<{ token: string }>("/agents", { name: values.name })
      return res.data.token
    },
    onSuccess: (token) => {
      setCreatedToken(token)
      queryClient.invalidateQueries({ queryKey: ["agents"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/agents/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["agents"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const uploadBinary = useMutation({
    mutationFn: async ({ version, file }: { version: string; file: File }) => {
      const form = new FormData()
      form.append("version", version)
      form.append("file", file)
      await api.post("/agents/binaries", form, {
        headers: { "Content-Type": "multipart/form-data" },
      })
    },
    onSuccess: () => {
      toast.success("二进制上传成功")
      queryClient.invalidateQueries({ queryKey: ["agent-binaries"] })
      setVersionTab("history")
      if (fileRef.current) fileRef.current.value = ""
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const requestUpgrade = useMutation({
    mutationFn: async ({ id, version }: { id: string; version: string }) => {
      await api.post(`/agents/${id}/upgrade`, { version })
    },
    onSuccess: () => {
      toast.success("升级指令已下发，Agent 将在下次轮询时执行")
      setUpgradeTarget(null)
      setPendingUpgradeVersion("")
      queryClient.invalidateQueries({ queryKey: ["agents"] })
      queryClient.invalidateQueries({ queryKey: ["agent"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const setEnabled = useMutation({
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      await api.post(`/agents/${id}/enabled`, { enabled })
    },
    onSuccess: () => {
      toast.success("已更新")
      queryClient.invalidateQueries({ queryKey: ["agents"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const renameAgent = useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      await api.put(`/agents/${id}`, { name })
    },
    onSuccess: () => {
      toast.success("已重命名")
      queryClient.invalidateQueries({ queryKey: ["agents"] })
      setEditAgent(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const batchUpgrade = useMutation({
    mutationFn: async (version: string) => {
      const res = await api.post<{ success: number; failed: { id: string; error: string }[] }>(
        "/agents/batch-upgrade",
        { ids: selected, version },
      )
      return res.data
    },
    onSuccess: (result) => {
      toast.success(
        `批量升级指令已下发：${result.success} 个成功${result.failed.length > 0 ? `，${result.failed.length} 个失败` : ""}`,
      )
      setSelected([])
      setBatchUpgradeOpen(false)
      queryClient.invalidateQueries({ queryKey: ["agents"] })
      queryClient.invalidateQueries({ queryKey: ["agent"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const taskHistory = useQuery({
    queryKey: ["agent-tasks", historyAgent?.id],
    queryFn: async () => {
      const res = await api.get<AgentTask[]>(`/agents/${historyAgent!.id}/tasks`)
      return res.data
    },
    enabled: !!historyAgent,
    refetchInterval: 5000, // 弹窗文案承诺实时刷新
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>Agent 管理</CardTitle>
        <div className="flex gap-2">
          {selected.length > 0 && (
            <Button
              variant="outline"
              onClick={() => setBatchUpgradeOpen(true)}
              disabled={!binaries || binaries.length === 0}
            >
              <Rocket className="mr-2 size-4" />
              批量升级 ({selected.length})
            </Button>
          )}
          <Button variant="outline" onClick={() => { setVersionTab("upload"); setUploadOpen(true) }}>
            <Upload className="mr-2 size-4" />
            版本管理
          </Button>
          <Button onClick={() => setDialogOpen(true)}>
            <Plus className="mr-2 size-4" />
            添加 Agent
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
                  checked={(agents?.length ?? 0) > 0 && selected.length === agents?.length}
                  onChange={(e) => {
                    if (e.target.checked) {
                      setSelected((agents ?? []).map((a) => a.id))
                    } else {
                      setSelected([])
                    }
                  }}
                />
              </TableHead>
              <TableHead>名称</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>系统</TableHead>
              <TableHead>版本</TableHead>
              <TableHead>最后心跳</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {!agents || agents.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground py-8">
                  <MonitorSmartphone className="mx-auto mb-2 size-8 opacity-50" />
                  暂无 Agent。添加后，业务机通过出站连接拉取部署任务（可穿透 NAT）
                </TableCell>
              </TableRow>
            ) : (
              agents.map((a) => (
                <TableRow key={a.id} className={selected.includes(a.id) ? "bg-primary/5" : ""}>
                  <TableCell>
                    <input
                      type="checkbox"
                      className="size-4 accent-primary"
                      checked={selected.includes(a.id)}
                      onChange={() =>
                        setSelected((prev) =>
                          prev.includes(a.id) ? prev.filter((x) => x !== a.id) : [...prev, a.id],
                        )
                      }
                    />
                  </TableCell>
                  <TableCell className="font-medium">
                    <NavLink to={`/agents/${a.id}`} className="hover:underline text-primary">
                      {a.name}
                    </NavLink>
                  </TableCell>
                  <TableCell>
                    {!a.enabled ? (
                      <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">已禁用</Badge>
                    ) : a.status === "online" ? (
                      <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">在线</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-zinc-500/15 text-zinc-600 dark:text-zinc-400">离线</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{a.os || "-"}</TableCell>
                  <TableCell className="text-muted-foreground">{a.version || "-"}</TableCell>
                  <TableCell>
                    {a.last_seen_at ? new Date(a.last_seen_at).toLocaleString() : "从未连接"}
                  </TableCell>
                  <TableCell className="text-right space-x-1">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (a.enabled) {
                          if (confirm(`禁用 Agent ${a.name}？禁用后其 Token 立即失效并置离线`)) {
                            setEnabled.mutate({ id: a.id, enabled: false })
                          }
                        } else {
                          setEnabled.mutate({ id: a.id, enabled: true })
                        }
                      }}
                    >
                      {a.enabled ? (
                        <>
                          <Ban className="mr-1 size-4" />禁用
                        </>
                      ) : (
                        <>
                          <CircleCheck className="mr-1 size-4" />启用
                        </>
                      )}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setEditAgent(a)
                        setEditName(a.name)
                      }}
                    >
                      <Eye className="mr-1 size-4" />
                      编辑
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => { setPendingUpgradeVersion(""); setUpgradeTarget(a) }}
                      disabled={!binaries || binaries.length === 0}
                      title={!binaries || binaries.length === 0 ? "请先上传新版本二进制" : ""}
                    >
                      <Rocket className="mr-1 size-4" />
                      升级
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setHistoryAgent(a)}>
                      <History className="mr-1 size-4" />
                      任务
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => {
                        if (confirm(`确认删除 Agent ${a.name}？`)) remove.mutate(a.id)
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <p className="mt-4 text-xs text-muted-foreground">
          Agent 在业务机运行（argus-agent --config agent.json），配置 server_url 与 token 后出站轮询任务，
          支持 NAT/无入站端口场景；部署目标类型选择「Agent」并绑定本页创建的 Agent。
        </p>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>添加 Agent</DialogTitle>
            <DialogDescription>创建后生成连接 Token（仅显示一次），配置到业务机 agent.json</DialogDescription>
          </DialogHeader>
          {createdToken ? (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Agent Token（仅此一次）</Label>
                <div className="flex gap-2">
                  <Input readOnly value={createdToken} className="font-mono text-xs" />
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={() => {
                      navigator.clipboard.writeText(createdToken)
                      toast.success("已复制")
                    }}
                  >
                    <Copy className="size-4" />
                  </Button>
                </div>
              </div>
              <DialogFooter>
                <Button
                  onClick={() => {
                    setCreatedToken(null)
                    setDialogOpen(false)
                  }}
                >
                  完成
                </Button>
              </DialogFooter>
            </div>
          ) : (
            <form className="space-y-4" onSubmit={handleSubmit((v) => create.mutate(v))}>
              <div className="space-y-2">
                <Label>名称</Label>
                <Input placeholder="生产机-01" {...register("name")} />
                {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
              </div>
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                  取消
                </Button>
                <Button type="submit" disabled={create.isPending}>
                  {create.isPending ? "创建中..." : "创建"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* 版本管理 */}
      <Dialog open={uploadOpen} onOpenChange={setUploadOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>版本管理</DialogTitle>
            <DialogDescription>上传新版本与查看已上传版本历史（SHA256 校验）</DialogDescription>
          </DialogHeader>
          <div className="inline-flex h-9 items-center rounded-lg bg-muted p-1 text-muted-foreground w-fit">
            <button
              onClick={() => setVersionTab("upload")}
              className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium ${versionTab === "upload" ? "bg-background text-foreground shadow" : ""}`}
            >
              上传二进制
            </button>
            <button
              onClick={() => setVersionTab("history")}
              className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium ${versionTab === "history" ? "bg-background text-foreground shadow" : ""}`}
            >
              版本历史
            </button>
          </div>
          {versionTab === "upload" ? (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                const version = new FormData(e.currentTarget).get("version") as string
                const file = fileRef.current?.files?.[0]
                if (!version || !file) {
                  toast.error("请填写版本号并选择文件")
                  return
                }
                uploadBinary.mutate({ version, file })
              }}
            >
              <div className="space-y-2">
                <Label>版本号</Label>
                <Input name="version" placeholder="0.2.0" required />
              </div>
              <div className="space-y-2">
                <Label>二进制文件（argus-agent，含目标平台）</Label>
                <Input ref={fileRef} type="file" accept=".exe,application/octet-stream" required />
              </div>
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => setUploadOpen(false)}>
                  取消
                </Button>
                <Button type="submit" disabled={uploadBinary.isPending}>
                  {uploadBinary.isPending ? "上传中..." : "上传"}
                </Button>
              </DialogFooter>
            </form>
          ) : (
            <div className="max-h-[50vh] overflow-y-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>版本</TableHead>
                    <TableHead>大小</TableHead>
                    <TableHead>SHA256</TableHead>
                    <TableHead>上传时间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {!binaries || binaries.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} className="text-center text-muted-foreground py-6">
                        暂无版本
                      </TableCell>
                    </TableRow>
                  ) : (
                    binaries.map((b: any) => (
                      <TableRow key={b.version}>
                        <TableCell className="font-medium">{b.version}</TableCell>
                        <TableCell>{(b.size / 1024).toFixed(0)} KB</TableCell>
                        <TableCell className="font-mono text-xs truncate max-w-[160px]" title={b.sha256}>
                          {b.sha256 ? `${b.sha256.slice(0, 16)}...` : "-"}
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs">
                          {b.created_at ? new Date(b.created_at).toLocaleString() : "-"}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* 下发升级 */}
      <Dialog open={!!upgradeTarget} onOpenChange={(open) => { if (!open) { setUpgradeTarget(null); setPendingUpgradeVersion("") } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>升级 Agent：{effectiveUpgradeTarget?.name}</DialogTitle>
            <DialogDescription>
              当前版本 {effectiveUpgradeTarget?.version || "未知"}，选择目标版本后点击下发
            </DialogDescription>
          </DialogHeader>
          {binaries && binaries.length > 0 && (
            <div className="space-y-2">
              <Label>目标版本</Label>
              <Select
                value={pendingUpgradeVersion}
                onValueChange={(v) => setPendingUpgradeVersion(v ?? "")}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择版本" />
                </SelectTrigger>
                <SelectContent className="min-w-[320px]">
                  {binaries.map((b) => {
                    const isCurrent = b.version === effectiveUpgradeTarget?.version
                    const isPending = b.version === (effectiveUpgradeTarget as any)?.upgrade_version
                    return (
                      <SelectItem key={b.version} value={b.version} disabled={isCurrent || isPending}>
                        {b.version}（{(b.size / 1024).toFixed(0)} KB）{isCurrent ? "（当前版本）" : isPending ? "（已待升级）" : ""}
                      </SelectItem>
                    )
                  })}
                </SelectContent>
              </Select>
              {pendingUpgradeVersion && pendingUpgradeVersion === effectiveUpgradeTarget?.version && (
                <p className="text-xs text-amber-600">目标版本与当前版本一致，无需升级</p>
              )}
              {pendingUpgradeVersion && pendingUpgradeVersion === (effectiveUpgradeTarget as any)?.upgrade_version && (
                <p className="text-xs text-amber-600">该版本已是待升级版本，无需重复下发</p>
              )}
            </div>
          )}
          {(!binaries || binaries.length === 0) && (
            <p className="text-sm text-muted-foreground">暂无已上传的二进制，请先上传。</p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => { setUpgradeTarget(null); setPendingUpgradeVersion("") }}>
              取消
            </Button>
            <Button
              disabled={!pendingUpgradeVersion || pendingUpgradeVersion === effectiveUpgradeTarget?.version || requestUpgrade.isPending}
              onClick={() => { if (pendingUpgradeVersion) requestUpgrade.mutate({ id: effectiveUpgradeTarget!.id, version: pendingUpgradeVersion }) }}
            >
              {requestUpgrade.isPending ? "下发中..." : "下发升级"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 任务历史 */}
      <Dialog open={!!historyAgent} onOpenChange={(open) => !open && setHistoryAgent(null)}>
        <DialogContent className="sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>任务历史：{historyAgent?.name}</DialogTitle>
            <DialogDescription>最近 50 条部署任务记录（5 秒自动刷新）</DialogDescription>
          </DialogHeader>
          <div className="max-h-[70vh] overflow-y-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[40%]">证书</TableHead>
                  <TableHead className="w-24">状态</TableHead>
                  <TableHead>消息</TableHead>
                  <TableHead className="w-44 whitespace-nowrap">创建时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!taskHistory.data || taskHistory.data.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-center text-muted-foreground py-6">
                      暂无任务记录
                    </TableCell>
                  </TableRow>
                ) : (
                  taskHistory.data.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="truncate font-mono text-xs" title={t.certificate_id}>
                        {t.certificate_domain || t.certificate_id}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant="outline"
                          className={
                            t.status === "success"
                              ? "bg-green-500/15 text-green-600 dark:text-green-400"
                              : t.status === "failed"
                                ? "bg-red-500/15 text-red-600 dark:text-red-400"
                                : ""
                          }
                        >
                          {t.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="truncate text-muted-foreground" title={t.message}>
                        {t.message || "-"}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-muted-foreground">
                        {new Date(t.created_at).toLocaleString()}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setHistoryAgent(null)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 批量升级 */}
      <Dialog open={batchUpgradeOpen} onOpenChange={setBatchUpgradeOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>批量升级（{selected.length} 个 Agent）</DialogTitle>
            <DialogDescription>向选中 Agent 下发升级指令，Agent 将在下次轮询时执行</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label>目标版本</Label>
            <Select value={batchVersion} onValueChange={(v) => setBatchVersion(v ?? "")}>
              <SelectTrigger className="w-full">
                <SelectValue>
                  {(v) => (v ? `${v}（${((binaries?.find((b) => b.version === v)?.size ?? 0) / 1024).toFixed(0)} KB）` : "选择版本")}
                </SelectValue>
              </SelectTrigger>
              <SelectContent className="min-w-[320px]">
                {binaries?.map((b) => (
                  <SelectItem key={b.version} value={b.version}>
                    {b.version}（{(b.size / 1024).toFixed(0)} KB）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {selected.length > 0 && batchVersion && (
            <div className="max-h-40 overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>名称</TableHead>
                    <TableHead>当前版本</TableHead>
                    <TableHead>结果</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {selected.map((id) => {
                    const a: any = agents?.find((x) => x.id === id)
                    const isCurrent = a?.version === batchVersion
                    const isPending = a?.upgrade_version === batchVersion
                    return (
                      <TableRow key={id}>
                        <TableCell className="font-medium">{a?.name ?? id}</TableCell>
                        <TableCell>{a?.version || "-"}</TableCell>
                        <TableCell className={isCurrent || isPending ? "text-amber-600" : "text-green-600"}>
                          {isCurrent ? "已是该版本（跳过）" : isPending ? "已待升级（跳过）" : "将升级"}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setBatchUpgradeOpen(false)}>
              取消
            </Button>
            <Button
              type="button"
              disabled={
                !batchVersion ||
                batchUpgrade.isPending ||
                (selected.length > 0 &&
                  !!batchVersion &&
                  selected.every((id) => {
                    const a: any = agents?.find((x) => x.id === id)
                    return a?.version === batchVersion || a?.upgrade_version === batchVersion
                  }))
              }
              onClick={() => batchUpgrade.mutate(batchVersion)}
            >
              {batchUpgrade.isPending ? "下发中..." : "下发升级"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* 查看/编辑 */}
      <Dialog open={!!editAgent} onOpenChange={(open) => !open && setEditAgent(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Agent 详情：{editAgent?.name}</DialogTitle>
            <DialogDescription>查看信息并重命名（Token 不可二次查看，仅创建时显示一次）</DialogDescription>
          </DialogHeader>
          {editAgent && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <Label className="text-muted-foreground">ID</Label>
                  <p className="font-mono text-xs break-all">{editAgent.id}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">状态</Label>
                  <p>
                    {!editAgent.enabled ? (
                      <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">已禁用</Badge>
                    ) : editAgent.status === "online" ? (
                      <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">在线</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-zinc-500/15 text-zinc-600 dark:text-zinc-400">离线</Badge>
                    )}
                  </p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">系统</Label>
                  <p>{editAgent.os || "-"}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">版本</Label>
                  <p>{editAgent.version || "-"}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">最后心跳</Label>
                  <p>{editAgent.last_seen_at ? new Date(editAgent.last_seen_at).toLocaleString() : "从未连接"}</p>
                </div>
                <div className="space-y-1">
                  <Label className="text-muted-foreground">创建时间</Label>
                  <p>{new Date(editAgent.created_at).toLocaleString()}</p>
                </div>
              </div>
              <div className="space-y-2">
                <Label>名称</Label>
                <Input value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setEditAgent(null)}>
                  取消
                </Button>
                <Button
                  disabled={!editName.trim() || renameAgent.isPending}
                  onClick={() => renameAgent.mutate({ id: editAgent.id, name: editName.trim() })}
                >
                  {renameAgent.isPending ? "保存中..." : "保存"}
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
