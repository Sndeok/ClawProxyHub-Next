'use client'

// 新增账号向导：选择客户端 → 授权（按插件声明的登录方式渲染）→ 配置（名称 / 模型 / 分组）
// 后端约定：internal/admin/accounts.go::submitLogin、internal/admin/plugins.go::authMethods
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link2, Plus, RotateCw, Upload } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { api } from '@/lib/api'
import type { GroupInfo, ModelInfo, PluginInfo } from '@/lib/types'
import { modelLabel, modelTitle } from '@/lib/utils'

export interface AuthField {
  name: string
  label?: Record<string, string>
  type?: string
  required?: boolean
  placeholder?: string
}

export interface AuthMethod {
  id: string
  label?: Record<string, string>
  fields?: AuthField[]
  capabilities?: string[]
  callback?: string // auto / wait / auto_wait
}

interface NextStep {
  action: string
  url?: string
  prompt?: Record<string, string>
  fields?: AuthField[]
  state?: string
  wait?: boolean
}

interface LoginResp {
  done: boolean
  account_id?: number
  next?: NextStep | null
}

interface DetailResp {
  display_name?: string
  profile?: { displayName?: string; nickname?: string } | null
}

type Step = 'select' | 'auth' | 'done'

const CAP_LABEL: Record<string, string> = {
  account: '账号',
  login: '登录',
  chat: '对话',
  models: '模型',
  tasks: '任务',
  refresh: '刷新',
  refreshable: '可刷新',
  auto_relogin: '自动续登',
  profile: '资料',
}

function pickLabel(map?: Record<string, string>, fallback = ''): string {
  if (!map) return fallback
  return map.zh || map.en || Object.values(map)[0] || fallback
}

// callback 模式判定（渲染与提交共用：提交时不能依赖上一轮渲染的 memo）
function shouldShowCallbackInput(method: AuthMethod | undefined, step: NextStep | null): boolean {
  switch (method?.callback) {
    case 'auto':
      return false
    case 'wait':
      return true
    case 'auto_wait':
      return !isLocalHost()
    default:
      return !!step?.fields?.length
  }
}

// auto / 本机 auto_wait 才后台轮询；wait 与服务器部署的 auto_wait 等用户粘贴回调
function shouldAutoPoll(method: AuthMethod | undefined, step: NextStep | null): boolean {
  if (!step?.wait) return false
  switch (method?.callback) {
    case 'auto':
      return true
    case 'wait':
      return false
    case 'auto_wait':
      return isLocalHost()
    default:
      return true
  }
}

function isLocalHost(): boolean {
  if (typeof window === 'undefined') return false
  const h = window.location.hostname
  return h === 'localhost' || h === '127.0.0.1' || h === '::1'
}

const POLL_INTERVAL = 2500

