'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { Download, Plus, Power, Settings2, Trash2, Upload } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { api } from '@/lib/api'
import type { PluginInfo } from '@/lib/types'

interface MarketRow {
  name: string
  author: string
  version: string
  label?: Record<string, string>
  local_ver?: string
  installed?: boolean
  updatable?: boolean
  description?: Record<string, string>
}

interface SchemaProp {
  type?: string
  title?: string
  description?: string
  default?: string
  enum?: string[]
}

export default function PluginsPage() {
  const [installed, setInstalled] = useState<PluginInfo[]>([])
  const [market, setMarket] = useState<MarketRow[]>([])
  const [source, setSource] = useState('')
const [marketLoading, setMarketLoading] = useState(true)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  const [settingsFor, setSettingsFor] = useState<PluginInfo | null>(null)
  const [schema, setSchema] = useState<Record<string, SchemaProp>>({})
  const [values, setValues] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setMarketLoading(true)
      const [p, m] = await Promise.all([
        api.get<{ plugins: PluginInfo[] }>('/admin/plugins'),
        api.get<{ plugins: MarketRow[]; source: string }>('/admin/plugins/marketplace').catch(() => ({ plugins: [], source: 'error' })),
      ])
      setInstalled(p.plugins ?? [])
      setMarket(m.plugins ?? [])
      setSource(m.source)
      setMarketLoading(false)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function install(row: MarketRow) {
    setBusy(row.name)
    setNotice('')
    try {
      await api.post('/admin/plugins/install-market', { name: row.name, author: row.author })
      setNotice(`${row.name} 安装完成`)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function upload(file: File) {
    setBusy('upload')
    setNotice('')
    try {
      const fd = new FormData()
      fd.append('file', file)
      const resp = await fetch('/admin/plugins/install-upload', {
        method: 'POST',
        headers: { Authorization: `Bearer ${window.localStorage.getItem('cph-admin-token') ?? ''}` },
        body: fd,
      })
      if (!resp.ok) throw new Error(await resp.text())
      setNotice('插件包已安装')
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function togglePower(p: PluginInfo, start: boolean) {
    setBusy(p.name)
    try {
      await api.post(`/admin/plugins/${p.name}/${start ? 'start' : 'stop'}`)
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function uninstall(p: PluginInfo) {
    setBusy(p.name)
    try {
      await api.del(`/admin/plugins/${p.name}`)
      await load()
    } finally {
      setBusy('')
    }
  }

  async function openSettings(p: PluginInfo) {
    setSettingsFor(p)
    const r = await api.get<{ schema: unknown; values: Record<string, string> }>(`/admin/plugins/${p.name}/settings`)
    const props = (r.schema as { properties?: Record<string, SchemaProp> } | null)?.properties ?? {}
    setSchema(props)
    const next: Record<string, string> = {}
    for (const key of Object.keys(props)) next[key] = r.values?.[key] ?? ''
    setValues(next)
  }

  async function saveSettings() {
    if (!settingsFor) return
    setSaving(true)
    try {
      await api.put(`/admin/plugins/${settingsFor.name}/settings`, { values })
      setSettingsFor(null)
      setNotice('插件设置已保存')
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const label = (m: MarketRow) => m.label?.zh || m.label?.en || m.name

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-3">
        {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
        <input
          ref={fileRef}
          type="file"
          accept=".cphplugin,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void upload(f)
            e.target.value = ''
          }}
        />
        <Button variant="outline" onClick={() => fileRef.current?.click()} disabled={busy === 'upload'}>
          <Upload className="h-3.5 w-3.5" /> 上传插件包
        </Button>
        <Button variant="outline" onClick={load} disabled={loading}>刷新</Button>
      </div>

      <Card>
        <CardHeader><CardTitle>已安装</CardTitle></CardHeader>
        <CardContent className="flex flex-col gap-3">
          {installed.map((p) => (
            <div key={p.name} className="flex items-center justify-between gap-4 rounded-md border p-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[13.5px] font-medium">{p.label || p.name}</span>
                  <Badge>v{p.version}</Badge>
                  {(p.capabilities ?? []).slice(0, 4).map((c) => <Badge key={c}>{c}</Badge>)}
                </div>
                <div className="mt-0.5 text-[11.5px] text-muted-foreground">{p.name} · {p.author || 'unknown'}</div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => openSettings(p)} disabled={busy === p.name}>
                  <Settings2 className="h-3.5 w-3.5" /> 设置
                </Button>
                <Button variant="outline" size="sm" onClick={() => togglePower(p, false)} disabled={busy === p.name}>
                  <Power className="h-3.5 w-3.5" /> 停止
                </Button>
                <Button variant="ghost" size="sm" onClick={() => uninstall(p)} disabled={busy === p.name}>
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          ))}
          {!loading && installed.length === 0 && (
            <p className="py-6 text-center text-[12.5px] text-muted-foreground">还没有运行中的插件，从下面市场安装</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>插件市场</CardTitle>
          <span className="text-[11.5px] text-muted-foreground">{source === 'online' ? '在线索引' : '内置离线清单'}</span>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {market.map((m) => (
            <div key={`${m.author}/${m.name}`} className="flex items-center justify-between gap-4 rounded-md border p-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[13.5px] font-medium">{label(m)}</span>
                  <Badge>v{m.version}</Badge>
                  {m.installed && <Badge tone="success">已安装 {m.local_ver}</Badge>}
                  {m.updatable && <Badge tone="warning">可升级</Badge>}
                </div>
                <div className="mt-0.5 text-[11.5px] text-muted-foreground">{m.name} · {m.author}</div>
              </div>
              <Button
                variant={m.updatable ? 'default' : 'outline'}
                size="sm"
                onClick={() => install(m)}
                disabled={busy === m.name || (m.installed && !m.updatable)}
              >
                <Download className="h-3.5 w-3.5" />
                {m.updatable ? '升级' : m.installed ? '已安装' : '安装'}
              </Button>
            </div>
          ))}
          {marketLoading && <p className="py-6 text-center text-[12.5px] text-muted-foreground">正在拉取市场索引…</p>}
          {!marketLoading && market.length === 0 && (
            <p className="py-6 text-center text-[12.5px] text-muted-foreground">市场索引为空（检查设置里的市场地址与代理）</p>
          )}
        </CardContent>
      </Card>

      <Modal
        open={!!settingsFor}
        title={`插件设置 · ${settingsFor?.label || settingsFor?.name || ''}`}
        onClose={() => setSettingsFor(null)}
        width={600}
        footer={<><Button variant="outline" onClick={() => setSettingsFor(null)}>取消</Button>
          <Button onClick={saveSettings} disabled={saving}>{saving ? '保存中…' : '保存'}</Button></>}
      >
        {Object.keys(schema).length === 0 && <p className="text-[12.5px] text-muted-foreground">该插件没有声明可配置项</p>}
        {Object.entries(schema).map(([key, prop]) => (
          <Field key={key} label={prop.title || key} hint={prop.description}>
            {prop.enum?.length ? (
              <Select className="w-full" value={values[key] ?? ''} onChange={(e) => setValues({ ...values, [key]: e.target.value })}>
                <option value="">（默认）</option>
                {prop.enum.map((o) => <option key={o} value={o}>{o}</option>)}
              </Select>
            ) : (
              <Input
                value={values[key] ?? ''}
                placeholder={prop.default || '留空 = 插件内置默认'}
                onChange={(e) => setValues({ ...values, [key]: e.target.value })}
              />
            )}
          </Field>
        ))}
      </Modal>
    </div>
  )
}
