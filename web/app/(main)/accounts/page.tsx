'use client'

import Link from 'next/link'
import { useCallback, useEffect, useMemo, useState } from 'react'
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
  // 按插件筛选：默认 'all' = 按插件分组展示全部账号
  const [pluginFilter, setPluginFilter] = useState<number | 'all'>('all')

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
  const proxyLabel = (id: number) => {
    const p = proxies.find((x) => x.ID === id)
    return p ? p.Name || `${p.Scheme}://${p.Host}:${p.Port}` : `#${id}`
  }
  // 账号实际生效的出站代理：账号级绑定 > 所属分组绑定 > 直连（与网关 ProxyForAccount 同口径）
  const proxyText = (a: Account) => {
    const own = (a.proxy_ids ?? []).map(proxyLabel)
    if (own.length) return own.join(' / ')
    const inherited: string[] = []
    for (const gid of a.group_ids ?? []) {
      const g = groups.find((x) => x.id === gid)
      for (const pid of g?.proxy_ids ?? []) {
        const s = proxyLabel(pid)
        if (!inherited.includes(s)) inherited.push(s)
      }
    }
    return inherited.length ? `${inherited.join(' / ')}（继承分组）` : '直连'
  }
  const expiryLabel = (a: Account) => {
    const c = a.credits
    if (!c?.next_expiry) return '-'
    const at = new Date(c.next_expiry.replace(' ', 'T'))
    const days = Math.ceil((at.getTime() - Date.now()) / 86400000)
    const left = c.next_left ? ` · ${Math.round(c.next_left)}` : ''
    return days <= 0 ? `已到期${left}` : `${days} 天后${left}`
  }

  // 账号按插件分组：账号页默认一块一个插件，避免十几个账号混在一起找不到
  const sections = useMemo(() => {
    const labelOf = (id: number) => plugins.find((p) => p.id === id)?.label || `#${id}`
    const bucket = new Map<number, Account[]>()
    for (const a of accounts) {
      const list = bucket.get(a.plugin_id) ?? []
      list.push(a)
      bucket.set(a.plugin_id, list)
    }
    const order = plugins.map((p) => p.id).filter((id) => bucket.has(id))
    for (const id of bucket.keys()) if (!order.includes(id)) order.push(id)
    // 顺序固定：按品牌名排序（后端已排序，这里再兜一层：块与筛选都不会再乱跳）
    return order
      .map((id) => ({ id, label: labelOf(id), accounts: bucket.get(id) ?? [] }))
      .sort((a, b) => a.label.localeCompare(b.label, 'zh-Hans-CN') || a.id - b.id)
  }, [accounts, plugins])

  const visibleSections = pluginFilter === 'all' ? sections : sections.filter((s) => s.id === pluginFilter)

  // 单个插件分组的整组刷新
  async function refreshSection(list: Account[]) {
    setRefreshing(true)
    setNotice(`正在刷新 ${list.length} 个账号…`)
    let ok = 0
    let failed = 0
    for (const a of list) {
      try {
        await api.post(`/admin/accounts/${a.id}/refresh`)
        ok++
      } catch {
        failed++
      }
    }
    await load()
    setRefreshing(false)
    setNotice(`已刷新 ${ok}/${list.length} 个账号${failed ? `，${failed} 个失败` : ''}`)
  }

  const chipClass = (active: boolean) =>
    'rounded-full border px-2.5 py-1 text-[12px] transition-colors ' +
    (active ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')

  // 账号表格行（各分组共用；原表体已提到这里）
  const accountRows = (list: Account[]) => (
    <>
      {list.map((a) => (
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
                  <div className="mt-1 text-[11.5px] text-muted-foreground md:hidden">代理：{proxyText(a)}</div>
                </Td>
                <Td className="hidden md:table-cell">
                  <div className="flex flex-wrap gap-1">
                    {(a.group_ids ?? []).map((gid) => (
                      <Badge key={gid}>{groupName(gid)}</Badge>
                    ))}
                    {(a.group_ids ?? []).length === 0 && <span className="text-muted-foreground">-</span>}
                  </div>
                  <div className="mt-1 text-[11.5px] text-muted-foreground">代理：{proxyText(a)}</div>
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
    </>
  )

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

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[12.5px] text-muted-foreground">按插件</span>
        <button className={chipClass(pluginFilter === 'all')} onClick={() => setPluginFilter('all')}>
          全部 {accounts.length}
        </button>
        {sections.map((s) => (
          <button key={s.id} className={chipClass(pluginFilter === s.id)} onClick={() => setPluginFilter(s.id)}>
            {s.label} {s.accounts.length}
          </button>
        ))}
      </div>

      {visibleSections.map((s) => (
        <TableShell key={s.id}>
          <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
            <div className="flex items-center gap-2">
              <span className="text-[13px] font-medium">{s.label}</span>
              <span className="text-[12px] text-muted-foreground">{s.accounts.length} 个账号</span>
            </div>
            <button
              className="text-[12px] text-muted-foreground underline-offset-2 hover:underline disabled:opacity-50"
              onClick={() => void refreshSection(s.accounts)}
              disabled={refreshing}
            >
              刷新本组
            </button>
          </div>
          <Table>
            <thead>
              <tr>
                <Th>账号</Th>
                <Th className="hidden md:table-cell">分组</Th>
                <Th className="text-right">积分</Th>
                <Th className="hidden text-right md:table-cell">今日 Token</Th>
                <Th className="hidden text-right md:table-cell">今日积分</Th>
                <Th className="hidden md:table-cell">积分到期</Th>
                <Th>状态</Th>
                <Th className="text-right">操作</Th>
              </tr>
            </thead>
            <tbody>{accountRows(s.accounts)}</tbody>
          </Table>
        </TableShell>
      ))}

      {!loading && accounts.length === 0 && (
        <TableShell>
          <div className="py-10 text-center text-[13px] text-muted-foreground">
            {plugins.length === 0 ? (
              <span>
                还没有可用插件，先到 
                <Link href="/plugins" className="underline underline-offset-2">
                  插件页
                </Link>
                安装并启动
              </span>
            ) : (
              <span>还没有账号：点左上角「添加账号」，用手机验证码 / 凭据文件 / 浏览器授权登录上游</span>
            )}
          </div>
        </TableShell>
      )}

      <AccountDetail
        open={!!detailTarget}
        account={detailTarget}
        groups={groups}
        proxies={proxies}
        onClose={() => setDetailTarget(null)}
      />

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
