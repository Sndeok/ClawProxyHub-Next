// 与后端 admin API 对齐的最小类型集（按页面取用，避免一次性搬完整个 schema）
export interface Stats {
  total_requests: number
  today_requests: number
  success_rate: number
  total_tokens: number
  active_keys: number
  active_accounts: number
  running_plugins: number
}

export interface TrendPoint {
  date: string
  requests: number
  success: number
  tokens: number
}

export interface QuotaPlugin {
  plugin: string
  accounts: number
  with_quota: number
  quota: Record<string, number>
}

export interface RequestLog {
  ID: number
  key_name?: string
  account_name?: string
  PluginID: number | null
  AccountID: number | null
  RequestedModel: string
  Model: string
  Protocol: string
  Stream: boolean
  Status: number
  Attempts: number
  ErrorType: string
  InputTokens: number
  OutputTokens: number
  CachedTokens: number
  FirstTokenMs: number
  LatencyMs: number
  ClientIP: string
  UserAgent: string
  ErrorBrief: string
  FinishReason: string
  CreatedAt: string
}

export interface LogPage {
  logs: RequestLog[]
  total: number
  page: number
  page_size: number
  has_more: boolean
}

export interface Account {
  id: number
  plugin_id: number
  display_name: string
  status: string
  group_ids: number[] | null
  last_refresh_at: string | null
  pause_reason: string
  credits?: {
    remaining?: string
    total?: string
    expiring?: number
    next_expiry?: string
    next_left?: number
  } | null
  today_tokens?: number
  today_cached?: number
  today_credits?: number
  today_credits_estimated?: boolean
}

export interface GroupInfo {
  id: number
  name: string
  plugin_id: number
  plugin: string
  plugin_label: string
  accounts: number
}

export interface RouteInfo {
  ID: number
  Name: string
  Strategy: string
  GroupsJSON: string
  TimeoutSeconds: number
  FailoverEnabled: boolean
}

export interface PluginInfo {
  id: number
  name: string
  label: string
  version: string
  author: string
  icon?: string
  capabilities: string[] | null
}

