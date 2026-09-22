'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { Database, Download, RefreshCw, Save, Upload } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input, Select } from '@/components/ui/input'
import { Field } from '@/components/ui/modal'
import { api, getToken } from '@/lib/api'
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
  gateway_user_agent?: string
  sticky_ttl?: string
  sticky_cleanup_period?: string
  route_default_strategy?: string
  [k: string]: unknown
}

interface SchemaProp { title?: string; description?: string; default?: string }

// 系统信息（/admin/system/info）
interface SystemInfo {
  version: string
  protocol_version: number
  go_version: string
  os: string
  arch: string
  started_at: string
  uptime_seconds: number
  data_dir: string
  db_path: string
  db_size_bytes: number
  migration_version: number
  migration_dirty: boolean
  mem_alloc_bytes: number
  goroutines: number
  counts: Record<string, number>
  pending_restore: boolean
}

function formatUptime(sec: number): string {
  if (sec < 60) return `${sec} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分`
  return `${Math.floor(sec / 86400)} 天 ${Math.floor((sec % 86400) / 3600)} 小时`
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1073741824) return `${(n / 1048576).toFixed(1)} MB`
  return `${(n / 1073741824).toFixed(2)} GB`
}

// 与后端 model.ValidRouteStrategy 保持一致的取值
const ROUTE_STRATEGIES: [string, string][] = [
  ['sticky_expiring', '会话粘性 + 过期积分优先（推荐）'],
  ['sticky', '会话粘性（同一会话固定账号，缓存命中高）'],
  ['round_robin', '轮询'],
  ['random', '随机'],
  ['least_used', '最少使用'],
  ['expiring', '过期优先（先消耗快过期的积分）'],
]

