import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, Pencil } from "lucide-react"
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
import type { DnsProviderAccount } from "@/lib/types"

const dnsAccountSchema = z.object({
  provider: z.enum(["alidns", "cloudflare", "dnspod", "godaddy"]),
  name: z.string().min(1, "请输入名称"),
  access_key: z.string().optional(),
  secret_key: z.string().optional(),
  email: z.string().optional(),
  api_token: z.string().optional(),
})

type DnsAccountForm = z.infer<typeof dnsAccountSchema>

export function DnsAccountsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<DnsProviderAccount | null>(null)

  const { data: accounts, isLoading } = useQuery({
    queryKey: ["dns-accounts"],
    queryFn: async () => {
      const res = await api.get<DnsProviderAccount[]>("/dns-accounts")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<DnsAccountForm>({
    resolver: zodResolver(dnsAccountSchema),
    defaultValues: { provider: "alidns" },
  })

  const openCreate = () => {
    setEditing(null)
    reset({ provider: "alidns", name: "", access_key: "", secret_key: "", email: "", api_token: "" })
    setDialogOpen(true)
  }

  const openEdit = async (account: DnsProviderAccount) => {
    setEditing(account)
    reset({
      provider: account.provider as DnsAccountForm["provider"],
      name: account.name,
      ...(account.config ?? {}),
    } as unknown as DnsAccountForm)
    // Fetch masked config for edit
      try {
      const res = await api.get<DnsProviderAccount>(`/dns-accounts/${account.id}`)
      const cfg = (res.data as unknown as { config?: Record<string, string> }).config
      if (cfg) {
        // Map stored keys to form fields
        if (cfg.ALICLOUD_ACCESS_KEY) setValue("access_key", cfg.ALICLOUD_ACCESS_KEY as string)
        if (cfg.CLOUDFLARE_DNS_API_TOKEN) setValue("api_token", cfg.CLOUDFLARE_DNS_API_TOKEN as string)
        if (cfg.DNSPOD_API_TOKEN) setValue("api_token", cfg.DNSPOD_API_TOKEN as string)
        if (cfg.GODADDY_API_KEY) setValue("access_key", cfg.GODADDY_API_KEY as string)
      }
    } catch {}
    setDialogOpen(true)
  }

  const create = useMutation({
    mutationFn: async (values: DnsAccountForm) => {
      const creds: Record<string, string> = {}
      switch (values.provider) {
        case "alidns":
          if (values.access_key) creds.ALICLOUD_ACCESS_KEY = values.access_key
          if (values.secret_key) creds.ALICLOUD_SECRET_KEY = values.secret_key
          break
        case "cloudflare":
          if (values.api_token) {
            creds.CLOUDFLARE_DNS_API_TOKEN = values.api_token
          } else {
            if (values.access_key) creds.CLOUDFLARE_API_KEY = values.access_key
            if (values.secret_key) creds.CLOUDFLARE_API_KEY = values.secret_key
            if (values.email) creds.CLOUDFLARE_EMAIL = values.email
          }
          break
        case "dnspod":
          if (values.api_token) creds.DNSPOD_API_TOKEN = values.api_token
          if (values.access_key) creds.DNSPOD_API_TOKEN = values.access_key
          break
        case "godaddy":
          if (values.access_key) creds.GODADDY_API_KEY = values.access_key
          if (values.secret_key) creds.GODADDY_API_SECRET = values.secret_key
          break
      }
      const payload = {
        provider: values.provider,
        name: values.name,
        credentials: creds,
      }
      if (editing) {
        await api.put(`/dns-accounts/${editing.id}`, payload)
      } else {
        await api.post("/dns-accounts", payload)
      }
    },
    onSuccess: () => {
      toast.success(editing ? "DNS 账户已更新" : "DNS 账户创建成功")
      queryClient.invalidateQueries({ queryKey: ["dns-accounts"] })
      setDialogOpen(false)
      setEditing(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/dns-accounts/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["dns-accounts"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>DNS 验证账户</CardTitle>
        <Button onClick={openCreate}>
          <Plus className="mr-2 size-4" />
          新建 DNS 账户
        </Button>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground mb-4">
          用于 ACME DNS-01 验证的 DNS 服务商凭据，证书签发时可复用，避免每张证书重复录入。
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>服务商</TableHead>
              <TableHead>凭据</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  加载中...
                </TableCell>
              </TableRow>
            )}
            {!isLoading && (!accounts || accounts.length === 0) && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  暂无 DNS 账户
                </TableCell>
              </TableRow>
            )}
            {accounts?.map((a) => (
              <TableRow key={a.id}>
                <TableCell className="font-medium">{a.name}</TableCell>
                <TableCell>{a.provider}</TableCell>
                <TableCell>
                  {a.has_credentials ? (
                    <Badge variant="outline">已配置</Badge>
                  ) : (
                    <span className="text-muted-foreground">未配置</span>
                  )}
                </TableCell>
                <TableCell>
                  {a.status === "active" ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">
                      正常
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400">
                      错误
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-right space-x-1">
                  <Button variant="outline" size="sm" onClick={() => openEdit(a)}>
                    <Pencil className="mr-1 size-4" />
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={() => {
                      if (confirm(`确认删除 DNS 账户 ${a.name}？`)) remove.mutate(a.id)
                    }}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑 DNS 账户" : "新建 DNS 账户"}</DialogTitle>
            <DialogDescription>
              {editing ? "修改 DNS 服务商凭据；留空表示保持不变" : "配置 DNS 服务商 API 凭据，供 ACME DNS-01 验证复用"}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => create.mutate(v))}>
            <div className="space-y-2">
              <Label>DNS 服务商</Label>
              <Select
                value={watch("provider")}
                onValueChange={(v) => setValue("provider", v as DnsAccountForm["provider"])}
                disabled={!!editing}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="alidns">阿里云 DNS (alidns)</SelectItem>
                  <SelectItem value="cloudflare">Cloudflare</SelectItem>
                  <SelectItem value="dnspod">DNSPod (腾讯云)</SelectItem>
                  <SelectItem value="godaddy">GoDaddy</SelectItem>
                </SelectContent>
              </Select>
              {editing && <p className="text-xs text-muted-foreground">创建后不可修改，如需更换请新建</p>}
            </div>
            <div className="space-y-2">
              <Label>名称</Label>
              <Input placeholder="如：主站阿里云 DNS" {...register("name")} />
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>

            {watch("provider") === "alidns" && (
              <>
                <div className="space-y-2">
                  <Label>ALICLOUD_ACCESS_KEY</Label>
                  <Input {...register("access_key")} />
                </div>
                <div className="space-y-2">
                  <Label>ALICLOUD_SECRET_KEY</Label>
                  <Input type="password" placeholder={editing ? "••••••（留空保持不变，填入新值即轮换）" : ""} {...register("secret_key")} />
                </div>
              </>
            )}
            {watch("provider") === "cloudflare" && (
              <>
                <div className="space-y-2">
                  <Label>CLOUDFLARE_DNS_API_TOKEN（推荐）</Label>
                  <Input type="password" placeholder={editing ? "••••••（留空保持不变）" : "或使用 Global API Key"} {...register("api_token")} />
                </div>
                <div className="space-y-2">
                  <Label>CLOUDFLARE_API_KEY（备选）</Label>
                  <Input type="password" placeholder={editing ? "••••••（留空保持不变）" : ""} {...register("access_key")} />
                </div>
                <div className="space-y-2">
                  <Label>CLOUDFLARE_EMAIL（配合 API Key）</Label>
                  <Input {...register("email")} />
                </div>
              </>
            )}
            {watch("provider") === "dnspod" && (
              <div className="space-y-2">
                <Label>DNSPOD_API_TOKEN</Label>
                <Input type="password" placeholder={editing ? "••••••（留空保持不变）" : ""} {...register("api_token")} />
              </div>
            )}
            {watch("provider") === "godaddy" && (
              <>
                <div className="space-y-2">
                  <Label>GODADDY_API_KEY</Label>
                  <Input type="password" placeholder={editing ? "••••••（留空保持不变）" : ""} {...register("access_key")} />
                </div>
                <div className="space-y-2">
                  <Label>GODADDY_API_SECRET</Label>
                  <Input type="password" placeholder={editing ? "••••••（留空保持不变，填入新值即轮换）" : ""} {...register("secret_key")} />
                </div>
              </>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? (editing ? "保存中..." : "创建中...") : editing ? "保存" : "创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
