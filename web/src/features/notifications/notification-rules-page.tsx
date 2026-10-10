import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, BellRing, Settings2, Pencil } from "lucide-react"
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
import { NOTIFICATION_EVENTS } from "@/lib/types"

interface NotificationRule {
  id: string
  name: string
  event: string
  channels: string[]
  channel_ids: string[]
  days_before: number[]
  enabled: boolean
  created_at: string
}

interface ChannelRecord {
  id: string
  name: string
  type: string
  enabled: boolean
}

interface NotificationTemplate {
  event: string
  title: string
  body: string
  updated_at: string
}

const channelTypeNames: Record<string, string> = {
  smtp: "邮件 SMTP",
  webhook: "Webhook",
  wecom: "企业微信",
  dingtalk: "钉钉",
  feishu: "飞书",
}

const eventNames: Record<string, string> = {
  cert_expiring: "证书到期提醒",
  renew_success: "续期成功",
  renew_failed: "续期失败",
  issue_success: "签发成功",
  issue_failed: "签发失败",
  deploy_success: "部署成功",
  deploy_failed: "部署失败",
}

const eventValues = NOTIFICATION_EVENTS.map((e) => e.value) as [
  string,
  ...string[],
]

const ruleSchema = z.object({
  name: z.string().min(1, "请输入规则名称"),
  event: z.enum(eventValues),
  channel_ids: z.array(z.string()).min(1, "请至少选择一个通知渠道"),
  days_before: z.string().optional(),
  enabled: z.boolean(),
})

type RuleForm = z.infer<typeof ruleSchema>

