'use client'

import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { Account, GroupInfo, PluginInfo } from '@/lib/types'
import { fmtCompact, fmtNum } from '@/lib/utils'

export default function AccountsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [refreshing, setRefreshing] = useState(false)
  const [notice, setNotice] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [a, g, p] = await Promise.all([
        api.get<{ accounts: Account[] }>('/admin/accounts'),
        api.get<{ groups: GroupInfo[] }>('/admin/groups'),
        api.get<{ plugins: PluginInfo[] }>('/admin/plugins'),
      ])
      setAccounts(a.accounts ?? [])
      setGroups(g.groups ?? [])
      setPlugins(p.plugins ?? [])
    } finally {
      setLoading(false)
    }
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
      <div className="flex items-center justify-end gap-2">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <Button variant="outline" onClick={refreshAll} disabled={refreshing || accounts.length === 0}>
          <RefreshCw className={refreshing ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
          一键刷新
        </Button>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th>账号</Th>
              <Th>插件</Th>
              <Th>分组</Th>
              <Th className="text-right">积分</Th>
              <Th className="text-right">今日 Token</Th>
              <Th className="text-right">今日积分</Th>
              <Th>积分到期</Th>
              <Th>状态</Th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => (
              <Tr key={a.id}>
                <Td className="font-medium">{a.display_name || `#${a.id}`}</Td>
                <Td className="text-muted-foreground">{pluginLabel(a.plugin_id)}</Td>
                <Td>
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
                <Td className="tnum text-right">{a.today_tokens ? fmtCompact(a.today_tokens) : '-'}</Td>
                <Td className="tnum text-right">
                  {a.today_credits ? (
                    <span>
                      {fmtNum(a.today_credits)}
                      {a.today_credits_estimated && <span className="ml-1 text-[11px] text-muted-foreground">估算</span>}
                    </span>
                  ) : (
                    '-'
                  )}
                </Td>
                <Td className={a.credits?.expiring ? 'text-[var(--warning)]' : 'text-muted-foreground'}>
                  {expiryLabel(a)}
                </Td>
                <Td>
                  <Badge tone={a.status === 'active' ? 'success' : a.status === 'expired' ? 'danger' : 'neutral'}>
                    {a.status === 'active' ? '正常' : a.status === 'expired' ? '已过期' : a.status}
                  </Badge>
                </Td>
              </Tr>
            ))}
            {!loading && accounts.length === 0 && (
              <Tr>
                <Td colSpan={8} className="py-10 text-center text-muted-foreground">还没有账号，先去插件页安装插件</Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>
    </div>
  )
}
