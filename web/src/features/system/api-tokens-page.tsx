import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, KeyRound, Copy } from "lucide-react"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, getErrorMessage } from "@/lib/api"

interface ApiToken {
  id: string
  name: string
  scopes: string[]
  expires_at: string
  last_used_at: string
  created_at: string
}

const scopeNames: Record<string, string> = {
  all: "全部权限",
  read: "只读",
  "certificates:read": "证书·读",
  "certificates:write": "证书·写",
  "accounts:read": "渠道·读",
  "accounts:write": "渠道·写",
  "deploy:read": "部署·读",
  "deploy:write": "部署·写",
  "notify:read": "通知·读",
  "notify:write": "通知·写",
  "admin:read": "系统·读",
  "admin:write": "系统·写",
}

// 可选 scope 分组（"全部"独立互斥）
const scopeGroups: { label: string; scopes: string[] }[] = [
  { label: "证书", scopes: ["certificates:read", "certificates:write"] },
  { label: "签发渠道", scopes: ["accounts:read", "accounts:write"] },
  { label: "部署", scopes: ["deploy:read", "deploy:write"] },
  { label: "通知", scopes: ["notify:read", "notify:write"] },
  { label: "系统", scopes: ["admin:read", "admin:write"] },
]

const tokenSchema = z.object({
  name: z.string().min(1, "请输入名称"),
  ttl_days: z.string().optional(),
})

type TokenForm = z.infer<typeof tokenSchema>

export function ApiTokensPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [selectedScopes, setSelectedScopes] = useState<string[]>(["all"])

  const { data: tokens } = useQuery({
    queryKey: ["api-tokens"],
    queryFn: async () => {
      const res = await api.get<ApiToken[]>("/api-tokens")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<TokenForm>({ resolver: zodResolver(tokenSchema) })

  const create = useMutation({
    mutationFn: async (values: TokenForm) => {
      if (selectedScopes.length === 0) {
        throw new Error("请至少选择一项权限范围")
      }
      const res = await api.post<{ token: string }>("/api-tokens", {
        name: values.name,
        ttl_days: values.ttl_days ? Number(values.ttl_days) : 0,
        scopes: selectedScopes,
      })
      return res.data.token
    },
    onSuccess: (token) => {
      setCreatedToken(token)
      queryClient.invalidateQueries({ queryKey: ["api-tokens"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const toggleAll = () => {
    setSelectedScopes(selectedScopes.includes("all") ? [] : ["all"])
  }

  const toggleScope = (s: string) => {
    if (selectedScopes.includes(s)) {
      setSelectedScopes(selectedScopes.filter((x) => x !== s))
    } else {
      setSelectedScopes([...selectedScopes.filter((x) => x !== "all"), s])
    }
  }

  const revoke = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/api-tokens/${id}`)
    },
    onSuccess: () => {
      toast.success("已吊销")
      queryClient.invalidateQueries({ queryKey: ["api-tokens"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>API Token</CardTitle>
        <Button onClick={() => setDialogOpen(true)}>
          <Plus className="mr-2 size-4" />
          创建 Token
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>权限</TableHead>
              <TableHead>过期时间</TableHead>
              <TableHead>最后使用</TableHead>
              <TableHead>创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {!tokens || tokens.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                  <KeyRound className="mx-auto mb-2 size-8 opacity-50" />
                  暂无 API Token（用于 CLI 远程模式与脚本集成）
                </TableCell>
              </TableRow>
            ) : (
              tokens.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">{t.name}</TableCell>
                  <TableCell>
                    <div className="flex gap-1 flex-wrap">
                      {t.scopes.map((s) => (
                        <Badge key={s} variant="outline">{scopeNames[s] ?? s}</Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell>
                    {t.expires_at ? new Date(t.expires_at).toLocaleDateString() : "永不过期"}
                  </TableCell>
                  <TableCell>
                    {t.last_used_at ? new Date(t.last_used_at).toLocaleString() : "未使用"}
                  </TableCell>
                  <TableCell>{new Date(t.created_at).toLocaleDateString()}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => {
                        if (confirm(`确认吊销 Token ${t.name}？吊销后立即失效`)) revoke.mutate(t.id)
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
          使用方式：请求头 <code className="font-mono">X-API-Token: &lt;token&gt;</code>，用于 CLI 远程模式与自动化脚本。
        </p>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建 API Token</DialogTitle>
            <DialogDescription>Token 创建后仅显示一次，请立即保存</DialogDescription>
          </DialogHeader>
          {createdToken ? (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Token（仅此一次）</Label>
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
                    setSelectedScopes(["all"])
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
                <Input placeholder="CI 部署脚本" {...register("name")} />
                {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
              </div>
              <div className="space-y-2">
                <Label>有效期（天，留空永不过期）</Label>
                <Input type="number" placeholder="90" {...register("ttl_days")} />
              </div>
              <div className="space-y-2">
                <Label>权限范围</Label>
                <div className="space-y-1 rounded-md border p-3">
                  <label className="flex cursor-pointer items-center justify-between text-sm">
                    <span>全部权限（继承用户角色）</span>
                    <input
                      type="checkbox"
                      className="size-4 accent-primary"
                      checked={selectedScopes.includes("all")}
                      onChange={toggleAll}
                    />
                  </label>
                  {scopeGroups.map((g) => (
                    <div key={g.label} className="flex items-center justify-between text-sm">
                      <span className="text-muted-foreground">{g.label}</span>
                      <div className="flex gap-3">
                        {g.scopes.map((s) => (
                          <label key={s} className="flex cursor-pointer items-center gap-1.5">
                            <span className="text-xs">{scopeNames[s]?.split("·")[1]}</span>
                            <input
                              type="checkbox"
                              className="size-4 accent-primary"
                              checked={selectedScopes.includes(s)}
                              onChange={() => toggleScope(s)}
                            />
                          </label>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
                {selectedScopes.length === 0 && (
                  <p className="text-xs text-destructive">请至少选择一项权限</p>
                )}
              </div>
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setDialogOpen(false)
                    setSelectedScopes(["all"])
                  }}
                >
                  取消
                </Button>
                <Button
                  type="submit"
                  disabled={create.isPending || selectedScopes.length === 0}
                >
                  {create.isPending ? "创建中..." : "创建"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
