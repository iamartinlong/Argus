import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { ChevronLeft, ChevronRight, Download, History, Search } from "lucide-react"
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

interface AuditLog {
  id: string
  username: string
  action: string
  resource: string
  detail: string
  ip: string
  user_agent: string
  created_at: string
}

const PAGE_SIZE = 50

const actionNames: Record<string, string> = {
  "login.success": "登录成功",
  "login.failure": "登录失败",
  "logout": "退出登录",
  "refresh": "刷新令牌",
  "password.change": "修改密码",
  "password.reset": "重置密码",
  "user.create": "创建用户",
  "user.update": "更新用户",
  "user.delete": "删除用户",
  "certificate.issue": "签发证书",
  "certificate.renew": "续期证书",
  "certificate.revoke": "吊销证书",
  "certificate.import": "导入证书",
  "certificate.delete": "删除证书",
  "certificate.key.access": "下载私钥",
  "certificate.targets.update": "更新部署目标绑定",
  "account.create": "创建账户",
  "account.delete": "删除账户",
  "account.test": "测试账户",
  "token.create": "创建 Token",
  "token.delete": "删除 Token",
  "rule.create": "创建通知规则",
  "rule.delete": "删除通知规则",
  "channel.create": "创建通知渠道",
  "channel.update": "更新通知渠道",
  "channel.delete": "删除通知渠道",
  "notify.sent": "通知已发送",
  "notify.failed": "通知失败",
  "deploy.target.create": "创建部署目标",
  "deploy.target.delete": "删除部署目标",
  "deploy.success": "部署成功",
  "deploy.failed": "部署失败",
  "agent.create": "注册 Agent",
  "agent.delete": "删除 Agent",
  "agent.enable": "启用 Agent",
  "agent.disable": "停用 Agent",
  "agent.binary.upload": "上传 Agent 二进制",
  "agent.upgrade.request": "Agent 升级请求",
}

// 分类前缀 → 动作筛选（action LIKE 前缀）
const actionGroups: Record<string, { label: string; prefix: string }> = {
  login: { label: "登录", prefix: "login." },
  user: { label: "用户与权限", prefix: "user." },
  password: { label: "密码", prefix: "password." },
  certificate: { label: "证书", prefix: "certificate." },
  account: { label: "签发账户", prefix: "account." },
  token: { label: "API Token", prefix: "token." },
  notify: { label: "通知", prefix: "notify." },
  rule: { label: "通知规则/渠道", prefix: "rule." },
  channel: { label: "通知渠道", prefix: "channel." },
  deploy: { label: "部署", prefix: "deploy." },
  agent: { label: "Agent", prefix: "agent." },
}

export function AuditLogsPage() {
  const [username, setUsername] = useState("")
  const [keyword, setKeyword] = useState("")
  const [group, setGroup] = useState("all")
  const [applied, setApplied] = useState({ username: "", keyword: "", group: "all" })
  const [page, setPage] = useState(0)

  const { data, isLoading } = useQuery({
    queryKey: ["audit-logs", applied, page],
    queryFn: async () => {
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(page * PAGE_SIZE) })
      if (applied.username) params.set("username", applied.username)
      if (applied.keyword) params.set("keyword", applied.keyword)
      if (applied.group && actionGroups[applied.group]) params.set("action", actionGroups[applied.group].prefix)
      const res = await api.get<Page<AuditLog>>(`/audit-logs?${params.toString()}`)
      return res.data
    },
  })
  const logs = data?.items

  const search = () => {
    setPage(0)
    setApplied({ username: username.trim(), keyword: keyword.trim(), group })
  }

  const exportCSV = async () => {
    try {
      const params = new URLSearchParams({ limit: "10000" })
      if (applied.username) params.set("username", applied.username)
      if (applied.keyword) params.set("keyword", applied.keyword)
      if (applied.group && actionGroups[applied.group]) params.set("action", actionGroups[applied.group].prefix)
      const res = await api.get<Blob>(`/audit-logs/export?${params.toString()}`, { responseType: "blob" })
      const url = URL.createObjectURL(res.data)
      const a = document.createElement("a")
      a.href = url
      a.download = "audit-logs.csv"
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      toast.error(await getErrorMessageAsync(err))
    }
  }

  const reset = () => {
    setUsername("")
    setKeyword("")
    setGroup("all")
    setPage(0)
    setApplied({ username: "", keyword: "", group: "all" })
  }

  const formatTime = (iso: string) => {
    const d = new Date(iso)
    return d.toLocaleString("zh-CN", { hour12: false })
  }

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>审计日志</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs text-muted-foreground">用户</Label>
            <Input
              className="w-44"
              placeholder="用户名"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs text-muted-foreground">动作分类</Label>
            <Select value={group} onValueChange={(v) => setGroup(v ?? "all")}>
              <SelectTrigger className="w-44">
                <SelectValue>
                  {(v) => (v && v !== "all" ? (actionGroups[v as string]?.label ?? v) : "全部动作")}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部动作</SelectItem>
                {Object.entries(actionGroups).map(([k, g]) => (
                  <SelectItem key={k} value={k}>{g.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-xs text-muted-foreground">关键字</Label>
            <Input
              className="w-52"
              placeholder="资源/详情/用户模糊搜索"
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
            <Button variant="outline" onClick={reset}>
              重置
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
              <TableHead className="w-28">用户</TableHead>
              <TableHead className="w-36">动作</TableHead>
              <TableHead className="w-48">资源</TableHead>
              <TableHead>详情</TableHead>
              <TableHead className="w-32">IP</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground">加载中...</TableCell>
              </TableRow>
            )}
            {!isLoading && (!logs || logs.length === 0) && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-10">
                  <History className="mx-auto mb-2 size-8 opacity-50" />
                  暂无审计日志
                </TableCell>
              </TableRow>
            )}
            {logs?.map((l) => (
              <TableRow key={l.id}>
                <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(l.created_at)}</TableCell>
                <TableCell className="font-medium">{l.username}</TableCell>
                <TableCell>
                  <Badge variant={l.action === "notify.failed" || l.action === "deploy.failed" ? "destructive" : "outline"}>
                    {actionNames[l.action] ?? l.action}
                  </Badge>
                </TableCell>
                <TableCell className="max-w-48 truncate">{l.resource || "-"}</TableCell>
                <TableCell className="max-w-md truncate text-muted-foreground" title={l.detail}>
                  {l.detail || "-"}
                </TableCell>
                <TableCell className="text-muted-foreground">{l.ip || "-"}</TableCell>
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