export function AccountWizard({
  open,
  plugins,
  groups,
  onClose,
  onFinished,
  onGroupsChanged,
}: {
  open: boolean
  plugins: PluginInfo[]
  groups: GroupInfo[]
  onClose: () => void
  onFinished: () => void | Promise<void>
  onGroupsChanged: () => void | Promise<void>
}) {
  const [step, setStep] = useState<Step>('select')
  const [pluginName, setPluginName] = useState('')
  const [pluginID, setPluginID] = useState(0)
  const [methods, setMethods] = useState<AuthMethod[]>([])
  const [methodId, setMethodId] = useState('')
  const [form, setForm] = useState<Record<string, string>>({})
  const [next, setNext] = useState<NextStep | null>(null)
  const [stepForm, setStepForm] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [polling, setPolling] = useState(false)
  const [error, setError] = useState('')
  const [hint, setHint] = useState('')

  // 第三步：授权成功后的配置
  const [accountID, setAccountID] = useState(0)
  const [name, setName] = useState('')
  const [profileName, setProfileName] = useState('')
  const [selectedGroups, setSelectedGroups] = useState<number[]>([])
  const [newGroup, setNewGroup] = useState('')
  const [models, setModels] = useState<ModelInfo[]>([])
  const [modelsBusy, setModelsBusy] = useState(false)
  const [saving, setSaving] = useState(false)

  const pollRef = useRef<number | null>(null)
  const pollCtx = useRef({ plugin: '', method: '', state: '', auto: false })

  const currentMethod = useMemo(() => methods.find((m) => m.id === methodId), [methods, methodId])
  const currentFields = currentMethod?.fields ?? []
  const pluginGroups = useMemo(() => groups.filter((g) => g.plugin_id === pluginID), [groups, pluginID])

  // 渲染用：仅按当前 state 推导（提交路径另用 shouldAutoPoll，避免读到上一轮渲染的值）
  const showCallbackInput = useMemo(() => shouldShowCallbackInput(currentMethod, next), [currentMethod, next])

  const stopPolling = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearTimeout(pollRef.current)
      pollRef.current = null
    }
    pollCtx.current.auto = false
    setPolling(false)
  }, [])

  const reset = useCallback(() => {
    stopPolling()
    setStep('select')
    setPluginName('')
    setPluginID(0)
    setMethods([])
    setMethodId('')
    setForm({})
    setNext(null)
    setStepForm({})
    setError('')
    setHint('')
    setAccountID(0)
    setName('')
    setProfileName('')
    setSelectedGroups([])
    setNewGroup('')
    setModels([])
    setBusy(false)
    setSaving(false)
  }, [stopPolling])

  useEffect(() => {
    if (open) reset()
  }, [open, reset])
  useEffect(() => stopPolling, [stopPolling])

  // syncModels 拉取该账号的模型目录（refresh=1 让插件向上游同步）
  const syncModels = useCallback(async (id: number) => {
    if (!id) return
    setModelsBusy(true)
    setHint('')
    try {
      const r = await api.get<{ models: ModelInfo[] | null }>(`/admin/accounts/${id}/models?refresh=1`)
      setModels(r.models ?? [])
    } catch (e) {
      setHint(`模型同步失败：${(e as Error).message}（可稍后到账号详情重试）`)
    } finally {
      setModelsBusy(false)
    }
  }, [])

  // enterDone 授权成功：进入第三步（名称 / 模型 / 分组）
  const enterDone = useCallback(
    (id: number) => {
      stopPolling()
      setAccountID(id)
      setName('')
      setSelectedGroups([])
      setModels([])
      setError('')
      setHint('')
      setStep('done')
      api
        .get<DetailResp>(`/admin/accounts/${id}/detail`)
        .then((d) => setProfileName(d.display_name || d.profile?.displayName || d.profile?.nickname || ''))
        .catch(() => void 0)
      void syncModels(id)
    },
    [stopPolling, syncModels],
  )

  // startPolling 浏览器授权（callback=auto / 本机 auto_wait）时的后台轮询
  const startPolling = useCallback(() => {
    stopPolling()
    pollCtx.current.auto = true
    setPolling(true)
    const tick = async () => {
      if (!pollCtx.current.auto) return
      try {
        const resp = await api.post<LoginResp>('/admin/accounts/login', {
          plugin: pollCtx.current.plugin,
          method_id: pollCtx.current.method,
          form: {},
          state: pollCtx.current.state,
        })
        if (resp.done) {
          pollCtx.current.auto = false
          enterDone(resp.account_id ?? 0)
          return
        }
        if (resp.next) {
          setNext(resp.next)
          pollCtx.current.state = resp.next.state ?? ''
        }
        if (pollCtx.current.auto) pollRef.current = window.setTimeout(tick, POLL_INTERVAL)
      } catch {
        // 网络抖动 / 上游仍在等待：继续轮询
        if (pollCtx.current.auto) pollRef.current = window.setTimeout(tick, POLL_INTERVAL)
      }
    }
    pollRef.current = window.setTimeout(tick, POLL_INTERVAL)
  }, [enterDone, stopPolling])

  // 切换登录方式：清空上一步的表单与等待状态
  useEffect(() => {
    setForm({})
    setNext(null)
    setStepForm({})
    setError('')
    setHint('')
    stopPolling()
  }, [methodId, stopPolling])

  async function choosePlugin(p: PluginInfo) {
    setPluginName(p.name)
    setPluginID(p.id)
    setStep('auth')
    setError('')
    setHint('')
    try {
      const r = await api.get<{ auth_methods: AuthMethod[] }>(`/admin/plugins/${p.name}/auth-methods`)
      const list = r.auth_methods ?? []
      setMethods(list)
      setMethodId(list[0]?.id ?? '')
    } catch (e) {
      setMethods([])
      setMethodId('')
      setError((e as Error).message)
    }
  }

  async function submit() {
    if (!pluginName || !methodId) return
    setBusy(true)
    setError('')
    setHint('')
    stopPolling()
    try {
      const payload = next
        ? { plugin: pluginName, method_id: methodId, form: stepForm, state: next.state ?? '' }
        : { plugin: pluginName, method_id: methodId, form, state: '' }
      const resp = await api.post<LoginResp>('/admin/accounts/login', payload)
      if (resp.done) {
        enterDone(resp.account_id ?? 0)
        return
      }
      const step = resp.next ?? null
      setNext(step)
      setStepForm({})
      if (step?.wait) {
        // 授权链接只展示，不自动打开新窗口（用户自己点「打开授权页」）
        // 判定必须用刚拿到的 step：此时 React 还没重渲染，读取 memo 会得到上一轮的值
        if (shouldAutoPoll(currentMethod, step)) {
          pollCtx.current = { plugin: pluginName, method: methodId, state: step.state ?? '', auto: false }
          startPolling()
        }
      }
    } catch (e) {
      setError((e as Error).message)
      stopPolling()
    } finally {
      setBusy(false)
    }
  }

  async function onDrop(e: React.DragEvent, field: string) {
    e.preventDefault()
    const file = e.dataTransfer?.files?.[0]
    if (!file) return
    const text = await file.text()
    setForm((f) => ({ ...f, [field]: text }))
    setHint(`已读取文件：${file.name}`)
  }

  async function createGroup() {
    const groupName = newGroup.trim()
    if (!groupName || !pluginID) return
    try {
      await api.post('/admin/groups', { name: groupName, plugin_id: pluginID })
      setNewGroup('')
      await onGroupsChanged()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  // closeWizard 统一收尾：已建档但没点「完成」时也要刷新父级列表，避免新账号看不到
  const closeWizard = useCallback(() => {
    stopPolling()
    if (accountID) void onFinished()
    onClose()
  }, [accountID, onClose, onFinished, stopPolling])

  async function finish() {
    if (!accountID) return
    setSaving(true)
    setError('')
    try {
      const body: Record<string, unknown> = { group_ids: selectedGroups }
      if (name.trim()) body.display_name = name.trim()
      await api.put(`/admin/accounts/${accountID}`, body)
      await onFinished()
      onClose()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const submitLabel = () => {
    if (next?.wait) return showCallbackInput ? '提交回调地址' : '等待浏览器授权…'
    if (next) return '下一步'
    if (currentFields.some((f) => f.type === 'phone')) return '发送验证码'
    if (currentFields.length === 0) return '生成授权链接'
    return '授权登录'
  }

  const selectedPlugin = plugins.find((p) => p.name === pluginName)

  return (
    <Modal
      open={open}
      title={step === 'done' ? '添加账号 · 完成配置' : step === 'auth' ? '添加账号 · 授权' : '添加账号 · 选择客户端'}
      onClose={closeWizard}
      width={680}
      footer={
        step === 'auth' ? (
          <>
            <Button variant="outline" onClick={() => { stopPolling(); setStep('select'); setNext(null); setForm({}); setStepForm({}) }}>
              上一步
            </Button>
            <Button onClick={submit} disabled={busy || polling || !methodId}>
              {busy ? '提交中…' : polling ? '等待授权…' : submitLabel()}
            </Button>
          </>
        ) : step === 'done' ? (
          <>
            <Button variant="outline" onClick={closeWizard}>
              稍后配置
            </Button>
            <Button onClick={finish} disabled={saving}>
              {saving ? '保存中…' : '完成'}
            </Button>
          </>
        ) : undefined
      }
    >
      {step === 'select' && (
        <div className="flex flex-col gap-3">
          {plugins.length === 0 ? (
            <p className="rounded-md border border-dashed px-4 py-8 text-center text-[13px] text-muted-foreground">
              还没有可用插件：先到「插件」页安装并启动，再回来添加账号。
            </p>
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              {plugins.map((p) => (
                <button
                  key={p.name}
                  type="button"
                  className="flex flex-col items-start gap-2 rounded-lg border bg-card p-3 text-left transition-colors hover:border-primary hover:bg-accent"
                  onClick={() => void choosePlugin(p)}
                >
                  <div className="flex items-center gap-2">
                    <span className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-md border bg-muted text-[13px] font-semibold">
                      {p.icon ? (
                        // 插件图标由网关注册的免鉴权静态端点提供
                        <img src={p.icon} alt={p.label || p.name} className="h-full w-full object-cover" />
                      ) : (
                        (p.label || p.name).slice(0, 1)
                      )}
                    </span>
                    <span className="text-[13px] font-medium">{p.label || p.name}</span>
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {(p.capabilities ?? []).slice(0, 3).map((c) => (
                      <Badge key={c}>{CAP_LABEL[c] ?? c}</Badge>
                    ))}
                  </div>
                </button>
              ))}
            </div>
          )}
          {error && <p className="text-[12.5px] text-[var(--destructive)]">{error}</p>}
        </div>
      )}

      {step === 'auth' && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
            <span>客户端</span>
            <Badge tone="neutral">{selectedPlugin?.label || pluginName}</Badge>
          </div>

          {methods.length > 1 && (
            <div className="flex flex-wrap gap-1 rounded-md border bg-muted/40 p-1">
              {methods.map((m) => (
                <button
                  key={m.id}
                  type="button"
                  className={
                    'rounded px-3 py-1.5 text-[12.5px] transition-colors ' +
                    (m.id === methodId ? 'bg-background font-medium shadow-sm' : 'text-muted-foreground hover:bg-background/60')
                  }
                  onClick={() => setMethodId(m.id)}
                >
                  {pickLabel(m.label, m.id)}
                </button>
              ))}
            </div>
          )}

          {currentFields.length > 0 && (
            <div>
              {currentFields.map((f) =>
                f.type === 'textarea' ? (
                  <Field key={f.name} label={pickLabel(f.label, f.name)} hint="可直接粘贴内容，或把凭据文件拖到这里">
                    <div
                      onDrop={(e) => void onDrop(e, f.name)}
                      onDragOver={(e) => e.preventDefault()}
                      className="rounded-md border border-dashed p-2"
                    >
                      <textarea
                        className="min-h-[120px] w-full resize-y rounded border bg-background p-2 font-mono text-[12px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
                        placeholder={f.placeholder || '粘贴凭据文件内容'}
                        value={form[f.name] ?? ''}
                        onChange={(e) => setForm((v) => ({ ...v, [f.name]: e.target.value }))}
                      />
                      <span className="mt-1 flex items-center gap-1 text-[11.5px] text-muted-foreground">
                        <Upload className="h-3 w-3" /> 支持拖入 .json / .info 文件自动读取
                      </span>
                    </div>
                  </Field>
                ) : (
                  <Field key={f.name} label={pickLabel(f.label, f.name) + (f.required ? ' *' : '')}>
                    <Input
                      type={f.type === 'password' ? 'password' : 'text'}
                      placeholder={f.placeholder}
                      value={form[f.name] ?? ''}
                      onChange={(e) => setForm((v) => ({ ...v, [f.name]: e.target.value }))}
                    />
                  </Field>
                ),
              )}
            </div>
          )}

          {!currentFields.length && !next && <p className="text-[12.5px] text-muted-foreground">该登录方式无需填写表单，点击下方按钮生成授权链接。</p>}

          {next && (
            <div className="rounded-md border bg-muted/40 p-3 text-[12.5px]">
              <div>{pickLabel(next.prompt, next.action === 'open_url' ? '请在浏览器完成授权' : '请继续完成登录')}</div>
              {next.wait && (
                <div className="mt-1 text-muted-foreground">
                  {showCallbackInput ? '授权完成后，把浏览器地址栏的完整回调 URL 粘贴到下面提交。' : '已发起授权，正在等待上游回调…'}
                </div>
              )}
              {next.url && (
                <div className="mt-2 flex flex-col gap-1">
                  <span className="break-all font-mono text-[11.5px] text-muted-foreground">{next.url}</span>
                  <div className="flex items-center gap-3">
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-[12.5px] underline-offset-2 hover:underline"
                      onClick={() => void navigator.clipboard.writeText(next.url ?? '')}
                    >
                      复制链接
                    </button>
                    <a
                      className="inline-flex items-center gap-1 text-[12.5px] underline-offset-2 hover:underline"
                      href={next.url}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <Link2 className="h-3 w-3" /> 打开授权页
                    </a>
                  </div>
                </div>
              )}
            </div>
          )}

          {next && (next.action === 'input_form' || (next.action === 'open_url' && (!next.wait || showCallbackInput))) && (next.fields ?? []).length > 0 && (
            <div>
              {(next.fields ?? []).map((f) => (
                <Field key={f.name} label={pickLabel(f.label, f.name) + (f.required ? ' *' : '')}>
                  {f.type === 'textarea' ? (
                    <textarea
                      className="min-h-[70px] w-full resize-y rounded-md border bg-background p-2 text-[12.5px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
                      placeholder={f.placeholder}
                      value={stepForm[f.name] ?? ''}
                      onChange={(e) => setStepForm((v) => ({ ...v, [f.name]: e.target.value }))}
                    />
                  ) : (
                    <Input
                      placeholder={f.placeholder}
                      value={stepForm[f.name] ?? ''}
                      onChange={(e) => setStepForm((v) => ({ ...v, [f.name]: e.target.value }))}
                    />
                  )}
                </Field>
              ))}
            </div>
          )}

          {polling && (
            <p className="flex items-center gap-1.5 text-[12.5px] text-muted-foreground">
              <RotateCw className="h-3.5 w-3.5 animate-spin" /> 正在等待上游确认授权，完成后会自动进入下一步
            </p>
          )}
          {error && <p className="text-[12.5px] text-[var(--destructive)] whitespace-pre-wrap">{error}</p>}
          {hint && <p className="text-[12.5px] text-muted-foreground">{hint}</p>}
        </div>
      )}

      {step === 'done' && (
        <div className="flex flex-col gap-4">
          <p className="rounded-md border border-[color-mix(in_oklch,var(--success)_40%,transparent)] bg-[color-mix(in_oklch,var(--success)_12%,transparent)] px-3 py-2 text-[12.5px]">
            授权成功，账号已建档（#{accountID}）。补全下面信息后即可参与调度。
          </p>

          <Field label="账号名称" hint={profileName ? `留空则使用上游昵称：${profileName}` : '留空则使用上游返回的昵称'}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>

          <div>
            <div className="flex items-center gap-2 text-[12.5px] font-medium">
              模型目录
              <button
                type="button"
                className="inline-flex items-center gap-1 text-[12px] font-normal text-muted-foreground underline-offset-2 hover:underline"
                onClick={() => void syncModels(accountID)}
                disabled={modelsBusy}
              >
                <RotateCw className={modelsBusy ? 'h-3 w-3 animate-spin' : 'h-3 w-3'} />
                {modelsBusy ? '同步中…' : '重新同步'}
              </button>
            </div>
            <div className="mt-1.5 flex flex-wrap gap-1">
              {models.length > 0 ? (
                models.map((m) => (
                  <Badge key={m.id} title={modelTitle(m)} className="gap-1">
                    {modelLabel(m)}
                    {modelLabel(m) !== m.id && <span className="text-[10.5px] opacity-60">{m.id}</span>}
                  </Badge>
                ))
              ) : (
                <span className="text-[12.5px] text-muted-foreground">{modelsBusy ? '正在读取上游模型…' : '暂无模型（可在账号详情里重新同步）'}</span>
              )}
            </div>
          </div>

          <div>
            <div className="mb-1.5 text-[12.5px] font-medium">分组</div>
            <div className="flex flex-wrap gap-1">
              {pluginGroups.length === 0 && <span className="text-[12.5px] text-muted-foreground">该插件还没有分组</span>}
              {pluginGroups.map((g) => {
                const on = selectedGroups.includes(g.id)
                return (
                  <button
                    key={g.id}
                    type="button"
                    className={
                      'rounded border px-2 py-1 text-[12px] transition-colors ' +
                      (on ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')
                    }
                    onClick={() => setSelectedGroups((v) => (on ? v.filter((x) => x !== g.id) : [...v, g.id]))}
                  >
                    {g.name}
                  </button>
                )
              })}
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Input
                className="max-w-[220px]"
                placeholder="新建分组名称"
                value={newGroup}
                aria-label="新建分组名称"
                onChange={(e) => setNewGroup(e.target.value)}
              />
              <Button variant="outline" size="sm" onClick={() => void createGroup()} disabled={!newGroup.trim()}>
                <Plus className="h-3.5 w-3.5" /> 新建分组
              </Button>
            </div>
          </div>

          {error && <p className="text-[12.5px] text-[var(--destructive)]">{error}</p>}
          {hint && <p className="text-[12.5px] text-muted-foreground">{hint}</p>}
        </div>
      )}
    </Modal>
  )
}
