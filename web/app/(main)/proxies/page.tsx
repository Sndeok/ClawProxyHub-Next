'use client'

import { useCallback, useEffect, useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'

interface ProxyRow {
  ID: number
  Name: string
  Scheme: string
  Host: string
  Port: number
  Username: string
}

const empty = { name: '', scheme: 'socks5', host: '', port: 1080, username: '', password: '' }

export default function ProxiesPage() {
  const [rows, setRows] = useState<ProxyRow[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<ProxyRow | null>(null)
  const [form, setForm] = useState({ ...empty })
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<number | null>(null)
  const [notice, setNotice] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await api.get<{ proxies: ProxyRow[] }>('/admin/proxies')
      setRows(r.proxies ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  function openCreate() {
    setEditing(null)
    setForm({ ...empty })
    setOpen(true)
  }

  function openEdit(row: ProxyRow) {
    setEditing(row)
    setForm({ name: row.Name, scheme: row.Scheme, host: row.Host, port: row.Port, username: row.Username, password: '' })
    setOpen(true)
  }

  async function save() {
    setSaving(true)
    try {
      const body = { ...form, port: Number(form.port) }
      if (editing) await api.put(`/admin/proxies/${editing.ID}`, body)
      else await api.post('/admin/proxies', body)
      setOpen(false)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function remove(id: number) {
    await api.del(`/admin/proxies/${id}`)
    await load()
  }

  async function test(row: ProxyRow) {
    setTesting(row.ID)
    setNotice('')
    try {
      const r = await api.post<{ ok: boolean; latency_ms?: number; error?: string }>(`/admin/proxies/${row.ID}/test`)
      setNotice(r.ok ? `${row.Name || row.Host} 连通，耗时 ${r.latency_ms} ms` : `${row.Name || row.Host} 失败：${r.error}`)
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setTesting(null)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-3">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <Button variant="outline" onClick={load} disabled={loading}>
          <RefreshCw className={loading ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} /> 刷新
        </Button>
        <Button onClick={openCreate}><Plus className="h-3.5 w-3.5" /> 新建代理</Button>
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th>名称</Th><Th>地址</Th><Th>账号</Th><Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((p) => (
              <Tr key={p.ID}>
                <Td className="font-medium">
                  {p.Name || '-'}
                  <div className="tnum mt-0.5 text-[11px] text-muted-foreground md:hidden">
                    {p.Scheme}://{p.Host}:{p.Port}
                  </div>
                </Td>
                <Td className="tnum hidden md:table-cell">{p.Scheme}://{p.Host}:{p.Port}</Td>
                <Td className="text-muted-foreground">{p.Username || '-'}</Td>
                <Td>
                  <div className="flex items-center gap-3 text-[12.5px]">
                    <button className="underline-offset-2 hover:underline" onClick={() => test(p)} disabled={testing === p.ID}>
                      {testing === p.ID ? '测试中…' : '测试'}
                    </button>
                    <button className="underline-offset-2 hover:underline" onClick={() => openEdit(p)}>编辑</button>
                    <button className="text-[var(--destructive)] underline-offset-2 hover:underline" onClick={() => remove(p.ID)}>删除</button>
                  </div>
                </Td>
              </Tr>
            ))}
            {!loading && rows.length === 0 && (
              <Tr><Td colSpan={4} className="py-10 text-center text-muted-foreground">还没有代理</Td></Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <Modal
        open={open}
        title={editing ? '编辑代理' : '新建代理'}
        onClose={() => setOpen(false)}
        footer={
          <>
            <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
            <Button onClick={save} disabled={saving || !form.host}>{saving ? '保存中…' : '保存'}</Button>
          </>
        }
      >
        <Field label="名称"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="可选，便于识别" /></Field>
        <div className="grid grid-cols-3 gap-3">
          <Field label="协议">
            <Select className="w-full" value={form.scheme} onChange={(e) => setForm({ ...form, scheme: e.target.value })}>
              <option value="socks5">socks5</option>
              <option value="socks5h">socks5h</option>
              <option value="http">http</option>
              <option value="https">https</option>
            </Select>
          </Field>
          <Field label="主机"><Input value={form.host} onChange={(e) => setForm({ ...form, host: e.target.value })} placeholder="127.0.0.1" /></Field>
          <Field label="端口"><Input type="number" value={form.port} onChange={(e) => setForm({ ...form, port: Number(e.target.value) })} /></Field>
        </div>
        <Field label="用户名"><Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} /></Field>
        <Field label="密码" hint={editing ? '留空表示不修改' : undefined}>
          <Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
        </Field>
      </Modal>
    </div>
  )
}
