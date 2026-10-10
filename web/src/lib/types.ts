export interface Certificate {
  id: string
  domains: string[]
  primary_domain: string
  provider: string
  status: "pending" | "valid" | "expiring" | "expired" | "revoked" | "failed"
  issuer_ca: string
  not_before: string
  not_after: string
  verification: string
  deploy_target_ids: string[]
  notify_events: string[]
  last_error: string
  created_at: string
  updated_at: string
  days_to_expire: number
}

export interface Order {
  id: string
  type: string
  status: string
  error: string
  attempts: number
  started_at: string
  finished_at: string
}

export interface AcmeAccount {
  id: string
  provider: string
  name: string
  directory_url: string
  contact: string
  eab_kid?: string
  has_eab: boolean
  has_credentials: boolean
  status: string
  last_error: string
  created_at: string
  updated_at?: string
  config?: Record<string, string>
}

export interface ContactPerson {
  id: string
  name: string
  email: string
  phone: string
  created_at: string
  updated_at: string
}

export interface DnsProviderAccount {
  id: string
  name: string
  provider: string
  has_credentials: boolean
  status: string
  last_error: string
  created_at: string
  updated_at: string
  config?: Record<string, string>
}

export interface Stats {
  total_certificates: number
  expiring_within_30d: number
  expired: number
  auto_renew_success_rate: number
  renewal_trend: { month: string; success: number; total: number }[]
  deploy_success_rate: number
  notify_success_rate: number
}

// NotificationEventItem 通知事件元数据（规则/表单/详情页共用）
export interface NotificationEventItem {
  value: string
  label: string
}

export const NOTIFICATION_EVENTS: NotificationEventItem[] = [
  { value: "cert_expiring", label: "到期提醒" },
  { value: "renew_success", label: "续期成功" },
  { value: "renew_failed", label: "续期失败" },
  { value: "issue_success", label: "签发成功" },
  { value: "issue_failed", label: "签发失败" },
  { value: "deploy_success", label: "部署成功" },
  { value: "deploy_failed", label: "部署失败" },
]