export function NotificationRulesPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<NotificationRule | null>(null)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [templates, setTemplates] = useState<Record<string, { title: string; body: string }>>({})

  const { data: rules, isLoading } = useQuery({
    queryKey: ["notification-rules"],
    queryFn: async () => {
      const res = await api.get<NotificationRule[]>("/notification-rules")
      return res.data
    },
  })

  const { data: channels } = useQuery({
    queryKey: ["notification-channels"],
    queryFn: async () => {
      const res = await api.get<ChannelRecord[]>("/notification-channels")
      return res.data
    },
  })

  const { data: existingTemplates } = useQuery({
    queryKey: ["notification-templates"],
    queryFn: async () => {
      const res = await api.get<NotificationTemplate[]>("/notification-templates")
      return res.data
    },
    enabled: templateOpen,
  })

  const openTemplates = () => {
    const map: Record<string, { title: string; body: string }> = {}
    existingTemplates?.forEach((t) => {
      map[t.event] = { title: t.title, body: t.body }
    })
    setTemplates(map)
    setTemplateOpen(true)
  }

  const saveTemplates = useMutation({
    mutationFn: async () => {
      const entries = Object.entries(templates)
      for (const [event, t] of entries) {
        await api.put(`/notification-templates/${event}`, { title: t.title, body: t.body })
      }
    },
    onSuccess: () => {
      toast.success("模板已保存")
      queryClient.invalidateQueries({ queryKey: ["notification-templates"] })
      setTemplateOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    reset,
    formState: { errors },
  } = useForm<RuleForm>({
    resolver: zodResolver(ruleSchema),
    defaultValues: { event: "cert_expiring", channel_ids: [], enabled: true },
  })

  const openCreate = () => {
    setEditing(null)
    reset({ name: "", event: "cert_expiring", channel_ids: [], days_before: "", enabled: true })
    setDialogOpen(true)
  }

  const openEdit = (rule: NotificationRule) => {
    setEditing(rule)
    reset({
      name: rule.name,
      event: rule.event as RuleForm["event"],
      channel_ids: rule.channel_ids,
      days_before: rule.days_before.join(","),
      enabled: rule.enabled,
    })
    setDialogOpen(true)
  }

  const save = useMutation({
    mutationFn: async (values: RuleForm) => {
      const body = {
        name: values.name,
        event: values.event,
        channel_ids: values.channel_ids,
        days_before: values.days_before
          ? values.days_before.split(/[\s,]+/).map(Number).filter((n) => n > 0)
          : [],
        enabled: values.enabled,
      }
      if (editing) {
        await api.put(`/notification-rules/${editing.id}`, body)
      } else {
        await api.post("/notification-rules", body)
      }
    },
    onSuccess: () => {
      toast.success(editing ? "规则已更新" : "规则创建成功")
      queryClient.invalidateQueries({ queryKey: ["notification-rules"] })
      setDialogOpen(false)
      setEditing(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const selectedChannelIDs = watch("channel_ids") ?? []
  const channelNameById = (id: string) =>
    channels?.find((c) => c.id === id)?.name ?? channels?.find((c) => c.id === id)?.type ?? id

  const toggleChannel = (id: string) => {
    const next = selectedChannelIDs.includes(id)
      ? selectedChannelIDs.filter((x) => x !== id)
      : [...selectedChannelIDs, id]
    setValue("channel_ids", next)
  }

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/notification-rules/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["notification-rules"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>通知规则</CardTitle>
        <div className="flex gap-2">
          <Button variant="outline" onClick={openTemplates}>
            <Settings2 className="mr-2 size-4" />
            模板设置
          </Button>
          <Button onClick={openCreate}>
            <Plus className="mr-2 size-4" />
            新建规则
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>事件</TableHead>
              <TableHead>渠道</TableHead>
              <TableHead>提前天数</TableHead>
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
            {!isLoading && (!rules || rules.length === 0) && (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                  <BellRing className="mx-auto mb-2 size-8 opacity-50" />
                  暂无通知规则，创建后系统将按规则推送通知
                </TableCell>
              </TableRow>
            )}
            {rules?.map((r) => (
              <TableRow key={r.id}>
                <TableCell className="font-medium">{r.name}</TableCell>
                <TableCell>{eventNames[r.event] ?? r.event}</TableCell>
                <TableCell>
                  <div className="flex gap-1 flex-wrap">
                    {(r.channel_ids ?? []).length > 0
                      ? (r.channel_ids ?? []).map((id) => (
                          <Badge key={id} variant="outline">{channelNameById(id)}</Badge>
                        ))
                      : (r.channels ?? []).map((c) => (
                          <Badge key={c} variant="outline">{channelTypeNames[c] ?? c}</Badge>
                        ))}
                  </div>
                </TableCell>
                <TableCell>
                  {(r.days_before ?? []).length > 0 ? (r.days_before ?? []).join(" / ") + " 天" : "-"}
                </TableCell>
                <TableCell>
                  {r.enabled ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">启用</Badge>
                  ) : (
                    <Badge variant="outline" className="bg-zinc-500/15 text-zinc-600 dark:text-zinc-400">停用</Badge>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button variant="ghost" size="sm" onClick={() => openEdit(r)}>
                      <Pencil className="size-4" />
                      编辑
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      onClick={() => {
                        if (confirm(`确认删除规则 ${r.name}？`)) remove.mutate(r.id)
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
            <DialogTitle>{editing ? "编辑通知规则" : "新建通知规则"}</DialogTitle>
            <DialogDescription>配置事件触发条件与通知渠道</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => save.mutate(v))}>
            <div className="space-y-2">
              <Label>规则名称</Label>
              <Input placeholder="生产证书到期提醒" {...register("name")} />
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>触发事件</Label>
              <Select
                value={watch("event")}
                onValueChange={(v) => setValue("event", v as RuleForm["event"])}
              >
                <SelectTrigger>
                  <SelectValue>{(v) => eventNames[v as string] ?? v}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {NOTIFICATION_EVENTS.map((e) => (
                    <SelectItem key={e.value} value={e.value}>
                      {eventNames[e.value]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>通知渠道</Label>
              {(!channels || channels.length === 0) && (
                <p className="text-sm text-muted-foreground">
                  暂无可用渠道，请先到「通知渠道」页面新增
                </p>
              )}
              <div className="space-y-2 rounded-md border p-3">
                {channels?.map((c) => (
                  <label
                    key={c.id}
                    className="flex cursor-pointer items-center justify-between gap-2 text-sm"
                  >
                    <span>
                      {c.name}
                      <span className="ml-2 text-xs text-muted-foreground">
                        {channelTypeNames[c.type] ?? c.type}
                      </span>
                    </span>
                    <input
                      type="checkbox"
                      className="size-4 accent-primary"
                      checked={selectedChannelIDs.includes(c.id)}
                      onChange={() => toggleChannel(c.id)}
                    />
                  </label>
                ))}
              </div>
              {errors.channel_ids && (
                <p className="text-sm text-destructive">{errors.channel_ids.message}</p>
              )}
            </div>
            {watch("event") === "cert_expiring" && (
              <div className="space-y-2">
                <Label>提前天数（逗号分隔，如 30,14,7,1）</Label>
                <Input placeholder="30,14,7,1" {...register("days_before")} />
              </div>
            )}
            <label className="flex items-center justify-between rounded-md border p-3">
              <span className="text-sm font-medium">启用规则</span>
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

      <Dialog open={templateOpen} onOpenChange={setTemplateOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>通知模板</DialogTitle>
<DialogDescription>
            自定义消息标题与正文；支持变量：{`{domain}`} 域名、{`{expires}`} 到期时间、{`{days_left}`} 剩余天数、
            {`{status}`} 状态、{`{target}`} 部署目标、{`{error}`} 错误信息。留空则使用默认文案
          </DialogDescription>
          </DialogHeader>
          <div className="space-y-5">
            {NOTIFICATION_EVENTS.map((e) => {
              const t = templates[e.value] ?? { title: "", body: "" }
              return (
                <div key={e.value} className="space-y-2">
                  <Label>{eventNames[e.value]}</Label>
                  <Input
                    placeholder={`标题（如：${eventNames[e.value]}：{domain}）`}
                    value={t.title}
                    onChange={(ev) =>
                      setTemplates((prev) => ({ ...prev, [e.value]: { ...t, title: ev.target.value } }))
                    }
                  />
                  <textarea
                    placeholder="正文内容"
                    className="flex min-h-20 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                    value={t.body}
                    onChange={(ev) =>
                      setTemplates((prev) => ({ ...prev, [e.value]: { ...t, body: ev.target.value } }))
                    }
                  />
                </div>
              )
            })}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setTemplateOpen(false)}>
              取消
            </Button>
            <Button type="button" disabled={saveTemplates.isPending} onClick={() => saveTemplates.mutate()}>
              {saveTemplates.isPending ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
