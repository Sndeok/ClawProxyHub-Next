'use client'

import { useEffect, useState } from 'react'
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { Badge, statusTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import type { QuotaPlugin, RequestLog, Stats, TrendPoint } from '@/lib/types'
import { fmtCompact, fmtNum, fmtTime } from '@/lib/utils'
import Link from 'next/link'

const CARDS: { key: keyof Stats; label: string }[] = [
  { key: 'today_requests', label: '今日请求' },
  { key: 'success_rate', label: '成功率' },
  { key: 'total_tokens', label: '总 Token' },
  { key: 'active_accounts', label: '活跃账号' },
  { key: 'running_plugins', label: '运行插件' },
  { key: 'active_keys', label: '有效密钥' },
]

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [quota, setQuota] = useState<QuotaPlugin[]>([])
  const [recent, setRecent] = useState<RequestLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    setLoading(true)
    Promise.all([
      api.get<Stats>('/admin/stats'),
      api.get<{ trend: TrendPoint[] }>('/admin/stats/trend?days=7'),
      api.get<{ plugins: QuotaPlugin[] }>('/admin/stats/quota'),
      api.get<{ logs: RequestLog[]; total: number }>(`/admin/logs?page=${page}&page_size=10`),
    ])
      .then(([s, t, q, l]) => {
        if (!alive) return
        setStats(s)
        setTrend(t.trend ?? [])
        setQuota(q.plugins ?? [])
        setRecent(l.logs ?? [])
        setTotal(l.total ?? 0)
      })
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [page])

  const cardValue = (key: keyof Stats) => {
    const v = stats?.[key]
    if (v === undefined || v === null) return '-'
    if (key === 'total_tokens') return fmtCompact(Number(v))
    if (key === 'success_rate') return `${v}%`
    return String(v)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-6">
        {CARDS.map((c, i) => (
          <Card key={c.key}>
            <CardContent className="pt-5">
              <div className="text-[12px] text-muted-foreground">{c.label}</div>
              <div className={i === 0 ? 'tnum mt-1 text-[22px] font-semibold' : 'tnum mt-1 text-[22px] font-semibold'}>
                {cardValue(c.key)}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader>
            <CardTitle>请求趋势（近 7 天）</CardTitle>
          </CardHeader>
          <CardContent className="h-[260px]">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={trend} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                <XAxis dataKey="date" tick={{ fontSize: 11 }} stroke="var(--muted-foreground)" />
                <YAxis tick={{ fontSize: 11 }} stroke="var(--muted-foreground)" />
                <Tooltip
                  contentStyle={{
                    background: 'var(--card)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                />
                <Line type="monotone" dataKey="requests" name="请求" stroke="var(--foreground)" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="success" name="成功" stroke="var(--success)" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>渠道积分概览</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {quota.length === 0 && <p className="text-[12.5px] text-muted-foreground">暂无账号积分数据</p>}
            {quota.map((q) => (
              <div key={q.plugin} className="rounded-md border p-3">
                <div className="flex items-center justify-between">
                  <span className="text-[13px] font-medium">{q.plugin}</span>
                  <span className="text-[11.5px] text-muted-foreground">{q.accounts} 个账号</span>
                </div>
                <div className="mt-2 grid grid-cols-3 gap-2 text-[12px]">
                  <div>
                    <div className="text-muted-foreground">已用</div>
                    <div className="tnum font-medium">{fmtNum(q.quota.used_credits)}</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground">剩余</div>
                    <div className="tnum font-medium">{fmtNum(q.quota.credits)}</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground">总</div>
                    <div className="tnum font-medium">{fmtNum(q.quota.total_credits)}</div>
                  </div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>最近请求</CardTitle>
          <Link href="/logs">
            <Button variant="outline" size="sm">查看全部日志 →</Button>
          </Link>
        </CardHeader>
        <CardContent>
          <TableShell>
            <Table>
              <thead>
                <tr>
                  <Th>时间</Th>
                  <Th>模型</Th>
                  <Th>协议</Th>
                  <Th>状态</Th>
                  <Th className="text-right">输入</Th>
                  <Th className="text-right">输出</Th>
                  <Th className="text-right">耗时</Th>
                </tr>
              </thead>
              <tbody>
                {recent.map((r) => (
                  <Tr key={r.ID}>
                    <Td className="tnum whitespace-nowrap">{fmtTime(r.CreatedAt)}</Td>
                    <Td className="max-w-[220px] truncate">{r.RequestedModel || r.Model}</Td>
                    <Td className="text-muted-foreground">{r.Protocol}</Td>
                    <Td>
                      <Badge tone={statusTone(r.Status)}>{r.Status}</Badge>
                    </Td>
                    <Td className="tnum text-right">{r.InputTokens}</Td>
                    <Td className="tnum text-right">{r.OutputTokens}</Td>
                    <Td className="tnum text-right">{r.LatencyMs}ms</Td>
                  </Tr>
                ))}
                {!loading && recent.length === 0 && (
                  <Tr>
                    <Td colSpan={7} className="py-8 text-center text-muted-foreground">暂无调用记录</Td>
                  </Tr>
                )}
              </tbody>
            </Table>
          </TableShell>
          <div className="mt-3 flex items-center justify-between">
            <span className="text-[12px] text-muted-foreground">共 {total} 条</span>
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>上一页</Button>
              <span className="tnum text-[12.5px]">第 {page} 页</span>
              <Button variant="outline" size="sm" disabled={page * 10 >= total} onClick={() => setPage((p) => p + 1)}>下一页</Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
