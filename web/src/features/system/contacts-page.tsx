import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, ContactRound, Pencil } from "lucide-react"
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
import type { ContactPerson } from "@/lib/types"

const contactSchema = z.object({
  name: z.string().min(1, "请输入姓名"),
  email: z.string().email("邮箱格式不正确"),
  phone: z.string().min(1, "请输入电话"),
})

type ContactForm = z.infer<typeof contactSchema>

export function ContactsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ContactPerson | null>(null)

  const { data: contacts, isLoading, error } = useQuery({
    queryKey: ["contacts"],
    queryFn: async () => {
      const res = await api.get<ContactPerson[]>("/settings/contacts")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ContactForm>({
    resolver: zodResolver(contactSchema),
  })

  const openCreate = () => {
    setEditing(null)
    reset({ name: "", email: "", phone: "" })
    setDialogOpen(true)
  }

  const openEdit = (c: ContactPerson) => {
    setEditing(c)
    reset({ name: c.name, email: c.email, phone: c.phone })
    setDialogOpen(true)
  }

  const save = useMutation({
    mutationFn: async (values: ContactForm) => {
      if (editing) {
        await api.put(`/settings/contacts/${editing.id}`, values)
      } else {
        await api.post("/settings/contacts", values)
      }
    },
    onSuccess: () => {
      toast.success(editing ? "申请人已更新" : "申请人已创建")
      queryClient.invalidateQueries({ queryKey: ["contacts"] })
      setDialogOpen(false)
      setEditing(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/settings/contacts/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["contacts"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>申请人信息</CardTitle>
        <Button onClick={openCreate}>
          <Plus className="mr-2 size-4" />
          新增申请人
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>姓名</TableHead>
              <TableHead>邮箱</TableHead>
              <TableHead>电话</TableHead>
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
            {error && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-destructive py-8">
                  加载失败：{getErrorMessage(error)}
                </TableCell>
              </TableRow>
            )}
            {!isLoading && !error && (!contacts || contacts.length === 0) && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  <ContactRound className="mx-auto mb-2 size-8 opacity-50" />
                  暂无申请人，点击"新增申请人"添加
                </TableCell>
              </TableRow>
            )}
            {contacts?.map((c) => (
              <TableRow key={c.id}>
                <TableCell className="font-medium">{c.name}</TableCell>
                <TableCell>{c.email}</TableCell>
                <TableCell>{c.phone}</TableCell>
                <TableCell className="text-muted-foreground">{new Date(c.created_at).toLocaleDateString()}</TableCell>
                <TableCell className="text-right space-x-1">
                  <Button variant="outline" size="sm" onClick={() => openEdit(c)}>
                    <Pencil className="mr-1 size-4" />
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={() => {
                      if (confirm(`确认删除申请人 ${c.name}？`)) remove.mutate(c.id)
                    }}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <p className="mt-4 text-xs text-muted-foreground">
          用于阿里云 / 腾讯云证书签发（申请人信息），签发时可选择具体联系人；ACME 渠道无需配置。
        </p>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑申请人" : "新增申请人"}</DialogTitle>
            <DialogDescription>证书签发使用的申请人姓名、邮箱与电话</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => save.mutate(v))}>
            <div className="space-y-2">
              <Label>姓名</Label>
              <Input placeholder="张三" {...register("name")} />
              {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>邮箱</Label>
              <Input type="email" placeholder="name@example.com" {...register("email")} />
              {errors.email && <p className="text-sm text-destructive">{errors.email.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>电话</Label>
              <Input placeholder="13800138000" {...register("phone")} />
              {errors.phone && <p className="text-sm text-destructive">{errors.phone.message}</p>}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {save.isPending ? (editing ? "保存中..." : "创建中...") : editing ? "保存" : "创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}