import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, PlayCircle, Pencil } from "lucide-react"
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
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
import type { AcmeAccount, ContactPerson } from "@/lib/types"

const accountSchema = z.object({
  provider: z.enum(["acme", "aliyun", "tencent"]),
  name: z.string().min(1, "请输入名称"),
  directory_url: z.string().optional(),
  contact: z.string().optional(),
  eab_kid: z.string().optional(),
  eab_hmac_key: z.string().optional(),
  access_key_id: z.string().optional(),
  access_key_secret: z.string().optional(),
  region: z.string().optional(),
  secret_id: z.string().optional(),
  secret_key: z.string().optional(),
  dv_auth_method: z.string().optional(),
})

type AccountForm = z.infer<typeof accountSchema>

export function AcmeAccountsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<AcmeAccount | null>(null)

  const { data: accounts, isLoading } = useQuery({
    queryKey: ["acme-accounts"],
    queryFn: async () => {
      const res = await api.get<AcmeAccount[]>("/acme-accounts")
      return res.data
    },
  })

  const { data: contacts } = useQuery({
    queryKey: ["contacts"],
    queryFn: async () => {
      const res = await api.get<ContactPerson[]>("/settings/contacts")
      return res.data
    },
  })

  // 从申请人信息选择的联系人 id（仅用于回填/展示，提交值仍是邮箱字符串）
  // 特殊值 "__custom" 表示选择自定义邮箱
  const [selectedContactId, setSelectedContactId] = useState<string>("")
  const selectedContact = contacts?.find((c) => c.id === selectedContactId)

  // contacts 异步加载后初始化默认选中（新建/编辑）
  useEffect(() => {
    if (!contacts) return
    if (editing) {
      const matched = contacts.find((c) => c.email === editing.contact)
      setSelectedContactId(matched ? matched.id : "__custom")
      return
    }
    if (contacts.length === 0) {
      setSelectedContactId("__custom")
    } else if (contacts.length === 1) {
      setSelectedContactId(contacts[0].id)
      setValue("contact", contacts[0].email)
    } else {
      setSelectedContactId("")
    }
  }, [contacts, editing])

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<AccountForm>({
    resolver: zodResolver(accountSchema),
    defaultValues: {
      provider: "acme",
      directory_url: "https://acme-v02.api.letsencrypt.org/directory",
    },
  })

  const openCreate = () => {
    setEditing(null)
    setSelectedContactId("")
    reset({
      provider: "acme",
      directory_url: "https://acme-v02.api.letsencrypt.org/directory",
      dv_auth_method: "DNS_AUTO",
    })
    setDialogOpen(true)
  }

  const openEdit = async (account: AcmeAccount) => {
    setEditing(account)
    setSelectedContactId(contacts?.find((c) => c.email === account.contact)?.id ?? "")
    reset({
      provider: account.provider as AccountForm["provider"],
      name: account.name,
      directory_url: account.directory_url,
      contact: account.contact,
      eab_kid: account.eab_kid,
      dv_auth_method: "DNS_AUTO",
      ...(account.config ?? {}),
    })
    setDialogOpen(true)
  }

  const create = useMutation({
    mutationFn: async (values: AccountForm) => {
      const payload: Record<string, unknown> = {
        provider: values.provider,
        name: values.name,
      }
      if (values.provider === "acme") {
        payload.directory_url = values.directory_url || "https://acme-v02.api.letsencrypt.org/directory"
        payload.contact = values.contact
        payload.eab_kid = values.eab_kid
        payload.eab_hmac_key = values.eab_hmac_key
      } else if (values.provider === "aliyun") {
        const creds: Record<string, string> = {}
        if (values.access_key_id) creds.access_key_id = values.access_key_id
        if (values.access_key_secret) creds.access_key_secret = values.access_key_secret
        if (values.region) creds.region = values.region
        payload.credentials = creds
      } else if (values.provider === "tencent") {
        const creds: Record<string, string> = {}
        if (values.secret_id) creds.secret_id = values.secret_id
        if (values.secret_key) creds.secret_key = values.secret_key
        if (values.dv_auth_method) creds.dv_auth_method = values.dv_auth_method
        payload.credentials = creds
      }
      if (editing) {
        await api.put(`/acme-accounts/${editing.id}`, payload)
      } else {
        await api.post("/acme-accounts", payload)
      }
    },
    onSuccess: () => {
      toast.success(editing ? "账户已更新" : "账户创建成功")
      queryClient.invalidateQueries({ queryKey: ["acme-accounts"] })
      setDialogOpen(false)
      setEditing(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const testAccount = useMutation({
    mutationFn: async (id: string) => {
      await api.post(`/acme-accounts/${id}/test`)
    },
    onSuccess: () => {
      toast.success("连接测试通过")
      queryClient.invalidateQueries({ queryKey: ["acme-accounts"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/acme-accounts/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["acme-accounts"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>SSL 渠道账户</CardTitle>
        <Button onClick={openCreate}>
          <Plus className="mr-2 size-4" />
          新建账户
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>渠道</TableHead>
              <TableHead>目录</TableHead>
              <TableHead>EAB</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground">加载中...</TableCell>
              </TableRow>
            )}
            {!isLoading && (!accounts || accounts.length === 0) && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                  暂无渠道账户
                </TableCell>
              </TableRow>
            )}
            {accounts?.map((a) => (
              <TableRow key={a.id}>
                <TableCell className="font-medium">{a.name}</TableCell>
                <TableCell>{a.provider}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground max-w-[240px] truncate">
                  {a.directory_url}
                </TableCell>
                <TableCell>
                  {a.provider === "acme" ? (
                    a.has_eab ? (
                      <Badge variant="outline">EAB 已配置</Badge>
                    ) : (
                      <span className="text-muted-foreground">-</span>
                    )
                  ) : a.has_credentials ? (
                    <Badge variant="outline">凭据已配置</Badge>
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
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => testAccount.mutate(a.id)}
                    disabled={testAccount.isPending}
                  >
                    <PlayCircle className="mr-1 size-4" />
                    测试
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => openEdit(a)}>
                    <Pencil className="mr-1 size-4" />
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={() => {
                      if (confirm(`确认删除账户 ${a.name}？`)) remove.mutate(a.id)
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
            <DialogTitle>{editing ? "编辑渠道账户" : "新建渠道账户"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "修改渠道账户信息；凭据留空表示保持不变"
                : "配置证书签发渠道（ACME / 阿里云 / 腾讯云）"}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => create.mutate(v))}>
            <div className="space-y-2">
              <Label>渠道类型</Label>
              <Select
                value={watch("provider")}
                onValueChange={(v) => setValue("provider", v as AccountForm["provider"])}
              >
                <SelectTrigger>
                  <SelectValue>
                    {(v) =>
                      v === "aliyun"
                        ? "阿里云 SSL 证书"
                        : v === "tencent"
                          ? "腾讯云 SSL 证书"
                          : "ACME（Let's Encrypt 等）"
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="acme">ACME（Let's Encrypt 等）</SelectItem>
                  <SelectItem value="aliyun">阿里云 SSL 证书</SelectItem>
                  <SelectItem value="tencent">腾讯云 SSL 证书</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>名称</Label>
              <Input placeholder="渠道名称" {...register("name")} />
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>

            {watch("provider") === "acme" && (
              <>
                <div className="space-y-2">
                  <Label>ACME 目录 URL</Label>
                  <Input placeholder="https://acme-v02.api.letsencrypt.org/directory" {...register("directory_url")} />
                </div>
                <div className="space-y-2">
                  <Label>联系邮箱</Label>
                  {contacts && contacts.length > 0 ? (
                    <>
                      <RadioGroup
                        value={selectedContactId}
                        onValueChange={(v) => {
                          const id = v ?? ""
                          setSelectedContactId(id)
                          if (id && id !== "__custom") {
                            const c = contacts.find((x) => x.id === id)
                            if (c) setValue("contact", c.email)
                          } else if (id === "__custom") {
                            setValue("contact", "")
                          }
                        }}
                      >
                        {contacts.map((c) => (
                          <label
                            key={c.id}
                            className="flex cursor-pointer items-center gap-2 text-sm"
                          >
                            <RadioGroupItem value={c.id} />
                            <span>
                              {c.name}（{c.email}）
                            </span>
                          </label>
                        ))}
                        <label className="flex cursor-pointer items-center gap-2 text-sm">
                          <RadioGroupItem value="__custom" />
                          <span>自定义邮箱</span>
                        </label>
                      </RadioGroup>
                      {selectedContact && (
                        <p className="text-xs text-muted-foreground">
                          已从申请人信息回填：{selectedContact.name}（{selectedContact.email}）
                        </p>
                      )}
                      {selectedContactId === "__custom" && (
                        <Input
                          type="email"
                          placeholder="admin@example.com"
                          {...register("contact")}
                        />
                      )}
                    </>
                  ) : (
                    <>
                      <Input
                        type="email"
                        placeholder="admin@example.com"
                        {...register("contact")}
                      />
                      <p className="text-xs text-muted-foreground">
                        未配置申请人，可自定义邮箱；也可到 系统→申请人信息 添加
                      </p>
                    </>
                  )}
                </div>
                <div className="space-y-2">
                  <Label>EAB KID</Label>
                  <Input {...register("eab_kid")} />
                </div>
                <div className="space-y-2">
                  <Label>EAB HMAC Key</Label>
                  <Input type="password" {...register("eab_hmac_key")} />
                </div>
              </>
            )}

            {watch("provider") === "aliyun" && (
              <>
                <div className="space-y-2">
                  <Label>AccessKey ID</Label>
                  <Input {...register("access_key_id")} />
                </div>
                <div className="space-y-2">
                  <Label>AccessKey Secret</Label>
                  <Input type="password" {...register("access_key_secret")} />
                </div>
                <div className="space-y-2">
                  <Label>地域（默认 cn-hangzhou）</Label>
                  <Input placeholder="cn-hangzhou" {...register("region")} />
                </div>
                <p className="text-xs text-muted-foreground">
                  免费证书按单域名签发；域名需托管在阿里云 DNS（自动验证）。申请人信息请到 系统→申请人信息 配置。
                </p>
              </>
            )}

            {watch("provider") === "tencent" && (
              <>
                <div className="space-y-2">
                  <Label>SecretId</Label>
                  <Input {...register("secret_id")} />
                </div>
                <div className="space-y-2">
                  <Label>SecretKey</Label>
                  <Input type="password" {...register("secret_key")} />
                </div>
                <div className="space-y-2">
                  <Label>域名验证方式</Label>
                  <Select
                    value={watch("dv_auth_method") || "DNS_AUTO"}
                    onValueChange={(v) => setValue("dv_auth_method", v ?? "DNS_AUTO")}
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {(v) =>
                          v === "DNS"
                            ? "手动添加 TXT 记录"
                            : "DNS 自动验证（域名托管腾讯云 DNS）"
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="DNS_AUTO">DNS 自动验证（域名托管腾讯云 DNS）</SelectItem>
                      <SelectItem value="DNS">手动添加 TXT 记录</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <p className="text-xs text-muted-foreground">
                  免费证书（TrustAsia C1 DV）按单域名签发，有效期 3 个月。申请人信息请到 系统→申请人信息 配置。
                </p>
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
