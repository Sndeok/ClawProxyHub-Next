'use client'

import { useCallback, useEffect, useState } from 'react'
import { Play, Plus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { Account, PluginInfo } from '@/lib/types'
import { cn } from '@/lib/utils'

interface Rule {
  id: number
  plugin_id: number
  plugin: string
  capability_id: string
  capability: string
  trigger_type: string
  trigger_value: string
  target_scope: string
  accounts: string[]
  enabled: boolean
  next_run_at: string | null
  last_run_at: string | null
}

interface Run {
  id: number
  plugin: string
  capability: string
  account: string
  status: string
  summary: string
  error_message: string
  started_at: string
  finished_at: string | null
}

const TRIGGER_LABEL: Record<string, string> = { interval: '周期', daily: '每日', once: '一次', cron: 'Cron' }

export default function TasksPage() {
  const [tab, setTab] = useState<'rules' | 'runs'>('rules')
  const [rules, setRules] = useState<Rule[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [caps, setCaps] = useState<{ id: string; label: string }[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [form, setForm] = useState({
    plugin_id: 0,
    capability_id: '',
    trigger_type: 'daily',
    trigger_value: '09:00',
    target_scope: 'all',
    account_ids: [] as number[],
  })

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [r, h, p, a] = await Promise.all([
        api.get<{ rules: Rule[] }>('/admin/task-rules'),
        api.get<{ runs: Run[] }>('/admin/task-runs?limit=50'),
        api.get<{ plugins: PluginInfo[] }>('/admin/plugins'),
        api.get<{ accounts: Account[] }>('/admin/accounts'),
      ])
      setRules(r.rules ?? [])
      setRuns(h.runs ?? [])
      setPlugins(p.plugins ?? [])
      setAccounts(a.accounts ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function onPluginChange(pluginID: number) {
    setForm((f) => ({ ...f, plugin_id: pluginID, capability_id: '' }))
    const plugin = plugins.find((p) => p.id === pluginID)
    if (!plugin) return
    try {
      const r = await api.get<{ capabilities: { id: string; label: string }[] }>(
        `/admin/plugins/${plugin.name}/task-capabilities`,
      )
      setCaps(r.capabilities ?? [])
    } catch {
      setCaps([])
    }
  }

  async function create() {
    try {
      await api.post('/admin/task-rules', {
        plugin_id: Number(form.plugin_id),
        capability_id: form.capability_id,
        trigger_type: form.trigger_type,
        trigger_value: form.trigger_value,
        target_scope: form.target_scope,
        target_json: form.target_scope === 'account_ids' ? form.account_ids : [],
      })
      setCreateOpen(false)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    }
  }

  async function toggle(rule: Rule) {
    await api.post(`/admin/task-rules/${rule.id}/toggle`)
    await load()
  }

  async function runNow(rule: Rule) {
    await api.post(`/admin/task-rules/${rule.id}/run`)
    setNotice(`已触发「${rule.capability}」，结果见执行历史`)
    setTimeout(() => void load(), 2500)
  }

  async function runAll() {
    setBusy(true)
    setNotice('')
    try {
      const r = await api.post<{ triggered: number }>('/admin/task-rules/run-all')
      setNotice(`已触发 ${r.triggered} 条规则，执行结果见历史`)
      setTimeout(() => void load(), 2500)
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function removeRule(id: number) {
    await api.del(`/admin/task-rules/${id}`)
    await load()
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <div className="inline-flex rounded-md border p-0.5">
          {(['rules', 'runs'] as const).map((x) => (
            <button
              key={x}
              onClick={() => setTab(x)}
              className={cn('rounded px-3 py-1 text-[12.5px]', tab === x ? 'bg-accent font-medium' : 'text-muted-foreground')}
            >
              {x === 'rules' ? '调度规则' : '执行历史'}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-3">
          {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
          <Button variant="outline" onClick={runAll} disabled={busy || rules.length === 0}>
            <Play className={busy ? 'h-3.5 w-3.5 animate-pulse' : 'h-3.5 w-3.5'} /> 全部执行
          </Button>
          <Button onClick={() => setCreateOpen(true)}><Plus className="h-3.5 w-3.5" /> 新建规则</Button>
        </div>
      </div>

      {tab === 'rules' && (
        <TableShell>
          <Table>
            <thead>
              <tr>
                <Th className="hidden md:table-cell">插件</Th>
                <Th>任务</Th>
                <Th className="hidden md:table-cell">触发</Th>
                <Th className="hidden md:table-cell">触发值</Th>
                <Th className="hidden md:table-cell">账号范围</Th>
                <Th>状态</Th>
                <Th>操作</Th>
              </tr>
            </thead>
            <tbody>
              {rules.map((r) => (
                <Tr key={r.id}>
                  <Td className="hidden md:table-cell">{r.plugin}</Td>
                  <Td className="font-medium">
                    {r.capability}
                    <div className="mt-1 flex flex-wrap items-center gap-1 md:hidden">
                      <Badge>{TRIGGER_LABEL[r.trigger_type] ?? r.trigger_type}</Badge>
                      <span className="tnum text-[11px] text-muted-foreground">{r.trigger_value}</span>
                      <span className="text-[11px] text-muted-foreground">{r.plugin}</span>
                    </div>
                  </Td>
                  <Td className="hidden md:table-cell"><Badge>{TRIGGER_LABEL[r.trigger_type] ?? r.trigger_type}</Badge></Td>
                  <Td className="tnum hidden md:table-cell">{r.trigger_value}</Td>
                  <Td>
                    <div className="flex flex-wrap gap-1">
                      {(r.accounts ?? []).slice(0, 3).map((a) => <Badge key={a}>{a}</Badge>)}
                      {(r.accounts ?? []).length > 3 && <Badge>+{(r.accounts ?? []).length - 3}</Badge>}
                    </div>
                  </Td>
                  <Td>
                    <button onClick={() => toggle(r)} aria-label="切换启用">
                      <Badge tone={r.enabled ? 'success' : 'neutral'}>{r.enabled ? '已启用' : '已停用'}</Badge>
                    </button>
                  </Td>
                  <Td>
                    <div className="flex items-center gap-3 text-[12.5px]">
                      <button className="underline-offset-2 hover:underline" onClick={() => runNow(r)}>立即执行</button>
                      <button className="text-[var(--destructive)] underline-offset-2 hover:underline" onClick={() => removeRule(r.id)}>删除</button>
                    </div>
                  </Td>
                </Tr>
              ))}
              {!loading && rules.length === 0 && (
                <Tr><Td colSpan={7} className="py-10 text-center text-muted-foreground">还没有调度规则</Td></Tr>
              )}
            </tbody>
          </Table>
        </TableShell>
      )}

      {tab === 'runs' && (
        <TableShell>
          <Table>
            <thead>
              <tr>
                <Th>结果</Th>
                <Th>任务</Th>
                <Th className="hidden md:table-cell">账号</Th>
                <Th className="hidden md:table-cell">摘要</Th>
                <Th>开始时间</Th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <Tr key={r.id}>
                  <Td>
                    <Badge tone={r.status === 'success' ? 'success' : r.status === 'failed' ? 'danger' : 'warning'}>
                      {r.status === 'success' ? '成功' : r.status === 'failed' ? '失败' : r.status}
                    </Badge>
                  </Td>
                  <Td>{r.capability}</Td>
                  <Td className="hidden text-muted-foreground md:table-cell">{r.account}</Td>
                  <Td className="hidden max-w-[420px] truncate md:table-cell">{r.error_message || r.summary || '-'}</Td>
                  <Td className="tnum text-muted-foreground">{r.started_at?.replace('T', ' ').slice(0, 19)}</Td>
                </Tr>
              ))}
              {!loading && runs.length === 0 && (
                <Tr><Td colSpan={5} className="py-10 text-center text-muted-foreground">还没有执行记录</Td></Tr>
              )}
            </tbody>
          </Table>
        </TableShell>
      )}

      <Modal open={createOpen} title="新建调度规则" onClose={() => setCreateOpen(false)}
        footer={<><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
          <Button onClick={create} disabled={!form.plugin_id || !form.capability_id || !form.trigger_value}>创建</Button></>}>
        <Field label="插件">
          <Select className="w-full" value={form.plugin_id} onChange={(e) => onPluginChange(Number(e.target.value))}>
            <option value={0}>选择插件</option>
            {plugins.map((p) => <option key={p.id} value={p.id}>{p.label || p.name}</option>)}
          </Select>
        </Field>
        <Field label="任务能力">
          <Select className="w-full" value={form.capability_id} onChange={(e) => setForm({ ...form, capability_id: e.target.value })} disabled={!caps.length}>
            <option value="">{caps.length ? '选择能力' : '先选择插件'}</option>
            {caps.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="触发方式">
            <Select className="w-full" value={form.trigger_type} onChange={(e) => setForm({ ...form, trigger_type: e.target.value, trigger_value: e.target.value === 'daily' ? '09:00' : e.target.value === 'interval' ? '24h' : '' })}>
              <option value="daily">每日</option>
              <option value="interval">固定间隔</option>
              <option value="once">一次</option>
            </Select>
          </Field>
          <Field label="触发值" hint={form.trigger_type === 'interval' ? 'Go 时长：30m / 24h' : form.trigger_type === 'daily' ? 'HH:mm' : 'RFC3339 时间'}>
            <Input value={form.trigger_value} onChange={(e) => setForm({ ...form, trigger_value: e.target.value })} />
          </Field>
        </div>
        <Field label="账号范围">
          <Select className="w-full" value={form.target_scope} onChange={(e) => setForm({ ...form, target_scope: e.target.value })}>
            <option value="all">全部账号</option>
            <option value="account_ids">指定账号</option>
          </Select>
        </Field>
        {form.target_scope === 'account_ids' && (
          <div className="mb-3 flex max-h-[180px] flex-col gap-2 overflow-y-auto rounded-md border p-3">
            {accounts.map((a) => (
              <label key={a.id} className="flex items-center gap-2 text-[13px]">
                <input
                  type="checkbox"
                  checked={form.account_ids.includes(a.id)}
                  onChange={(e) => setForm({
                    ...form,
                    account_ids: e.target.checked ? [...form.account_ids, a.id] : form.account_ids.filter((x) => x !== a.id),
                  })}
                />
                <span>{a.display_name || `#${a.id}`}</span>
              </label>
            ))}
          </div>
        )}
      </Modal>
    </div>
  )
}
