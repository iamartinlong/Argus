import axios from "axios"

// 安装向导专用 client：不带鉴权头、不触发登录刷新（未初始化时无会话）。
const setupApi = axios.create({
  baseURL: "/api/v1/setup",
  withCredentials: true,
  timeout: 15000,
})

setupApi.interceptors.response.use((response) => {
  const body = response.data as { code?: number; data?: unknown } | undefined
  if (body && typeof body === "object" && "code" in body && body.code === 0) {
    return { ...response, data: body.data }
  }
  return response
})

export interface SetupStatus {
  initialized: boolean
  version: string
  jwt_from_env: boolean
  master_from_env: boolean
  min_password_len: number
  config_file: boolean
  running_as_service: boolean
  setup_only: boolean
}

export interface SetupInitResult {
  ok: boolean
  admin_username: string
  needs_restart: boolean
  secrets_changed: boolean
  /** 安装模式重启令牌：调用 /restart 时必须携带（初始化后匿名重启被拒绝） */
  restart_token?: string
}

export interface SetupRestartResult {
  restarting?: boolean
  needs_manual_restart?: boolean
  message?: string
  powershell?: string
}

export async function fetchSetupStatus(): Promise<SetupStatus> {
  const res = await setupApi.get<SetupStatus>("/status")
  return res.data
}

export async function generateSetupKeys(): Promise<{ jwt_secret: string; master_key: string }> {
  const res = await setupApi.post<{ jwt_secret: string; master_key: string }>("/generate-keys")
  return res.data
}

export interface SetupInitInput {
  jwt_secret?: string
  master_key?: string
  min_password_len?: number
  max_login_failures?: number
  lockout_minutes?: number
  admin_username: string
  admin_password: string
}

export async function submitSetupInit(input: SetupInitInput): Promise<SetupInitResult> {
  const res = await setupApi.post<SetupInitResult>("/init", input)
  return res.data
}

// 安装模式重启：需携带 /setup/init 返回的 restart_token（完整模式走管理员鉴权，无需令牌）。
export async function publicSetupRestart(token?: string): Promise<SetupRestartResult> {
  const headers = token ? { "X-Setup-Token": token } : undefined
  const res = await setupApi.post<SetupRestartResult>("/restart", {}, { headers })
  return res.data
}

export function getSetupErrorMessage(error: unknown): string {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data as { message?: string; error?: string } | undefined
    return data?.message ?? data?.error ?? "请求失败，请稍后重试"
  }
  // 前端本地校验（密码复杂度等）抛出的 Error：直接展示具体原因
  if (error instanceof Error && error.message) {
    return error.message
  }
  return "请求失败，请稍后重试"
}
