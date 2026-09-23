'use client'

// 账号详情抽屉：基础信息 / 积分包 / 模型目录 / 任务历史 + 在线测试。
// 在线测试直调插件（绕路由与密钥），用来回答「上游到底返回了什么」。
import { useCallback, useEffect, useState } from 'react'
import { FlaskConical, RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { api } from '@/lib/api'
import type { Account, ModelInfo } from '@/lib/types'
import { fmtNum, fmtTime, modelLabel, modelTitle } from '@/lib/utils'

interface CreditPackage {
  remaining?: string | number
  left?: string | number
  total?: string | number
  expiresAt?: string
  expireTime?: string
  expiry?: string
}

interface Detail {
  id: number
  display_name?: string
  status?: string
  pause_reason?: string
  manual_pause?: boolean
  last_refresh_at?: string | null
  last_used_at?: string | null
  created_at?: string
  credits?: { packages?: CreditPackage[]; total?: string; remaining?: string; used?: string } | null
  models?: ModelInfo[] | null
  runs?: { id: number; capability?: string; status?: string; summary?: string; error_message?: string; started_at?: string }[] | null
}

function pkgLeft(p: CreditPackage): string {
  const v = p.remaining ?? p.left
  return v === undefined || v === null || v === '' ? '-' : String(v)
}

function pkgExpiry(p: CreditPackage): string {
  const at = p.expiresAt || p.expireTime || p.expiry || ''
  return at ? fmtTime(at) : '-'
}


export function AccountDetail({
  open,
  account,
  onClose,
}: {
  open: boolean
  account: Account | null
  onClose: () => void
}) {
  const [detail, setDetail] = useState<Detail | null>(null)
  const [loading, setLoading] = useState(false)
  const [endpoint, setEndpoint] = useState('chat_completions')
  const [model, setModel] = useState('')
  const [question, setQuestion] = useState('你好，请用一句话自我介绍。')
  const [testing, setTesting] = useState(false)
  const [testText, setTestText] = useState('')
  const [testLogs, setTestLogs] = useState<string[]>([])
  const [testError, setTestError] = useState('')
  const [customModel, setCustomModel] = useState(false)
  const [syncing, setSyncing] = useState(false)

  const load = useCallback(async () => {
    if (!account) return
    setLoading(true)
    setTestText('')
    setTestLogs([])
    setTestError('')
    try {
      const d = await api.get<Detail>('/admin/accounts/' + account.id + '/detail')
      let list = d.models ?? []
      if (list.length === 0) {
        // 账号快照为空时顺手拉一次上游目录，避免「在线测试」只能手打模型名
        try {
          const r = await api.get<{ models: ModelInfo[] }>('/admin/accounts/' + account.id + '/models?refresh=1')
          list = r.models ?? []
          d.models = list
        } catch {
          // 拉不到就保持空列表，仍可手输模型名
        }
      }
      setDetail(d)
      setModel((m) => m || (list[0]?.id ?? ''))
    } finally {
      setLoading(false)
    }
  }, [account])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  async function runTest() {
    if (!account) return
    setTesting(true)
    setTestText('')
    setTestLogs([])
    setTestError('')
    try {
      const r = await api.post<{ text: string; logs: string[] }>('/admin/accounts/' + account.id + '/test', {
        endpoint,
        model: model.trim(),
        question,
      })
      setTestText(r.text || '')
      setTestLogs(r.logs ?? [])
    } catch (e) {
      setTestError((e as Error).message)
    } finally {
      setTesting(false)
    }
  }

  // syncModels 手动同步账号模型目录（在线测试的下拉列表就是它）
  async function syncModels() {
    if (!account) return
    setSyncing(true)
    setTestError('')
    try {
      const r = await api.get<{ models: ModelInfo[] }>('/admin/accounts/' + account.id + '/models?refresh=1')
      const list = r.models ?? []
      setDetail((d) => (d ? { ...d, models: list } : d))
      if (list.length > 0) {
        setCustomModel(false)
        setModel((m) => (list.some((x) => x.id === m) ? m : list[0].id))
      }
    } catch (e) {
      setTestError((e as Error).message)
    } finally {
      setSyncing(false)
    }
  }

  if (!open || !account) return null
  const packages = detail?.credits?.packages ?? []
  const models = detail?.models ?? []
  const runs = (detail?.runs ?? []).slice(0, 10)

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/40" onClick={onClose} role="dialog" aria-modal="true">
      <div
        className="h-full w-[560px] max-w-full overflow-y-auto bg-background p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <h2 className="truncate text-[15px] font-semibold">{account.display_name || '#' + account.id}</h2>
            <p className="text-[12px] text-muted-foreground">账号详情与在线测试</p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
              <RefreshCw className={loading ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} /> 刷新
            </Button>
            <Button variant="ghost" size="sm" onClick={onClose}>关闭</Button>
          </div>
        </div>

        <dl className="text-[13px]">
          {([
            ['状态', detail?.status || account.status],
            ['暂停原因', detail?.pause_reason || '-'],
            ['最近刷新', detail?.last_refresh_at ? fmtTime(detail.last_refresh_at) : '-'],
            ['最近使用', detail?.last_used_at ? fmtTime(detail.last_used_at) : '-'],
            ['创建时间', detail?.created_at ? fmtTime(detail.created_at) : '-'],
            ['剩余积分', fmtNum(detail?.credits?.remaining ?? account.credits?.remaining)],
            ['积分总额', fmtNum(detail?.credits?.total ?? account.credits?.total)],
          ] as [string, string][]).map(([k, v]) => (
            <div key={k} className="flex justify-between gap-4 border-b py-2">
              <dt className="shrink-0 text-muted-foreground">{k}</dt>
              <dd className="break-all text-right">{v}</dd>
            </div>
          ))}
        </dl>

        <div className="mt-4">
          <div className="mb-1 text-[13px] font-medium">积分包（{packages.length}）</div>
          {packages.length === 0 ? (
            <p className="text-[12.5px] text-muted-foreground">没有分池信息（上游未返回套餐明细）</p>
          ) : (
            <div className="flex flex-col gap-1">
              {packages.map((p, i) => (
                <div key={i} className="flex items-center justify-between rounded-md border px-3 py-2 text-[12.5px]">
                  <span className="tnum">剩余 {pkgLeft(p)}</span>
                  <span className="text-muted-foreground">到期 {pkgExpiry(p)}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="mt-4">
          <div className="mb-1 text-[13px] font-medium">模型目录（{models.length}）</div>
          <div className="flex max-h-[160px] flex-wrap gap-1 overflow-y-auto rounded-md border p-2">
            {models.length === 0 && <span className="text-[12.5px] text-muted-foreground">未同步（到「模型中心」点同步上游目录）</span>}
            {models.map((m) => {
              const label = modelLabel(m)
              return (
                <Badge key={m.id} title={modelTitle(m)} className="gap-1">
                  {label}
                  {label !== m.id && <span className="text-[10.5px] opacity-60">{m.id}</span>}
                </Badge>
              )
            })}
          </div>
        </div>

        <div className="mt-4 rounded-md border p-3">
          <div className="mb-2 flex items-center gap-2 text-[13px] font-medium">
            <FlaskConical className="h-3.5 w-3.5" /> 在线测试（直调上游，绕路由与密钥）
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Select className="w-[170px]" value={endpoint} onChange={(e) => setEndpoint(e.target.value)}>
              <option value="chat_completions">OpenAI Chat</option>
              <option value="responses">OpenAI Responses</option>
              <option value="messages">Anthropic Messages</option>
            </Select>
            {customModel || models.length === 0 ? (
              <Input
                className="min-w-[140px] flex-1"
                placeholder="模型 id（留空用 auto）"
                value={model}
                onChange={(e) => setModel(e.target.value)}
              />
            ) : (
              <Select
                className="min-w-[170px] flex-1"
                value={model}
                onChange={(e) => {
                  if (e.target.value === '__custom__') {
                    setCustomModel(true)
                    setModel('')
                    return
                  }
                  setModel(e.target.value)
                }}
              >
                {model !== '' && !models.some((m) => m.id === model) && <option value={model}>{model}（手输）</option>}
                {models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {modelLabel(m)}
                    {modelLabel(m) !== m.id ? ` · ${m.id}` : ''}
                  </option>
                ))}
                <option value="__custom__">自定义模型名…</option>
              </Select>
            )}
            <Button variant="outline" size="sm" onClick={() => void syncModels()} disabled={syncing}>
              <RefreshCw className={syncing ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} /> 同步模型
            </Button>
            {customModel && models.length > 0 && (
              <button
                className="text-[12px] text-muted-foreground underline-offset-2 hover:underline"
                onClick={() => setCustomModel(false)}
              >
                返回列表
              </button>
            )}
          </div>
          <div className="mt-2 flex items-center gap-2">
            <Input value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="测试问题" />
            <Button onClick={runTest} disabled={testing}>
              {testing ? '测试中…' : '开始测试'}
            </Button>
          </div>
          {testError && (
            <pre className="mt-2 max-h-[200px] overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-2 text-[12px] text-[var(--destructive)]">
              {testError}
            </pre>
          )}
          {testText && (
            <pre className="mt-2 max-h-[200px] overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-2 text-[12px]">
              {testText}
            </pre>
          )}
          {testLogs.length > 0 && (
            <pre className="mt-2 max-h-[160px] overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-[11.5px] text-muted-foreground">
              {testLogs.join('\n')}
            </pre>
          )}
        </div>

        <div className="mt-4">
          <div className="mb-1 text-[13px] font-medium">最近任务（{runs.length}）</div>
          {runs.length === 0 ? (
            <p className="text-[12.5px] text-muted-foreground">还没有执行记录</p>
          ) : (
            <div className="flex flex-col gap-1">
              {runs.map((r) => (
                <div key={r.id} className="rounded-md border px-3 py-2 text-[12px]">
                  <div className="flex items-center justify-between gap-2">
                    <span>{r.capability}</span>
                    <Badge tone={r.status === 'success' ? 'success' : r.status === 'failed' ? 'danger' : 'neutral'}>
                      {r.status}
                    </Badge>
                  </div>
                  <div className="mt-0.5 text-muted-foreground">{r.error_message || r.summary || '-'}</div>
                  <div className="tnum mt-0.5 text-[11px] text-muted-foreground">{fmtTime(r.started_at)}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
