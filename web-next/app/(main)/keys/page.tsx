'use client'

import { useCallback, useEffect, useState } from 'react'
import { Plus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { RouteInfo } from '@/lib/types'

interface KeyRow {
  id: number
  name: string
  enabled: boolean
  created_at: string
  last_used_at: string
  key_mask: string
  route_ids: number[] | null
}

export default function KeysPage() {
  const [keys, setKeys] = useState<KeyRow[]>([])
  const [routes, setRoutes] = useState<RouteInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [plain, setPlain] = useState('')          // 新建/显示明文
  const [plainOpen, setPlainOpen] = useState(false)
  const [editing, setEditing] = useState<KeyRow | null>(null)
  const [editName, setEditName] = useState('')
  const [bindTarget, setBindTarget] = useState<KeyRow | null>(null)
  const [bindIds, setBindIds] = useState<number[]>([])
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [k, r] = await Promise.all([
        api.get<{ keys: KeyRow[] }>('/admin/keys'),
        api.get<{ routes: RouteInfo[] }>('/admin/routes'),
      ])
      setKeys(k.keys ?? [])
      setRoutes(r.routes ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function create() {
    try {
      const r = await api.post<{ id: number; key: string }>('/admin/keys', { name })
      setCreateOpen(false)
      setName('')
      setPlain(r.key)
      setPlainOpen(true)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function reveal(row: KeyRow) {
    try {
      const r = await api.get<{ key: string }>(`/admin/keys/${row.id}/reveal`)
      setPlain(r.key)
      setPlainOpen(true)
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function toggle(row: KeyRow) {
    await api.post(`/admin/keys/${row.id}/toggle`)
    await load()
  }

  async function saveName() {
    if (!editing) return
    await api.put(`/admin/keys/${editing.id}`, { name: editName })
    setEditing(null)
    await load()
  }

  async function remove(id: number) {
    await api.del(`/admin/keys/${id}`)
    await load()
  }

  async function openBind(row: KeyRow) {
    setBindTarget(row)
    setBindIds(row.route_ids ?? [])
  }

  async function saveBind() {
    if (!bindTarget) return
    await api.put(`/admin/keys/${bindTarget.id}/routes`, { route_ids: bindIds })
    setBindTarget(null)
    await load()
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      setNotice('已复制到剪贴板')
    } catch {
      setNotice('复制失败，请手动选择复制')
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-3">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <Button onClick={() => setCreateOpen(true)}><Plus className="h-3.5 w-3.5" /> 创建密钥</Button>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th className="w-[60px]">ID</Th><Th>名称</Th><Th>密钥</Th><Th>启用</Th>
              <Th>授权路由</Th><Th>最后调用</Th><Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <Tr key={k.id}>
                <Td className="tnum text-muted-foreground">{k.id}</Td>
                <Td className="font-medium">{k.name || '-'}</Td>
                <Td className="tnum text-muted-foreground">{k.key_mask}</Td>
                <Td>
                  <button onClick={() => toggle(k)} aria-label="切换启用">
                    <Badge tone={k.enabled ? 'success' : 'neutral'}>{k.enabled ? '已启用' : '已停用'}</Badge>
                  </button>
                </Td>
                <Td className="text-muted-foreground">
                  {(k.route_ids ?? []).length === 0 ? '全部路由' : `${(k.route_ids ?? []).length} 条`}
                </Td>
                <Td className="tnum text-muted-foreground">{k.last_used_at || '从未'}</Td>
                <Td>
                  <div className="flex items-center gap-3 text-[12.5px]">
                    <button className="underline-offset-2 hover:underline" onClick={() => reveal(k)}>显示</button>
                    <button className="underline-offset-2 hover:underline" onClick={() => { setEditing(k); setEditName(k.name) }}>改名</button>
                    <button className="underline-offset-2 hover:underline" onClick={() => openBind(k)}>授权路由</button>
                    <button className="text-[var(--destructive)] underline-offset-2 hover:underline" onClick={() => remove(k.id)}>删除</button>
                  </div>
                </Td>
              </Tr>
            ))}
            {!loading && keys.length === 0 && (
              <Tr><Td colSpan={7} className="py-10 text-center text-muted-foreground">还没有调用密钥，客户端需要它才能访问 /v1</Td></Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <Modal open={createOpen} title="创建密钥" onClose={() => setCreateOpen(false)}
        footer={<><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button onClick={create}>创建</Button></>}>
        <Field label="名称" hint="仅用于识别，不影响调用"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 codex-cli" /></Field>
      </Modal>

      <Modal open={plainOpen} title="密钥明文" onClose={() => setPlainOpen(false)} width={560}
        footer={<><Button variant="outline" onClick={() => copy(plain)}>复制</Button><Button onClick={() => setPlainOpen(false)}>关闭</Button></>}>
        <p className="mb-3 text-[12.5px] text-muted-foreground">只在创建时展示一次，请立即保存到客户端配置里。</p>
        <code className="block break-all rounded-md bg-muted p-3 text-[12.5px]">{plain}</code>
      </Modal>

      <Modal open={!!editing} title="重命名密钥" onClose={() => setEditing(null)}
        footer={<><Button variant="outline" onClick={() => setEditing(null)}>取消</Button><Button onClick={saveName}>保存</Button></>}>
        <Field label="名称"><Input value={editName} onChange={(e) => setEditName(e.target.value)} /></Field>
      </Modal>

      <Modal open={!!bindTarget} title={`授权路由 · ${bindTarget?.name ?? ''}`} onClose={() => setBindTarget(null)}
        footer={<><Button variant="outline" onClick={() => setBindTarget(null)}>取消</Button><Button onClick={saveBind}>保存</Button></>}>
        <p className="mb-3 text-[12.5px] text-muted-foreground">不勾选 = 该密钥可访问全部路由</p>
        <div className="flex flex-col gap-2">
          {routes.map((r) => (
            <label key={r.ID} className="flex items-center gap-2 text-[13px]">
              <input type="checkbox" checked={bindIds.includes(r.ID)}
                onChange={(e) => setBindIds(e.target.checked ? [...bindIds, r.ID] : bindIds.filter((x) => x !== r.ID))} />
              <span>{r.Name}</span>
            </label>
          ))}
          {routes.length === 0 && <p className="text-[12.5px] text-muted-foreground">还没有路由</p>}
        </div>
      </Modal>
    </div>
  )
}