export default function SettingsPage() {
  const [s, setS] = useState<Settings | null>(null)
  const [notice, setNotice] = useState('')
  const [saving, setSaving] = useState('')
  const [testingMarket, setTestingMarket] = useState(false)
  const [marketHint, setMarketHint] = useState('')
  const [version, setVersion] = useState<{ version?: string; latest?: string; update_available?: boolean; release_url?: string } | null>(null)
  const [pw, setPw] = useState({ p1: '', p2: '' })
  // 出站标识：按插件配置
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [pluginName, setPluginName] = useState('')
  const [outSchema, setOutSchema] = useState<Record<string, SchemaProp>>({})
  const [outValues, setOutValues] = useState<Record<string, string>>({})
  const OUT_KEYS = ['user_agent', 'client_name', 'client_version', 'cli_version'] as const
  // 系统信息 / 备份 / 恢复
  const [sysInfo, setSysInfo] = useState<SystemInfo | null>(null)
  const [sysBusy, setSysBusy] = useState('')
  const [sysMsg, setSysMsg] = useState('')
  const restoreRef = useRef<HTMLInputElement>(null)

  const loadSysInfo = useCallback(async () => {
    try {
      setSysInfo(await api.get<SystemInfo>('/admin/system/info'))
    } catch {
      setSysInfo(null)
    }
  }, [])

  // 下载备份：管理 API 需要 Bearer 头，不能直接用 <a href>，改用 fetch + blob
  async function downloadBackup() {
    setSysBusy('backup')
    setSysMsg('')
    try {
      const resp = await fetch('/admin/system/backup', { headers: { Authorization: `Bearer ${getToken()}` } })
      if (!resp.ok) throw new Error((await resp.text()) || `HTTP ${resp.status}`)
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')
      a.href = url
      a.download = `cph-backup-${stamp}.zip`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      setSysMsg('备份已下载（含数据库快照与凭据密钥，请妥善保管）')
    } catch (e) {
      setSysMsg((e as Error).message)
    } finally {
      setSysBusy('')
    }
  }

  async function uploadRestore(file: File) {
    if (!window.confirm('恢复会覆盖当前数据库（重启后生效，旧库另存为 .bak）。确定继续？')) return
    setSysBusy('restore')
    setSysMsg('')
    try {
      const fd = new FormData()
      fd.append('file', file)
      const resp = await fetch('/admin/system/restore', {
        method: 'POST',
        headers: { Authorization: `Bearer ${getToken()}` },
        body: fd,
      })
      const text = await resp.text()
      let payload: { error?: string; message?: string; with_key?: boolean } = {}
      try { payload = JSON.parse(text) } catch { payload = { error: text } }
      if (!resp.ok) throw new Error(payload.error || text)
      setSysMsg((payload.message || '已暂存') + (payload.with_key ? '（含凭据密钥）' : '（不含密钥：账号凭据需原密钥才能解开）'))
      await loadSysInfo()
    } catch (e) {
      setSysMsg((e as Error).message)
    } finally {
      setSysBusy('')
    }
  }

  // 市场连通性：用当前表单值测（不落库），失败信息原样展示
  async function testMarket() {
    setTestingMarket(true)
    setMarketHint('')
    try {
      const r = await api.post<{ ok: boolean; plugins?: number; latency_ms?: number; error?: string }>(
        '/admin/settings/test-market',
        { marketplace_url: (s?.marketplace_url ?? '').trim(), market_proxy: (s?.market_proxy ?? '').trim() },
      )
      if (r.ok) {
        setMarketHint('连通正常：' + (r.plugins ?? 0) + ' 个插件' + (r.latency_ms ? '，' + r.latency_ms + 'ms' : ''))
      } else {
        setMarketHint('失败：' + (r.error || '未知错误'))
      }
    } catch (e) {
      setMarketHint((e as Error).message)
    } finally {
      setTestingMarket(false)
    }
  }

  const load = useCallback(async () => {
    const r = await api.get<{ settings: Settings }>('/admin/settings')
    setS(r.settings)
    const p = await api.get<{ plugins: PluginInfo[] }>('/admin/plugins').catch(() => ({ plugins: [] }))
    setPlugins(p.plugins ?? [])
    if (!pluginName && p.plugins?.length) setPluginName(p.plugins[0].name)
  }, [pluginName])

  useEffect(() => {
    void load()
    void loadSysInfo()
    api
      .get<{ version?: string; latest?: string; update_available?: boolean; release_url?: string }>('/admin/version')
      .then(setVersion)
      .catch(() => void 0)
  }, [load, loadSysInfo])

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
          <Field
            label="全局网关 UA"
            hint="对话请求出站 UA；路由未单独配置时生效，留空 = 透传客户端自带 UA。插件按需采用（优先级：路由 UA > 全局 > 客户端）"
          >
            <Input
              value={s.gateway_user_agent ?? ''}
              placeholder="留空 = 透传客户端 UA"
              onChange={(e) => setS({ ...s, gateway_user_agent: e.target.value })}
            />
          </Field>
          <Button
            onClick={() =>
              save('gw', {
                first_event_timeout: Number(s.first_event_timeout),
                gateway_user_agent: (s.gateway_user_agent ?? '').trim(),
              })
            }
            disabled={saving === 'gw'}
          >
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
          <div className="flex flex-wrap items-center gap-3">
            <Button onClick={() => save('net', { marketplace_url: s.marketplace_url.trim(), market_proxy: s.market_proxy.trim(), github_proxy: s.github_proxy.trim() })} disabled={saving === 'net'}>
              <Save className="h-3.5 w-3.5" /> 保存
            </Button>
            <Button variant="outline" onClick={() => void testMarket()} disabled={testingMarket}>
              {testingMarket ? '测试中…' : '测试连通性'}
            </Button>
            {marketHint && <span className="text-[12px] text-muted-foreground">{marketHint}</span>}
          </div>

          <div className="mt-4 flex flex-wrap items-center gap-3 border-t pt-3 text-[12.5px] text-muted-foreground">
            <span>核心版本 v{version?.version || '-'}</span>
            {version?.update_available && (
              <a
                className="text-[var(--warning)] underline underline-offset-2"
                href={version.release_url || 'https://github.com/Sndeok/ClawProxyHub-Next/releases'}
                target="_blank"
                rel="noreferrer"
              >
                有新版本 v{version.latest}，查看更新
              </a>
            )}
          </div>
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
        <CardHeader>
          <CardTitle>负载策略</CardTitle>
          <span className="text-[11.5px] text-muted-foreground">全局默认；路由里可以单独覆盖</span>
        </CardHeader>
        <CardContent>
          <Field
            label="默认策略"
            hint="新建路由与未单独配置策略的路由都使用它。会话粘性类策略让同一会话固定账号，上游缓存命中更高"
          >
            <Select
              className="w-full"
              value={s.route_default_strategy ?? 'sticky_expiring'}
              onChange={(e) => setS({ ...s, route_default_strategy: e.target.value })}
            >
              {ROUTE_STRATEGIES.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>
          <Button
            onClick={() => save('route', { route_default_strategy: s.route_default_strategy ?? 'sticky_expiring' })}
            disabled={saving === 'route'}
          >
            <Save className="h-3.5 w-3.5" /> 保存
          </Button>
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

      <Card className="xl:col-span-2">
        <CardHeader>
          <CardTitle>系统</CardTitle>
          <span className="text-[11.5px] text-muted-foreground">运行时信息与数据备份</span>
        </CardHeader>
        <CardContent>
          {sysInfo ? (
            <div className="grid gap-1.5 text-[12.5px] text-muted-foreground md:grid-cols-2">
              <div>核心 v{sysInfo.version} · 插件契约 v{sysInfo.protocol_version}</div>
              <div>{sysInfo.os}/{sysInfo.arch} · {sysInfo.go_version} · {sysInfo.goroutines} goroutines</div>
              <div>运行时长 {formatUptime(sysInfo.uptime_seconds)} · 内存 {formatBytes(sysInfo.mem_alloc_bytes)}</div>
              <div>
                数据库 {formatBytes(sysInfo.db_size_bytes)} · 迁移 v{sysInfo.migration_version}
                {sysInfo.migration_dirty ? '（dirty，需修复）' : ''}
              </div>
              <div className="md:col-span-2">
                账号 {sysInfo.counts.accounts ?? 0} · 分组 {sysInfo.counts.groups ?? 0} · 路由 {sysInfo.counts.routes ?? 0} · 密钥{' '}
                {sysInfo.counts.keys ?? 0} · 调用日志 {sysInfo.counts.request_logs ?? 0} · 插件 {sysInfo.counts.plugins ?? 0}
              </div>
              <div className="break-all md:col-span-2">数据目录 {sysInfo.data_dir}</div>
              {sysInfo.pending_restore && (
                <div className="md:col-span-2 text-[var(--warning)]">
                  已暂存恢复包：重启服务后自动换入（当前库会另存为 .bak-时间戳）
                </div>
              )}
            </div>
          ) : (
            <p className="text-[12.5px] text-muted-foreground">系统信息不可用</p>
          )}

          <div className="mt-4 flex flex-wrap items-center gap-3 border-t pt-3">
            <Button variant="outline" onClick={() => void downloadBackup()} disabled={sysBusy === 'backup'}>
              <Download className="h-3.5 w-3.5" /> {sysBusy === 'backup' ? '打包中…' : '下载备份'}
            </Button>
            <input
              ref={restoreRef}
              type="file"
              accept=".zip"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void uploadRestore(f)
                e.target.value = ''
              }}
            />
            <Button variant="outline" onClick={() => restoreRef.current?.click()} disabled={sysBusy === 'restore'}>
              <Upload className="h-3.5 w-3.5" /> {sysBusy === 'restore' ? '上传中…' : '恢复备份'}
            </Button>
            <Button variant="ghost" onClick={() => void loadSysInfo()} disabled={!!sysBusy}>
              <RefreshCw className="h-3.5 w-3.5" /> 刷新
            </Button>
            <span className="inline-flex items-center gap-1.5 text-[12px] text-muted-foreground">
              <Database className="h-3.5 w-3.5" /> 备份含数据库快照与凭据密钥（secret.key），恢复后需重启服务生效
            </span>
          </div>
          {sysMsg && <p className="mt-2 text-[12.5px] text-muted-foreground">{sysMsg}</p>}
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
