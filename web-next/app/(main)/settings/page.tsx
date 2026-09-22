'use client'

import { useCallback, useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input, Select } from '@/components/ui/input'
import { Field } from '@/components/ui/modal'
import { api } from '@/lib/api'
import type { PluginInfo } from '@/lib/types'

interface Settings {
  first_event_timeout: number
  log_retention_days: number
  github_proxy: string
  marketplace_url: string
  market_proxy: string
  outbound_user_agent?: string
  outbound_client_name?: string
  outbound_client_version?: string
  outbound_cli_version?: string
  sticky_ttl?: string
  sticky_cleanup_period?: string
  [k: string]: unknown
}

interface SchemaProp { title?: string; description?: string; default?: string }

export default function SettingsPage() {
  const [s, setS] = useState<Settings | null>(null)
  const [notice, setNotice] = useState('')
  const [saving, setSaving] = useState('')
  const [pw, setPw] = useState({ p1: '', p2: '' })
  // 出站标识：按插件配置
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [pluginName, setPluginName] = useState('')
  const [outSchema, setOutSchema] = useState<Record<string, SchemaProp>>({})
  const [outValues, setOutValues] = useState<Record<string, string>>({})
  const OUT_KEYS = ['user_agent', 'client_name', 'client_version', 'cli_version'] as const

  const load = useCallback(async () => {
    const r = await api.get<{ settings: Settings }>('/admin/settings')
    setS(r.settings)
    const p = await api.get<{ plugins: PluginInfo[] }>('/admin/plugins').catch(() => ({ plugins: [] }))
    setPlugins(p.plugins ?? [])
    if (!pluginName && p.plugins?.length) setPluginName(p.plugins[0].name)
  }, [pluginName])

  useEffect(() => { void load() }, [load])

  const loadPluginSettings = useCallback(async (name: string) => {
    if (!name) return
    const r = await api.get<{ schema: { properties?: Record<string, SchemaProp> } | null; values: Record<string, string> }>(
      `/admin/plugins/${name}/settings`,
    )
    const props = r.schema?.properties ?? {}
    setOutSchema(props)
    const next: Record<string, string> = {}
    for (const k of OUT_KEYS) next[k] = r.values?.[k] ?? ''
    setOutValues(next)
  }, [])

  useEffect(() => { void loadPluginSettings(pluginName) }, [pluginName, loadPluginSettings])

  async function save(part: string, patch: Record<string, unknown>) {
    setSaving(part)
    setNotice('')
    try {
      const merged = { ...(s ?? {}), ...patch }
      await api.put('/admin/settings', merged)
      setNotice('已保存，即时生效')
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  async function savePluginIdentity() {
    if (!pluginName) return
    setSaving('outbound')
    try {
      const cur = await api.get<{ values: Record<string, string> }>(`/admin/plugins/${pluginName}/settings`)
      const merged = { ...(cur.values ?? {}) }
      for (const k of OUT_KEYS) merged[k] = outValues[k]?.trim() ?? ''
      await api.put(`/admin/plugins/${pluginName}/settings`, { values: merged })
      setNotice('插件出站标识已保存')
      await loadPluginSettings(pluginName)
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  async function changePassword() {
    if (pw.p1.length < 6) { setNotice('密码至少 6 位'); return }
    if (pw.p1 !== pw.p2) { setNotice('两次输入不一致'); return }
    setSaving('pw')
    try {
      await api.post('/admin/password', { password: pw.p1 })
      setPw({ p1: '', p2: '' })
      setNotice('密码已修改')
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  if (!s) return <p className="text-[13px] text-muted-foreground">加载中…</p>

  return (
    <div className="grid gap-4 xl:grid-cols-2">
      {notice && <div className="xl:col-span-2 text-[12.5px] text-muted-foreground">{notice}</div>}

      <Card>
        <CardHeader><CardTitle>网关</CardTitle></CardHeader>
        <CardContent>
          <Field label="首字超时（秒）" hint="首字超时配置，路由级 > 全局">
            <Input type="number" value={s.first_event_timeout} onChange={(e) => setS({ ...s, first_event_timeout: Number(e.target.value) })} />
          </Field>
          <Button onClick={() => save('gw', { first_event_timeout: Number(s.first_event_timeout) })} disabled={saving === 'gw'}>
            <Save className="h-3.5 w-3.5" /> 保存
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>日志</CardTitle></CardHeader>
        <CardContent>
          <Field label="日志保留（天）" hint="0 = 永久保留；超过保留期的调用日志每 6 小时自动清理">
            <Input type="number" value={s.log_retention_days} onChange={(e) => setS({ ...s, log_retention_days: Number(e.target.value) })} />
          </Field>
          <Button onClick={() => save('logs', { log_retention_days: Number(s.log_retention_days) })} disabled={saving === 'logs'}>
            <Save className="h-3.5 w-3.5" /> 保存
          </Button>
        </CardContent>
      </Card>

      <Card className="xl:col-span-2">
        <CardHeader><CardTitle>网络</CardTitle></CardHeader>
        <CardContent>
          <Field label="插件市场地址" hint="指向插件仓库的 index.json，默认即自建插件仓库">
            <Input value={s.marketplace_url} onChange={(e) => setS({ ...s, marketplace_url: e.target.value })} placeholder="留空 = 内置默认地址" />
          </Field>
          <Field label="插件市场代理" hint="拉索引与下载插件包共用；支持 socks5://host:port、http://host:port">
            <Input value={s.market_proxy} onChange={(e) => setS({ ...s, market_proxy: e.target.value })} placeholder="socks5://127.0.0.1:1080（留空 = 直连）" />
          </Field>
          <Field label="GitHub 加速代理" hint="仅对 GitHub 域名做 URL 前缀改写（ghproxy 风格），与市场代理相互独立">
            <Input value={s.github_proxy} onChange={(e) => setS({ ...s, github_proxy: e.target.value })} placeholder="https://gh-proxy.com" />
          </Field>
          <Button onClick={() => save('net', { marketplace_url: s.marketplace_url.trim(), market_proxy: s.market_proxy.trim(), github_proxy: s.github_proxy.trim() })} disabled={saving === 'net'}>
            <Save className="h-3.5 w-3.5" /> 保存
          </Button>
        </CardContent>
      </Card>

      <Card className="xl:col-span-2">
        <CardHeader>
          <CardTitle>出站标识</CardTitle>
          <span className="text-[11.5px] text-muted-foreground">每个插件单独配置；留空 = 用插件内置默认（灰字即默认值）</span>
        </CardHeader>
        <CardContent>
          {plugins.length === 0 ? (
            <p className="text-[12.5px] text-muted-foreground">还没有运行中的插件，先去插件页安装</p>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap gap-2">
                {plugins.map((p) => (
                  <button
                    key={p.name}
                    onClick={() => setPluginName(p.name)}
                    className={pluginName === p.name
                      ? 'rounded-md bg-primary px-3 py-1 text-[12.5px] font-medium text-primary-foreground'
                      : 'rounded-md border px-3 py-1 text-[12.5px] text-muted-foreground'}
                  >
                    {p.label || p.name}
                  </button>
                ))}
              </div>
              <Field label="出站 User-Agent" hint="整段 UA；留空则用下面的客户端名称/版本 + CLI 版本拼装">
                <Input value={outValues.user_agent ?? ''} placeholder={outSchema.user_agent?.default || '内置默认'} onChange={(e) => setOutValues({ ...outValues, user_agent: e.target.value })} />
              </Field>
              <div className="grid gap-3 md:grid-cols-3">
                <Field label="客户端名称" hint="X-Product / X-IDE-Name / X-IDE-Type 取值">
                  <Input value={outValues.client_name ?? ''} placeholder={outSchema.client_name?.default || '内置默认'} onChange={(e) => setOutValues({ ...outValues, client_name: e.target.value })} />
                </Field>
                <Field label="客户端版本" hint="UA 的 <名称>/<版本>，也用于 X-IDE-Version">
                  <Input value={outValues.client_version ?? ''} placeholder={outSchema.client_version?.default || '内置默认'} onChange={(e) => setOutValues({ ...outValues, client_version: e.target.value })} />
                </Field>
                <Field label="CLI 版本" hint="UA 里 CLI/<版本> 这段">
                  <Input value={outValues.cli_version ?? ''} placeholder={outSchema.cli_version?.default || '内置默认'} onChange={(e) => setOutValues({ ...outValues, cli_version: e.target.value })} />
                </Field>
              </div>
              <Button onClick={savePluginIdentity} disabled={saving === 'outbound'}>
                <Save className="h-3.5 w-3.5" /> 保存
              </Button>
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>会话粘性</CardTitle></CardHeader>
        <CardContent>
          <p className="mb-3 text-[12px] text-muted-foreground">
            同一会话的连续请求尽量路由到同一账号，多轮对话更连贯、上游缓存命中率更高。
          </p>
          <Field label="会话保持时长" hint="一次会话多久没活动就解除绑定（30m / 1h，范围 1m–24h）">
            <Input value={s.sticky_ttl ?? '30m'} onChange={(e) => setS({ ...s, sticky_ttl: e.target.value })} />
          </Field>
          <Field label="会话清理周期" hint="后台多久清理一次过期会话（5m / 10m，范围 30s–24h）">
            <Input value={s.sticky_cleanup_period ?? '5m'} onChange={(e) => setS({ ...s, sticky_cleanup_period: e.target.value })} />
          </Field>
          <Button onClick={() => save('sticky', { sticky_ttl: s.sticky_ttl, sticky_cleanup_period: s.sticky_cleanup_period })} disabled={saving === 'sticky'}>
            <Save className="h-3.5 w-3.5" /> 保存
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>管理员密码</CardTitle></CardHeader>
        <CardContent>
          <Field label="新密码"><Input type="password" value={pw.p1} onChange={(e) => setPw({ ...pw, p1: e.target.value })} placeholder="至少 6 位" /></Field>
          <Field label="确认新密码"><Input type="password" value={pw.p2} onChange={(e) => setPw({ ...pw, p2: e.target.value })} /></Field>
          <Button onClick={changePassword} disabled={saving === 'pw' || !pw.p1}>
            <Save className="h-3.5 w-3.5" /> 修改密码
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
