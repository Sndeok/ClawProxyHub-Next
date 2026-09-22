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

export const api = {
  get: <T,>(url: string) => request<T>('GET', url),
  post: <T,>(url: string, body?: unknown) => request<T>('POST', url, body),
  put: <T,>(url: string, body?: unknown) => request<T>('PUT', url, body),
  del: <T,>(url: string) => request<T>('DELETE', url),
}
