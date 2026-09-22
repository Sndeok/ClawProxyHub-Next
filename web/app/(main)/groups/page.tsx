'use client'

import { useCallback, useEffect, useState } from 'react'
import { Plus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { GroupInfo, PluginInfo } from '@/lib/types'

interface ProxyRow { ID: number; Name: string; Scheme: string; Host: string; Port: number }

export default function GroupsPage() {
  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [proxies, setProxies] = useState<ProxyRow[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ name: '', plugin_id: 0 })
  const [bindTarget, setBindTarget] = useState<GroupInfo | null>(null)
  const [bindIds, setBindIds] = useState<number[]>([])
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [g, p, px] = await Promise.all([
        api.get<{ groups: GroupInfo[] }>('/admin/groups'),
        api.get<{ plugins: PluginInfo[] }>('/admin/plugins'),
        api.get<{ proxies: ProxyRow[] }>('/admin/proxies').catch(() => ({ proxies: [] })),
      ])
      setGroups(g.groups ?? [])
      setPlugins(p.plugins ?? [])
      setProxies(px.proxies ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function create() {
    try {
      await api.post('/admin/groups', { name: form.name, plugin_id: Number(form.plugin_id) })
      setCreateOpen(false)
      setForm({ name: '', plugin_id: 0 })
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function remove(id: number) {
    await api.del(`/admin/groups/${id}`)
    await load()
  }

  async function openBind(g: GroupInfo) {
    setBindTarget(g)
    const r = await api.get<{ proxy_ids: number[] }>(`/admin/groups/${g.id}/proxies`).catch(() => ({ proxy_ids: [] }))
    setBindIds(r.proxy_ids ?? [])
  }

  async function saveBind() {
    if (!bindTarget) return
    await api.put(`/admin/groups/${bindTarget.id}/proxies`, { proxy_ids: bindIds })
    setBindTarget(null)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-3">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <Button onClick={() => setCreateOpen(true)}><Plus className="h-3.5 w-3.5" /> 新建分组</Button>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th className="hidden w-[60px] md:table-cell">ID</Th>
              <Th>名称</Th>
              <Th className="hidden md:table-cell">插件</Th>
              <Th className="text-right">账号数</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {groups.map((g) => (
              <Tr key={g.id}>
                <Td className="tnum hidden text-muted-foreground md:table-cell">{g.id}</Td>
                <Td className="font-medium">
                  {g.name}
                  <div className="mt-0.5 text-[11px] text-muted-foreground md:hidden">{g.plugin_label || g.plugin}</div>
                </Td>
                <Td className="hidden text-muted-foreground md:table-cell">{g.plugin_label || g.plugin}</Td>
                <Td className="tnum text-right">{g.accounts}</Td>
                <Td>
                  <div className="flex items-center gap-3 text-[12.5px]">
                    <button className="underline-offset-2 hover:underline" onClick={() => openBind(g)}>绑定代理</button>
                    <button className="text-[var(--destructive)] underline-offset-2 hover:underline" onClick={() => remove(g.id)}>删除</button>
                  </div>
                </Td>
              </Tr>
            ))}
            {!loading && groups.length === 0 && (
              <Tr><Td colSpan={5} className="py-10 text-center text-muted-foreground">还没有分组（网关会按分组选账号）</Td></Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <Modal
        open={createOpen}
        title="新建分组"
        onClose={() => setCreateOpen(false)}
        footer={<><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
          <Button onClick={create} disabled={!form.name || !form.plugin_id}>创建</Button></>}
      >
        <Field label="分组名称"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
        <Field label="所属插件" hint="分组只在同一插件内生效">
          <Select className="w-full" value={form.plugin_id} onChange={(e) => setForm({ ...form, plugin_id: Number(e.target.value) })}>
            <option value={0}>选择插件</option>
            {plugins.map((p) => <option key={p.id} value={p.id}>{p.label || p.name}</option>)}
          </Select>
        </Field>
      </Modal>

      <Modal
        open={!!bindTarget}
        title={`绑定出站代理 · ${bindTarget?.name ?? ''}`}
        onClose={() => setBindTarget(null)}
        footer={<><Button variant="outline" onClick={() => setBindTarget(null)}>取消</Button><Button onClick={saveBind}>保存</Button></>}
      >
        <p className="mb-3 text-[12.5px] text-muted-foreground">分组下的账号共用的出站代理（账号级绑定优先级更高）</p>
        {proxies.length === 0 && <p className="text-[12.5px] text-muted-foreground">还没有代理，先去代理页创建</p>}
        <div className="flex flex-col gap-2">
          {proxies.map((p) => (
            <label key={p.ID} className="flex items-center gap-2 text-[13px]">
              <input
                type="checkbox"
                checked={bindIds.includes(p.ID)}
                onChange={(e) => setBindIds(e.target.checked ? [...bindIds, p.ID] : bindIds.filter((x) => x !== p.ID))}
              />
              <span>{p.Name || `${p.Scheme}://${p.Host}:${p.Port}`}</span>
              <Badge>{p.Scheme}</Badge>
            </label>
          ))}
        </div>
      </Modal>
    </div>
  )
}
