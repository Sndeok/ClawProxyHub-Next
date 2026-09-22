'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field } from '@/components/ui/modal'
import { api, setToken } from '@/lib/api'

export default function SetupPage() {
  const router = useRouter()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [checking, setChecking] = useState(true)

  // 已经初始化过就直接去登录页（静态托管下这里是唯一一次性的引导入口）
  useEffect(() => {
    api
      .get<{ initialized: boolean }>('/admin/setup-status')
      .then((r) => r.initialized && router.replace('/login'))
      .catch(() => undefined)
      .finally(() => setChecking(false))
  }, [router])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    if (password.length < 6) { setErr('密码至少 6 位'); return }
    if (password !== confirm) { setErr('两次输入不一致'); return }
    setLoading(true)
    try {
      await api.post('/admin/setup', { username, password })
      setToken(`${username}:${password}`)
      router.replace('/dashboard')
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  if (checking) return null

  return (
    <div className="grid min-h-screen place-items-center bg-[var(--sidebar)] px-4">
      <Card className="w-full max-w-[420px]">
        <CardHeader className="flex-col items-start gap-1 pt-6">
          <CardTitle className="text-[18px]">初始化 ClawProxyHub-Next</CardTitle>
          <p className="text-[12.5px] text-muted-foreground">首次使用，请创建管理员账号</p>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit}>
            <Field label="用户名"><Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" /></Field>
            <Field label="管理密码" hint="至少 6 位"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" /></Field>
            <Field label="确认密码"><Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" /></Field>
            {err && <p className="mb-2 text-[12.5px] text-[var(--destructive)]">{err}</p>}
            <Button type="submit" className="w-full" disabled={loading || !password}>{loading ? '创建中…' : '完成初始化'}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
