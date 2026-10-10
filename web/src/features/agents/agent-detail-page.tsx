import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useParams, useNavigate } from "react-router-dom"
import { useState } from "react"
import { toast } from "sonner"
import { ArrowLeft, Ban, CircleCheck, Rocket, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
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
  upgrade_version: string
  upgrade_sha256: string
  upgrade_requested_at: string
}

export function AgentDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<"info" | "tasks" | "upgrade">("info")
  const [editName, setEditName] = useState("")
  const [upgradeVersion, setUpgradeVersion] = useState("")

  const { data: agent, isLoading } = useQuery({
    queryKey: ["agent", id],
    queryFn: async () => {
      const res = await api.get<Agent>(`/agents/${id}`)
      return res.data
    },
    enabled: !!id,
  })

  const { data: tasks } = useQuery({
    queryKey: ["agent-tasks", id],
    queryFn: async () => {
      const res = await api.get<any[]>(`/agents/${id}/tasks`)
      return res.data
    },
    enabled: !!id && tab === "tasks",
  })

  const { data: binaries } = useQuery({
    queryKey: ["agent-binaries"],
    queryFn: async () => {
      const res = await api.get<any[]>("/agents/binaries")
      return res.data
    },
  })

  const rename = useMutation({
    mutationFn: async (name: string) => {
      await api.put(`/agents/${id}`, { name })
    },
    onSuccess: () => {
      toast.success("已重命名")
      queryClient.invalidateQueries({ queryKey: ["agent", id] })
      queryClient.invalidateQueries({ queryKey: ["agents"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const setEnabled = useMutation({
    mutationFn: async (enabled: boolean) => {
      await api.post(`/agents/${id}/enabled`, { enabled })
    },
    onSuccess: () => {
      toast.success("已更新")
      queryClient.invalidateQueries({ queryKey: ["agent", id] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async () => {
      await api.delete(`/agents/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      navigate("/agents")
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const requestUpgrade = useMutation({
    mutationFn: async (version: string) => {
      await api.post(`/agents/${id}/upgrade`, { version })
    },
    onSuccess: () => {
      toast.success("升级指令已下发")
      queryClient.invalidateQueries({ queryKey: ["agent", id] })
      queryClient.invalidateQueries({ queryKey: ["agents"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  if (isLoading) return <div className="p-4 text-muted-foreground">加载中...</div>
  if (!agent) return <div className="p-4">未找到 Agent</div>

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => navigate("/agents")}>
          <ArrowLeft className="mr-1 size-4" />
          返回
        </Button>
        <h2 className="text-lg font-semibold">{agent.name}</h2>
        {agent.enabled ? (
          agent.status === "online" ? <Badge className="bg-green-500/15 text-green-600">在线</Badge> : <Badge variant="outline">离线</Badge>
        ) : (
          <Badge variant="outline" className="bg-red-500/15 text-red-600">已禁用</Badge>
        )}
      </div>

      <div className="inline-flex h-9 items-center rounded-lg bg-muted p-1 w-fit">
        <button onClick={() => setTab("info")} className={`px-3 py-1 text-sm rounded-md ${tab === "info" ? "bg-background shadow" : ""}`}>基本信息</button>
        <button onClick={() => setTab("upgrade")} className={`px-3 py-1 text-sm rounded-md ${tab === "upgrade" ? "bg-background shadow" : ""}`}>升级</button>
        <button onClick={() => setTab("tasks")} className={`px-3 py-1 text-sm rounded-md ${tab === "tasks" ? "bg-background shadow" : ""}`}>任务历史</button>
      </div>

      {tab === "info" && (
        <Card>
          <CardHeader><CardTitle>基本信息</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div><span className="text-muted-foreground">ID：</span><span className="font-mono text-xs break-all">{agent.id}</span></div>
              <div><span className="text-muted-foreground">系统：</span>{agent.os || "-"}</div>
              <div><span className="text-muted-foreground">版本：</span>{agent.version || "-"}</div>
              <div><span className="text-muted-foreground">最后心跳：</span>{agent.last_seen_at ? new Date(agent.last_seen_at).toLocaleString() : "从未连接"}</div>
              <div><span className="text-muted-foreground">创建时间：</span>{new Date(agent.created_at).toLocaleString()}</div>
            </div>
            <div className="space-y-2">
              <Label>重命名</Label>
              <div className="flex gap-2">
                <Input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder={agent.name} />
                <Button onClick={() => rename.mutate(editName)} disabled={!editName.trim()}>保存</Button>
              </div>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setEnabled.mutate(!agent.enabled)}>
                {agent.enabled ? <><Ban className="mr-1 size-4" />禁用</> : <><CircleCheck className="mr-1 size-4" />启用</>}
              </Button>
              <Button variant="ghost" className="text-destructive" onClick={() => { if (confirm(`确认删除 ${agent.name}？`)) remove.mutate() }}>
                <Trash2 className="mr-1 size-4" />删除
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {tab === "upgrade" && (
        <Card>
          <CardHeader><CardTitle>升级</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div><span className="text-muted-foreground">当前版本：</span>{agent.version || "-"}</div>
              <div>
                <span className="text-muted-foreground">待升级版本：</span>
                {agent.upgrade_version && agent.upgrade_version !== agent.version ? (
                  <span>
                    {agent.upgrade_version}{" "}
                    <span className="text-xs text-muted-foreground">
                      ({agent.upgrade_requested_at ? new Date(agent.upgrade_requested_at).toLocaleString() : ""})
                    </span>
                  </span>
                ) : agent.upgrade_version && agent.upgrade_version === agent.version ? (
                  <span className="text-green-600">已是目标版本（下次心跳后自动清理）</span>
                ) : (
                  <span className="text-muted-foreground">无</span>
                )}
              </div>
              {agent.upgrade_sha256 && agent.upgrade_version !== agent.version && (
                <div className="col-span-2">
                  <span className="text-muted-foreground">待升级 SHA256：</span>
                  <span className="font-mono text-xs break-all">{agent.upgrade_sha256}</span>
                </div>
              )}
            </div>
            <div className="space-y-2">
              <Label>目标版本</Label>
              <Select value={upgradeVersion} onValueChange={(v) => setUpgradeVersion(v ?? "")}>
                <SelectTrigger className="w-full"><SelectValue placeholder="选择版本" /></SelectTrigger>
                <SelectContent className="min-w-[320px]">
                  {binaries?.map((b: any) => (
                    <SelectItem
                      key={b.version}
                      value={b.version}
                      disabled={b.version === agent.version || b.version === agent.upgrade_version}
                    >
                      {b.version}（{(b.size / 1024).toFixed(0)} KB）
                      {b.version === agent.version ? "（当前版本）" : b.version === agent.upgrade_version ? "（已待升级）" : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {upgradeVersion && upgradeVersion === agent.version && (
                <p className="text-xs text-amber-600">目标版本与当前版本一致</p>
              )}
              {upgradeVersion && upgradeVersion === agent.upgrade_version && (
                <p className="text-xs text-amber-600">该版本已是待升级版本，无需重复下发</p>
              )}
            </div>
            <Button
              onClick={() => requestUpgrade.mutate(upgradeVersion)}
              disabled={!upgradeVersion || upgradeVersion === agent.version || upgradeVersion === agent.upgrade_version}
            >
              <Rocket className="mr-2 size-4" />下发升级
            </Button>
            <p className="text-xs text-muted-foreground">Agent 将在下次轮询（约 10 秒）时下载并校验 SHA256 后自升级，成功后版本自动更新。</p>
          </CardContent>
        </Card>
      )}

      {tab === "tasks" && (
        <Card>
          <CardHeader><CardTitle>任务历史（最近 50 条）</CardTitle></CardHeader>
          <CardContent>
            <Table>
              <TableHeader><TableRow><TableHead>证书</TableHead><TableHead>状态</TableHead><TableHead>消息</TableHead><TableHead>时间</TableHead></TableRow></TableHeader>
              <TableBody>
                {!tasks || tasks.length === 0 ? (
                  <TableRow><TableCell colSpan={4} className="text-center text-muted-foreground py-6">暂无任务</TableCell></TableRow>
                ) : tasks.map((t: any) => (
                  <TableRow key={t.id}>
                    <TableCell className="truncate font-mono text-xs" title={t.certificate_id}>
                      {t.certificate_domain || t.certificate_id}
                    </TableCell>
                    <TableCell><Badge variant="outline">{t.status}</Badge></TableCell>
                    <TableCell className="truncate text-muted-foreground">{t.message || "-"}</TableCell>
                    <TableCell className="whitespace-nowrap text-xs">{new Date(t.created_at).toLocaleString()}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
