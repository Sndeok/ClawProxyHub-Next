'use client'

import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { GroupInfo, RouteInfo } from '@/lib/types'

const STRATEGY: Record<string, string> = {
  round_robin: '轮询',
  random: '随机',
  least_used: '最少使用',
  sticky: '会话粘性',
  expiring: '过期优先',
}

interface GroupEntry {
  group_id: number
  weight: number
  model: string
}

export default function RoutesPage() {
  const [routes, setRoutes] = useState<RouteInfo[]>([])
  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [r, g] = await Promise.all([
        api.get<{ routes: RouteInfo[] }>('/admin/routes'),
        api.get<{ groups: GroupInfo[] }>('/admin/groups'),
      ])
      setRoutes(r.routes ?? [])
      setGroups(g.groups ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function syncUpstream() {
    setBusy(true)
    setNotice('')
    try {
      const r = await api.post<{ models: number; created: string[]; skipped: string[]; accounts: number }>(
        '/admin/routes/sync-models',
        {},
      )
      setNotice(
        r.accounts === 0
          ? '没有可用账号（或账号未加入分组），无法同步'
          : `上游共 ${r.models} 个模型：新增 ${r.created.length} 条路由，已存在 ${r.skipped.length} 条`,
      )
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  function parseGroups(json: string): GroupEntry[] {
    try {
      return JSON.parse(json) as GroupEntry[]
    } catch {
      return []
    }
  }

  const groupLabel = (e: GroupEntry) => {
    const g = groups.find((x) => x.id === e.group_id)
    return `${g?.name ?? e.group_id} → ${e.model} (${e.weight})`
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-3">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <Button variant="outline" onClick={syncUpstream} disabled={busy}>
          <RefreshCw className={busy ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
          同步上游模型
        </Button>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th className="w-[60px]">ID</Th>
              <Th>对外模型名</Th>
              <Th>策略</Th>
              <Th>分组 → 真实模型</Th>
              <Th className="text-right">超时</Th>
              <Th>降级</Th>
            </tr>
          </thead>
          <tbody>
            {routes.map((r) => (
              <Tr key={r.ID}>
                <Td className="tnum text-muted-foreground">{r.ID}</Td>
                <Td className="font-medium">{r.Name}</Td>
                <Td>
                  <Badge tone={r.Strategy === 'sticky' ? 'success' : 'neutral'}>
                    {STRATEGY[r.Strategy] ?? r.Strategy}
                  </Badge>
                </Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {parseGroups(r.GroupsJSON).map((e, i) => (
                      <Badge key={i}>{groupLabel(e)}</Badge>
                    ))}
                  </div>
                </Td>
                <Td className="tnum text-right">{r.TimeoutSeconds > 0 ? `${r.TimeoutSeconds}s` : '全局'}</Td>
                <Td>{r.FailoverEnabled ? <Badge tone="warning">已开启</Badge> : <Badge>关闭</Badge>}</Td>
              </Tr>
            ))}
            {!loading && routes.length === 0 && (
              <Tr>
                <Td colSpan={6} className="py-10 text-center text-muted-foreground">
                  还没有路由，点「同步上游模型」一键生成
                </Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>
    </div>
  )
}
