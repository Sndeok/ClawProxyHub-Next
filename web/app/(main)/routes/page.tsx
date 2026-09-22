'use client'

import { useCallback, useEffect, useState } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { RouteEdit, strategyLabel, type GroupEntry } from '@/components/route-edit'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { GroupInfo, RouteInfo } from '@/lib/types'


function parseGroups(json: string): GroupEntry[] {
  try {
    const parsed = JSON.parse(json) as GroupEntry[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export default function RoutesPage() {
  const [routes, setRoutes] = useState<RouteInfo[]>([])
  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [loading, setLoading] = useState(true)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [defaultStrategy, setDefaultStrategy] = useState('sticky_expiring')
  const [editTarget, setEditTarget] = useState<RouteInfo | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [r, g, st] = await Promise.all([
        api.get<{ routes: RouteInfo[] }>('/admin/routes'),
        api.get<{ groups: GroupInfo[] }>('/admin/groups'),
        api
          .get<{ settings?: { route_default_strategy?: string } }>('/admin/settings')
          .catch(() => ({ settings: undefined })),
      ])
      setRoutes(r.routes ?? [])
      setGroups(g.groups ?? [])
      setDefaultStrategy(st.settings?.route_default_strategy || 'sticky_expiring')
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

  function openCreate() {
    setEditTarget(null)
    setDialogOpen(true)
  }

  function openEdit(r: RouteInfo) {
    setEditTarget(r)
    setDialogOpen(true)
  }

  async function remove(r: RouteInfo) {
    if (!window.confirm(`确定删除路由「${r.Name}」？客户端再用这个名字会 404。`)) return
    setNotice('')
    try {
      await api.del(`/admin/routes/${r.ID}`)
      await load()
      setNotice(`已删除路由 ${r.Name}`)
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  const groupLabel = (e: GroupEntry) => {
    const g = groups.find((x) => x.id === e.group_id)
    return `${g?.name ?? e.group_id} → ${e.model} (${e.weight})`
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button onClick={openCreate}>
          <Plus className="h-3.5 w-3.5" /> 新建路由
        </Button>
        <div className="flex items-center gap-3">
          {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
          <Button variant="outline" onClick={syncUpstream} disabled={busy}>
            <RefreshCw className={busy ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
            同步上游模型
          </Button>
        </div>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th className="hidden w-[60px] md:table-cell">ID</Th>
              <Th>对外模型名</Th>
              <Th>策略</Th>
              <Th className="hidden md:table-cell">分组 → 真实模型</Th>
              <Th className="hidden text-right md:table-cell">超时</Th>
              <Th className="hidden md:table-cell">降级</Th>
              <Th className="text-right">操作</Th>
            </tr>
          </thead>
          <tbody>
            {routes.map((r) => (
              <Tr key={r.ID}>
                <Td className="tnum hidden text-muted-foreground md:table-cell">{r.ID}</Td>
                <Td className="font-medium">
                  {r.Name}
                  <div className="mt-1 flex flex-wrap gap-1 md:hidden">
                    {parseGroups(r.GroupsJSON).map((e, i) => (
                      <Badge key={i}>{groupLabel(e)}</Badge>
                    ))}
                  </div>
                </Td>
                <Td>
                  <Badge tone={(r.Strategy || defaultStrategy).startsWith('sticky') ? 'success' : 'neutral'}>
                      {r.Strategy ? strategyLabel(r.Strategy) : '全局 · ' + strategyLabel(defaultStrategy)}
                    </Badge>
                </Td>
                <Td className="hidden md:table-cell">
                  <div className="flex flex-wrap gap-1">
                    {parseGroups(r.GroupsJSON).map((e, i) => (
                      <Badge key={i}>{groupLabel(e)}</Badge>
                    ))}
                  </div>
                </Td>
                <Td className="tnum hidden text-right md:table-cell">
                  {r.TimeoutSeconds > 0 ? `${r.TimeoutSeconds}s` : '全局'}
                </Td>
                <Td className="hidden md:table-cell">
                  {r.FailoverEnabled ? <Badge tone="warning">已开启</Badge> : <Badge>关闭</Badge>}
                </Td>
                <Td className="whitespace-nowrap text-right">
                  <div className="flex items-center justify-end gap-3 text-[12.5px]">
                    <button className="inline-flex items-center gap-1 underline-offset-2 hover:underline" onClick={() => openEdit(r)}>
                      <Pencil className="h-3 w-3" /> 编辑
                    </button>
                    <button
                      className="inline-flex items-center gap-1 text-[var(--destructive)] underline-offset-2 hover:underline"
                      onClick={() => void remove(r)}
                    >
                      <Trash2 className="h-3 w-3" /> 删除
                    </button>
                  </div>
                </Td>
              </Tr>
            ))}
            {!loading && routes.length === 0 && (
              <Tr>
                <Td colSpan={7} className="py-10 text-center text-muted-foreground">
                  还没有路由：点「同步上游模型」自动生成，或「新建路由」手工添加
                </Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <RouteEdit
        open={dialogOpen}
        defaultStrategy={defaultStrategy}
        route={editTarget}
        groups={groups}
        onClose={() => setDialogOpen(false)}
        onSaved={load}
      />
    </div>
  )
}
