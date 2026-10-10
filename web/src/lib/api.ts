import axios, { AxiosError } from "axios"
import { useAuthStore } from "@/stores/auth-store"

export const api = axios.create({
  baseURL: "/api/v1",
  withCredentials: true,
})

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let refreshPromise: Promise<boolean> | null = null

// restoreSession 通过刷新 cookie 恢复会话（应用启动/刷新页面时调用）
export async function restoreSession(): Promise<boolean> {
  if (refreshPromise) return refreshPromise
  refreshPromise = (async () => {
    try {
      // 裸 axios 不经响应拦截器：手动解包信封 {code, data:{access_token, user}}
      const res = await axios.post<{ code: number; data: { access_token: string; user: AuthUser; must_change_password?: boolean } }>(
        "/api/v1/auth/refresh",
        {},
        { withCredentials: true },
      )
      const body = res.data
      if (body.code !== 0 || !body.data?.access_token) {
        useAuthStore.getState().clearAuth()
        return false
      }
      useAuthStore.getState().setAuth(
        body.data.access_token,
        body.data.user,
        body.data.must_change_password,
      )
      return true
    } catch {
      useAuthStore.getState().clearAuth()
      return false
    } finally {
      refreshPromise = null
    }
  })()
  return refreshPromise
}

// refreshSession 401 拦截器专用（与 restoreSession 同实现，供兼容引用）
async function refreshSession(): Promise<boolean> {
  return restoreSession()
}

interface AuthUser {
  id: string
  username: string
  role: string
}

// Page 分页信封（data 层）
export interface Page<T> {
  items: T[]
  total: number
  limit: number
  offset: number
}

api.interceptors.response.use(
  (response) => {
    // blob / 非 JSON 响应不解包
    if (response.config.responseType === "blob") return response
    const body = response.data as { code?: number; data?: unknown } | undefined
    if (body && typeof body === "object" && "code" in body && body.code === 0) {
      return { ...response, data: body.data }
    }
    return response
  },
  async (error: AxiosError) => {
    const original = error.config as (typeof error.config & { _retry?: boolean }) | undefined
    // 登录/刷新端点自身的 401 属于正常业务响应，不触发刷新重试（否则失败登录会多打一次 refresh）
    const isAuthEndpoint =
      !!original?.url &&
      (original.url.startsWith("/auth/login") || original.url.startsWith("/auth/refresh"))
    if (error.response?.status === 401 && original && !original._retry && !isAuthEndpoint) {
      original._retry = true
      const ok = await refreshSession()
      if (ok) {
        const token = useAuthStore.getState().accessToken
        original.headers.Authorization = `Bearer ${token}`
        return api(original)
      }
    }
    return Promise.reject(error)
  },
)

const DEFAULT_ERROR_MESSAGE = "请求失败，请稍后重试"

// 从错误信封对象读取后端 message；Blob 无法同步读取，由 getErrorMessageAsync 处理
function readErrorMessage(payload: unknown): string | undefined {
  if (!payload || typeof payload !== "object" || payload instanceof Blob) return undefined
  const body = payload as { message?: unknown; error?: unknown }
  if (typeof body.message === "string" && body.message) return body.message
  if (typeof body.error === "string" && body.error) return body.error
  return undefined
}

export function getErrorMessage(
  error: unknown,
  fallback: string = DEFAULT_ERROR_MESSAGE,
): string {
  if (axios.isAxiosError(error)) {
    // 错误信封：{code, message, trace_id, timestamp, data}
    return readErrorMessage(error.response?.data) ?? fallback
  }
  return fallback
}

// responseType: "blob" 下载失败时错误信封是 Blob，需 await text() 再按 JSON 解析 message
export async function getErrorMessageAsync(
  error: unknown,
  fallback: string = DEFAULT_ERROR_MESSAGE,
): Promise<string> {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data
    if (data instanceof Blob) {
      try {
        return readErrorMessage(JSON.parse(await data.text()) as unknown) ?? fallback
      } catch {
        // 非 JSON 响应体（如纯文本/空）→ 使用兜底文案
      }
      return fallback
    }
    return readErrorMessage(data) ?? fallback
  }
  return fallback
}
