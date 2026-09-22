'use client'

import Link from 'next/link'
import { useCallback, useEffect, useState } from 'react'
import { Plus, RefreshCw } from 'lucide-react'
import { AccountDetail } from '@/components/account-detail'
import { AccountEdit } from '@/components/account-edit'
import { AccountWizard } from '@/components/account-wizard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { Account, GroupInfo, PluginInfo, ProxyRow } from '@/lib/types'
import { fmtCompact, fmtNum } from '@/lib/utils'

export default function AccountsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [refreshing, setRefreshing] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<Account | null>(null)
  const [detailTarget, setDetailTarget] = useState<Account | null>(null)
  const [proxies, setProxies] = useState<ProxyRow[]>([])
  const [notice, setNotice] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [a, g, p, px] = await Promise.all([
        api.get<{ accounts: Account[] }>('/admin/accounts'),
        api.get<{ groups: GroupInfo[] }>('/admin/groups'),
        api.get<{ plugins: PluginInfo[] }>('/admin/plugins'),
        api.get<{ proxies: ProxyRow[] }>('/admin/proxies').catch(() => ({ proxies: [] })),
      ])
      setAccounts(a.accounts ?? [])
      setGroups(g.groups ?? [])
      setPlugins(p.plugins ?? [])
      setProxies(px.proxies ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  const loadGroups = useCallback(async () => {
    const g = await api.get<{ groups: GroupInfo[] }>('/admin/groups')
    setGroups(g.groups ?? [])
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function refreshAll() {
    setRefreshing(true)
    setNotice('')
    try {
      const r = await api.post<{ total: number; refreshed: number; failed: number }>('/admin/accounts/refresh-all')
      setNotice(`已刷新 ${r.refreshed}/${r.total} 个账号${r.failed ? `，${r.failed} 个失败` : ''}`)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setRefreshing(false)
    }
  }

  async function togglePause(a: Account) {
    setNotice('')
    try {
      if (a.status === 'paused') {
        await api.post(`/admin/accounts/${a.id}/resume`)
        setNotice(`已启用 ${a.display_name || `#${a.id}`}`)
      } else {
        await api.post(`/admin/accounts/${a.id}/pause`)
        setNotice(`已停用 ${a.display_name || `#${a.id}`}（不会参与路由选号）`)
      }
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function refreshOne(a: Account) {
    setNotice('')
    try {
      await api.post(`/admin/accounts/${a.id}/refresh`)
      await load()
      setNotice(`已刷新 ${a.display_name || `#${a.id}`}`)
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function remove(a: Account) {
    const label = a.display_name || `#${a.id}`
    if (!window.confirm(`确定删除账号「${label}」？该账号的登录凭据会被移除，且无法恢复。`)) return
    try {
      await api.del(`/admin/accounts/${a.id}`)
      await load()
      setNotice(`已删除 ${label}`)
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  const pluginLabel = (id: number) => plugins.find((p) => p.id === id)?.label || `#${id}`
  const groupName = (id: number) => groups.find((g) => g.id === id)?.name || `#${id}`
  const expiryLabel = (a: Account) => {
    const c = a.credits
    if (!c?.next_expiry) return '-'
    const at = new Date(c.next_expiry.replace(' ', 'T'))
    const days = Math.ceil((at.getTime() - Date.now()) / 86400000)
    const left = c.next_left ? ` · ${Math.round(c.next_left)}` : ''
    return days <= 0 ? `已到期${left}` : `${days} 天后${left}`
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <Button onClick={() => setAddOpen(true)} disabled={plugins.length === 0}>
          <Plus className="h-3.5 w-3.5" /> 添加账号
        </Button>
        <div className="flex items-center gap-2">
          {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
          <Button variant="outline" onClick={refreshAll} disabled={refreshing || accounts.length === 0}>
            <RefreshCw className={refreshing ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
            一键刷新
          </Button>
        </div>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th>账号</Th>
              <Th className="hidden md:table-cell">插件</Th>
              <Th className="hidden md:table-cell">分组</Th>
              <Th className="text-right">积分</Th>
              <Th className="hidden text-right md:table-cell">今日 Token</Th>
              <Th className="hidden text-right md:table-cell">今日积分</Th>
              <Th className="hidden md:table-cell">积分到期</Th>
              <Th>状态</Th>
              <Th className="text-right">操作</Th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => (
              <Tr key={a.id}>
                <Td className="min-w-[120px] font-medium">
                  {a.display_name || `#${a.id}`}
                  <div className="mt-1 flex flex-wrap gap-1 md:hidden">
                    <Badge>{pluginLabel(a.plugin_id)}</Badge>
                    {(a.group_ids ?? []).map((gid) => (
                      <Badge key={gid}>{groupName(gid)}</Badge>
                    ))}
                    {!!a.today_tokens && <Badge>今日 {fmtCompact(a.today_tokens)}</Badge>}
                    {!!a.credits?.next_expiry && (
                      <Badge tone={a.credits?.expiring ? 'warning' : 'neutral'}>{expiryLabel(a)}</Badge>
                    )}
                  </div>
                </Td>
                <Td className="hidden text-muted-foreground md:table-cell">{pluginLabel(a.plugin_id)}</Td>
                <Td className="hidden md:table-cell">
                  <div className="flex flex-wrap gap-1">
                    {(a.group_ids ?? []).map((gid) => (
                      <Badge key={gid}>{groupName(gid)}</Badge>
                    ))}
                    {(a.group_ids ?? []).length === 0 && <span className="text-muted-foreground">-</span>}
                  </div>
                </Td>
                <Td className="tnum whitespace-nowrap text-right">
                  <div>剩余 {fmtNum(a.credits?.remaining)}</div>
                  <div className="text-[11.5px] text-muted-foreground">总 {fmtNum(a.credits?.total)}</div>
                </Td>
                <Td className="tnum hidden text-right md:table-cell">{a.today_tokens ? fmtCompact(a.today_tokens) : '-'}</Td>
                <Td className="tnum hidden text-right md:table-cell">
                  {a.today_credits ? (
                    <span>
                      {fmtNum(a.today_credits)}
                      {a.today_credits_estimated && <span className="ml-1 text-[11px] text-muted-foreground">估算</span>}
                    </span>
                  ) : (
                    '-'
                  )}
                </Td>
                <Td
                  className={
                    a.credits?.expiring
                      ? 'hidden text-[var(--warning)] md:table-cell'
                      : 'hidden text-muted-foreground md:table-cell'
                  }
                >
                  {expiryLabel(a)}
                </Td>
                <Td className="whitespace-nowrap">
                  <Badge tone={a.status === 'active' ? 'success' : a.status === 'expired' ? 'danger' : a.status === 'paused' ? 'warning' : 'neutral'}>
                    {a.status === 'active' ? '正常' : a.status === 'expired' ? '已过期' : a.status === 'paused' ? '已停用' : a.status}
                  </Badge>
                </Td>
                <Td className="whitespace-nowrap text-right">
                  <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1 text-[12.5px]">
                    <button className="underline-offset-2 hover:underline" onClick={() => setDetailTarget(a)}>
                      详情
                    </button>
                    <button className="underline-offset-2 hover:underline" onClick={() => setEditTarget(a)}>
                      编辑
                    </button>
                    <button className="underline-offset-2 hover:underline" onClick={() => void refreshOne(a)}>
                      刷新
                    </button>
                    <button className="underline-offset-2 hover:underline" onClick={() => void togglePause(a)}>
                      {a.status === 'paused' ? '启用' : '停用'}
                    </button>
                    <button className="text-[var(--destructive)] underline-offset-2 hover:underline" onClick={() => void remove(a)}>
                      删除
                    </button>
                  </div>
                </Td>
              </Tr>
            ))}
            {!loading && accounts.length === 0 && (
              <Tr>
                <Td colSpan={9} className="py-10 text-center text-muted-foreground">
                  {plugins.length === 0 ? (
                    <span>
                      还没有可用插件，先到{' '}
                      <Link href="/plugins" className="underline underline-offset-2">
                        插件页
                      </Link>{' '}
                      安装并启动
                    </span>
                  ) : (
                    <span>还没有账号：点左上角「添加账号」，用手机验证码 / 凭据文件 / 浏览器授权登录上游</span>
                  )}
                </Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <AccountDetail open={!!detailTarget} account={detailTarget} onClose={() => setDetailTarget(null)} />

      <AccountEdit
        open={!!editTarget}
        account={editTarget}
        groups={groups}
        proxies={proxies}
        onClose={() => setEditTarget(null)}
        onSaved={load}
      />

      <AccountWizard
        open={addOpen}
        plugins={plugins}
        groups={groups}
        onClose={() => setAddOpen(false)}
        onFinished={load}
        onGroupsChanged={loadGroups}
      />
    </div>
  )
}
