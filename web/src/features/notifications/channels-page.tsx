import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { BellRing, Pencil, Plus, Send, Trash2 } from "lucide-react"
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
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, getErrorMessage } from "@/lib/api"

interface ChannelRecord {
  id: string
  name: string
  type: string
  enabled: boolean
  config?: Record<string, unknown>
  created_at: string
}

const channelMeta: Record<string, { name: string }> = {
  smtp: { name: "邮件 SMTP" },
  webhook: { name: "通用 Webhook" },
  wecom: { name: "企业微信机器人" },
  dingtalk: { name: "钉钉机器人" },
  feishu: { name: "飞书机器人" },
}

interface FieldDef {
  key: string
  label: string
  type?: "text" | "number" | "password" | "switch"
  required?: boolean
  placeholder?: string
}

const fieldsByType: Record<string, FieldDef[]> = {
  smtp: [
    { key: "host", label: "SMTP 服务器", required: true, placeholder: "smtp.example.com" },
    { key: "port", label: "端口", type: "number", placeholder: "465" },
    { key: "username", label: "用户名", placeholder: "noreply@example.com" },
    { key: "password", label: "密码", type: "password" },
    { key: "from", label: "发件人", placeholder: "Argus <noreply@example.com>" },
    { key: "use_tls", label: "使用隐式 TLS（465 端口）", type: "switch" },
  ],
  webhook: [
    { key: "url", label: "Webhook URL", required: true, placeholder: "https://example.com/hook" },
    { key: "secret", label: "签名密钥（可选）", type: "password" },
  ],
  wecom: [
    { key: "webhook_url", label: "Webhook URL", required: true, placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..." },
    { key: "key", label: "签名密钥（可选）", type: "password" },
  ],
  dingtalk: [
    { key: "webhook_url", label: "Webhook URL", required: true, placeholder: "https://oapi.dingtalk.com/robot/send?access_token=..." },
    { key: "key", label: "加签 Secret（可选）", type: "password" },
  ],
  feishu: [
    { key: "webhook_url", label: "Webhook URL", required: true, placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/..." },
  ],
}

const channelSchema = z
  .object({
    name: z.string().min(1, "请输入渠道名称"),
    type: z.enum(["smtp", "webhook", "wecom", "dingtalk", "feishu"]),
    enabled: z.boolean(),
  })
  // 保留动态字段（url/host/webhook_url 等），供 superRefine 校验
  .catchall(z.unknown())
  .superRefine((data, ctx) => {
    // 动态必填字段校验（按所选类型）
    for (const f of fieldsByType[data.type] ?? []) {
      if (f.required) {
        const v = String((data as unknown as Record<string, unknown>)[f.key] ?? "").trim()
        if (!v) {
          ctx.addIssue({ code: z.ZodIssueCode.custom, path: [f.key], message: `${f.label}为必填项` })
        }
      }
    }
  })

type ChannelForm = z.infer<typeof channelSchema>

export function ChannelsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ChannelRecord | null>(null)

  const { data: channels, isLoading } = useQuery({
    queryKey: ["notification-channels"],
    queryFn: async () => {
      const res = await api.get<ChannelRecord[]>("/notification-channels")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    getValues,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<ChannelForm>({
    resolver: zodResolver(channelSchema),
    defaultValues: { name: "", type: "webhook", enabled: true },
  })

  // 构建渠道配置：提交时读取表单最新值（不依赖渲染期 watch 循环）
  const buildConfig = (type: string): Record<string, unknown> => {
    return fieldsByType[type].reduce<Record<string, unknown>>((acc, f) => {
      const v = getValues(f.key as never)
      acc[f.key] = v ?? (f.type === "switch" ? false : "")
      return acc
    }, {})
  }

  const openCreate = () => {
    setEditing(null)
    reset({ name: "", type: "webhook", enabled: true })
    setDialogOpen(true)
  }

  const openEdit = async (c: ChannelRecord) => {
    setEditing(c)
    reset({ name: c.name, type: c.type as ChannelForm["type"], enabled: c.enabled })
    setDialogOpen(true)
    // 重新查询详情（列表不回传配置）：回填非敏感字段；敏感字段（password/secret/key）留空=保持原值
    try {
      const res = await api.get<ChannelRecord>(`/notification-channels/${c.id}`)
      const cfg = res.data.config ?? {}
      const values: Record<string, unknown> = { name: res.data.name, type: res.data.type, enabled: res.data.enabled }
      for (const f of fieldsByType[res.data.type] ?? []) {
        const v = cfg[f.key]
        values[f.key] = typeof v === "string" ? v : f.type === "switch" ? !!v : ""
      }
      reset(values as ChannelForm)
    } catch {
      // 详情查询失败：保留列表数据（配置留空=保持原值）
    }
  }

  const save = useMutation({
    mutationFn: async (values: ChannelForm) => {
      const configValues = buildConfig(values.type)
      const hasConfigValues = Object.values(configValues).some((v) => v !== "" && v !== false)
      // 编辑模式：配置留空则保持原配置（不覆盖）
      const config = editing ? (hasConfigValues ? configValues : null) : configValues
      if (editing) {
        await api.put(`/notification-channels/${editing.id}`, {
          name: values.name,
          type: values.type,
          enabled: values.enabled,
          config,
        })
      } else {
        await api.post("/notification-channels", {
          name: values.name,
          type: values.type,
          enabled: values.enabled,
          config,
        })
      }
    },
    onSuccess: () => {
      toast.success(editing ? "渠道已更新" : "渠道创建成功")
      queryClient.invalidateQueries({ queryKey: ["notification-channels"] })
      queryClient.invalidateQueries({ queryKey: ["notification-rules"] })
      setDialogOpen(false)
      setEditing(null)
      reset({ name: "", type: "webhook", enabled: true })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/notification-channels/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["notification-channels"] })
      queryClient.invalidateQueries({ queryKey: ["notification-rules"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const testChannel = useMutation({
    mutationFn: async (id: string) => {
      await api.post(`/notification-channels/${id}/test`)
    },
    onSuccess: () => toast.success("测试消息已发送，请检查渠道是否收到"),
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const channelType = watch("type")

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>通知渠道</CardTitle>
        <Button onClick={openCreate}>
          <Plus className="mr-2 size-4" />
          新增渠道
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">加载中...</TableCell>
              </TableRow>
            )}
            {!isLoading && (!channels || channels.length === 0) && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  <BellRing className="mx-auto mb-2 size-8 opacity-50" />
                  暂无通知渠道，新增后可在通知规则中选择
                </TableCell>
              </TableRow>
            )}
            {channels?.map((c) => (
              <TableRow key={c.id}>
                <TableCell className="font-medium">{c.name}</TableCell>
                <TableCell>
                  <Badge variant="outline">{channelMeta[c.type]?.name ?? c.type}</Badge>
                </TableCell>
                <TableCell>
                  {c.enabled ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">启用</Badge>
                  ) : (
                    <Badge variant="outline" className="bg-zinc-500/15 text-zinc-600 dark:text-zinc-400">停用</Badge>
                  )}
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  {new Date(c.created_at).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={testChannel.isPending}
                      onClick={() => testChannel.mutate(c.id)}
                    >
                      <Send className="size-4" />
                      测试
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => openEdit(c)}>
                      <Pencil className="size-4" />
                      编辑
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => {
                        if (confirm(`确认删除渠道 ${c.name}？相关通知规则将不再发送该渠道。`)) remove.mutate(c.id)
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑通知渠道" : "新增通知渠道"}</DialogTitle>
            <DialogDescription>
              {editing ? "编辑渠道信息；配置参数留空则保持原配置" : "配置渠道参数，保存后可在通知规则中选用"}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => save.mutate(v))}>
            <div className="space-y-2">
              <Label>渠道名称</Label>
              <Input placeholder="生产告警群机器人" {...register("name")} />
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>渠道类型</Label>
              <Select
                value={watch("type")}
                onValueChange={(v) => setValue("type", v as ChannelForm["type"])}
              >
                <SelectTrigger>
                  <SelectValue>{(v) => channelMeta[v as string]?.name ?? v}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="smtp">邮件 SMTP</SelectItem>
                  <SelectItem value="webhook">通用 Webhook</SelectItem>
                  <SelectItem value="wecom">企业微信机器人</SelectItem>
                  <SelectItem value="dingtalk">钉钉机器人</SelectItem>
                  <SelectItem value="feishu">飞书机器人</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {fieldsByType[channelType].map((f) => (
              <div key={f.key} className="space-y-2">
                {f.type === "switch" ? (
                  <label className="flex items-center justify-between rounded-md border p-3">
                    <span className="text-sm font-medium">{f.label}</span>
                    <Switch
                      checked={!!watch(f.key as never)}
                      onCheckedChange={(v) => (setValue as unknown as (k: string, val: boolean) => void)(f.key, v)}
                    />
                  </label>
                ) : (
                  <>
                    <Label>
                      {f.label}
                      {f.required && <span className="text-destructive"> *</span>}
                    </Label>
                    <Input
                      type={f.type === "password" ? "password" : f.type === "number" ? "number" : "text"}
                      placeholder={f.placeholder}
                      {...register(f.key as never)}
                    />
                    {errors[f.key as keyof typeof errors] && (
                      <p className="text-sm text-destructive">
                        {errors[f.key as keyof typeof errors]?.message}
                      </p>
                    )}
                  </>
                )}
              </div>
            ))}
            <label className="flex items-center justify-between rounded-md border p-3">
              <span className="text-sm font-medium">启用渠道（停用后通知规则将跳过该渠道）</span>
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={watch("enabled") ?? false}
                onChange={(e) => setValue("enabled", e.target.checked)}
              />
            </label>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {save.isPending ? "保存中..." : editing ? "保存" : "创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
