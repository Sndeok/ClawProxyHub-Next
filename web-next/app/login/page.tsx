'use client'

import { useRouter } from 'next/navigation'
import { useState } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { setToken } from '@/lib/api'

export default function LoginPage() {
  const router = useRouter()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      const token = `${username}:${password}`
      const resp = await fetch('/admin/me', { headers: { Authorization: `Bearer ${token}` } })
      if (!resp.ok) {
        setErr(resp.status === 401 ? '用户名或密码错误' : `登录失败（HTTP ${resp.status}）`)
        return
      }
      setToken(token)
      router.replace('/dashboard')
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="grid min-h-screen place-items-center bg-[var(--sidebar)] px-4">
      <Card className="w-full max-w-[380px]">
        <CardHeader className="flex-col items-start gap-1 pt-6">
          <CardTitle className="text-[18px]">ClawProxyHub-Next</CardTitle>
          <p className="text-[12.5px] text-muted-foreground">登录管理后台</p>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="flex flex-col gap-3">
            <label className="flex flex-col gap-1.5">
              <span className="text-[12.5px] text-muted-foreground">用户名</span>
              <Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
            </label>
            <label className="flex flex-col gap-1.5">
              <span className="text-[12.5px] text-muted-foreground">管理密码</span>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                autoFocus
              />
            </label>
            {err && <p className="text-[12.5px] text-[var(--destructive)]">{err}</p>}
            <Button type="submit" disabled={loading || !password} className="mt-1 w-full">
              {loading ? '登录中…' : '登录'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
