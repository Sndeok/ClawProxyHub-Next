'use client'

// 管理 API 客户端：管理员密码换来的 Bearer token 存 localStorage，
// 401 一律清 token 回登录页（与后端 admin.auth 的行为对齐）。
const TOKEN_KEY = 'cph-admin-token'

export function getToken(): string {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setToken(t: string) {
  window.localStorage.setItem(TOKEN_KEY, t)
}

export function clearToken() {
  window.localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const resp = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (resp.status === 401) {
    clearToken()
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    throw new ApiError(401, '未授权')
  }
  if (!resp.ok) {
    const text = await resp.text()
    let msg = text
    try {
      msg = JSON.parse(text).error ?? text
    } catch {
      /* 非 JSON 错误体，原样展示 */
    }
    throw new ApiError(resp.status, msg)
  }
  return resp.json() as Promise<T>
}

// 流式请求：逐行解析 NDJSON 事件（插件安装进度流用）。
// signal 中断 = 取消安装（服务端 r.Context() 随之取消，下载立即停止）。
export interface StreamEventBase {
  phase?: string
  received?: number
  total?: number
  installed?: string
  error?: string
}

export async function requestStream<T extends StreamEventBase>(
  url: string,
  body: unknown,
  onEvent: (ev: T) => void,
  signal?: AbortSignal,
): Promise<void> {
  const resp = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
    body: JSON.stringify(body),
    signal,
  })
  if (resp.status === 401) {
    clearToken()
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    throw new ApiError(401, '未授权')
  }
  if (!resp.ok) {
    const text = await resp.text()
    let msg = text
    try {
      msg = JSON.parse(text).error ?? text
    } catch {
      /* 非 JSON 错误体 */
    }
    throw new ApiError(resp.status, msg)
  }
  if (!resp.body) throw new ApiError(500, '响应不含可读流')

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let idx = buf.indexOf('\n')
    while (idx >= 0) {
      const line = buf.slice(0, idx).trim()
      buf = buf.slice(idx + 1)
      if (line) onEvent(JSON.parse(line) as T)
      idx = buf.indexOf('\n')
    }
  }
  const tail = buf.trim()
  if (tail) onEvent(JSON.parse(tail) as T)
}

export const api = {
  get: <T,>(url: string) => request<T>('GET', url),
  post: <T,>(url: string, body?: unknown) => request<T>('POST', url, body),
  put: <T,>(url: string, body?: unknown) => request<T>('PUT', url, body),
  del: <T,>(url: string) => request<T>('DELETE', url),
}
