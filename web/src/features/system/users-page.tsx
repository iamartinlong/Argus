import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, UserRound, Ban, CircleCheck } from "lucide-react"
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
import { useAuthStore } from "@/stores/auth-store"

interface User {
  id: string
  username: string
  role: string
  status: string
  created_at: string
}

const roleNames: Record<string, string> = {
  admin: "管理员",
  operator: "操作员",
  readonly: "只读",
}

const userSchema = z.object({
  username: z.string().min(2, "用户名至少 2 位").max(64),
  password: z
    .string()
    .min(12, "密码至少 12 位")
    .regex(/[a-z]/, "需包含小写字母")
    .regex(/[A-Z]/, "需包含大写字母")
    .regex(/[0-9]/, "需包含数字"),
  role: z.enum(["admin", "operator", "readonly"]),
})

type UserForm = z.infer<typeof userSchema>

export function UsersPage() {
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((s) => s.user)
  const [createOpen, setCreateOpen] = useState(false)

  const { data: users } = useQuery({
    queryKey: ["users"],
    queryFn: async () => {
      const res = await api.get<User[]>("/users")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<UserForm>({
    resolver: zodResolver(userSchema),
    defaultValues: { role: "operator" },
  })

  const create = useMutation({
    mutationFn: async (values: UserForm) => {
      await api.post("/users", values)
    },
    onSuccess: () => {
      toast.success("用户创建成功")
      queryClient.invalidateQueries({ queryKey: ["users"] })
      setCreateOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const update = useMutation({
    mutationFn: async ({ id, role, status }: { id: string; role?: string; status?: string }) => {
      await api.put(`/users/${id}`, { role, status })
    },
    onSuccess: () => {
      toast.success("已更新")
      queryClient.invalidateQueries({ queryKey: ["users"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/users/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["users"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>用户管理</CardTitle>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="mr-2 size-4" />
          新建用户
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>用户名</TableHead>
              <TableHead>角色</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {!users || users.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  <UserRound className="mx-auto mb-2 size-8 opacity-50" />
                  暂无用户
                </TableCell>
              </TableRow>
            ) : (
              users.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="font-medium">
                    {u.username}
                    {u.id === currentUser?.id && (
                      <Badge variant="outline" className="ml-2">当前</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <Select
                      value={u.role}
                      onValueChange={(v) => update.mutate({ id: u.id, role: v ?? undefined })}
                      disabled={u.id === currentUser?.id}
                    >
                      <SelectTrigger className="w-32">
                        <SelectValue>{roleNames[u.role] ?? u.role}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="admin">管理员</SelectItem>
                        <SelectItem value="operator">操作员</SelectItem>
                        <SelectItem value="readonly">只读</SelectItem>
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    {u.status === "active" ? (
                      <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">启用</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-zinc-500/15 text-zinc-600 dark:text-zinc-400">禁用</Badge>
                    )}
                  </TableCell>
                  <TableCell>{new Date(u.created_at).toLocaleDateString()}</TableCell>
                  <TableCell className="text-right space-x-1">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={u.id === currentUser?.id}
                      onClick={() =>
                        update.mutate({
                          id: u.id,
                          status: u.status === "active" ? "disabled" : "active",
                        })
                      }
                    >
                      {u.status === "active" ? (
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
                      variant="ghost"
                      size="sm"
                      className="text-destructive"
                      disabled={u.id === currentUser?.id}
                      onClick={() => {
                        if (confirm(`确认删除用户 ${u.username}？删除后其会话与 Token 将失效`)) {
                          remove.mutate(u.id)
                        }
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
          禁用用户立即吊销其全部会话；密码需 ≥12 位并包含大小写字母与数字。
        </p>
      </CardContent>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建用户</DialogTitle>
            <DialogDescription>创建后用户需使用密码登录</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => create.mutate(v))}>
            <div className="space-y-2">
              <Label>用户名</Label>
              <Input placeholder="ops-user" {...register("username")} />
              {errors.username && <p className="text-sm text-destructive">{errors.username.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>初始密码</Label>
              <Input type="password" placeholder="至少 12 位，含大小写与数字" {...register("password")} />
              {errors.password && <p className="text-sm text-destructive">{errors.password.message}</p>}
            </div>
            <div className="space-y-2">
              <Label>角色</Label>
              <Select value={watch("role")} onValueChange={(v) => setValue("role", v as UserForm["role"])}>
                <SelectTrigger>
                  <SelectValue>{(v) => roleNames[v as string] ?? v}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="admin">管理员（全部权限）</SelectItem>
                  <SelectItem value="operator">操作员（证书/部署/规则）</SelectItem>
                  <SelectItem value="readonly">只读（仅查询）</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? "创建中..." : "创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
