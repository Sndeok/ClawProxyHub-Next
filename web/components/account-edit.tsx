'use client'

// 账号编辑弹窗：改名 / 绑分组 / 绑出站代理 / 勾选模型目录。
// 与后端约定：PUT /admin/accounts/{id}、PUT /admin/accounts/{id}/proxies、
// PUT /admin/accounts/{id}/models（模型以用户勾选为准）。
import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { api } from '@/lib/api'
import type { Account, GroupInfo, ModelInfo } from '@/lib/types'
import { modelLabel, modelTitle } from '@/lib/utils'

interface ProxyRow {
  ID: number
  Name: string
  Scheme: string
  Host: string
  Port: number
}

export function AccountEdit({
  open,
  account,
  groups,
  proxies,
  onClose,
  onSaved,
}: {
  open: boolean
  account: Account | null
  groups: GroupInfo[]
  proxies: ProxyRow[]
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [name, setName] = useState('')
  const [groupIds, setGroupIds] = useState<number[]>([])
  const [proxyIds, setProxyIds] = useState<number[]>([])
  const [models, setModels] = useState<ModelInfo[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [notice, setNotice] = useState('')

  useEffect(() => {
    if (!open || !account) return
    setName(account.display_name || '')
    setGroupIds(account.group_ids ?? [])
    setProxyIds([])
    setModels([])
    setSelected([])
    setNotice('')
    let alive = true
    void (async () => {
      const [px, md] = await Promise.all([
        api.get<{ proxy_ids: number[] }>(`/admin/accounts/${account.id}/proxies`).catch(() => ({ proxy_ids: [] })),
        api.get<{ models: ModelInfo[] | null }>(`/admin/accounts/${account.id}/models`).catch(() => ({ models: null })),
      ])
      if (!alive) return
      setProxyIds(px.proxy_ids ?? [])
      const list = md.models ?? []
      setModels(list)
      setSelected(list.map((m) => m.id))
    })()
    return () => {
      alive = false
    }
  }, [open, account])

  // 账号所属插件的分组：跨插件分组没有意义，后端也会拒
  const pluginGroups = groups.filter((g) => g.plugin_id === account?.plugin_id)

  async function syncModels() {
    if (!account) return
    setSyncing(true)
    setNotice('')
    try {
      const r = await api.get<{ models: ModelInfo[] | null }>(`/admin/accounts/${account.id}/models?refresh=1`)
      const list = r.models ?? []
      setModels(list)
      setSelected(list.map((m) => m.id))
      setNotice(list.length ? `已同步 ${list.length} 个模型` : '上游没有返回模型')
    } catch (e) {
      setNotice(`同步失败：${(e as Error).message}`)
    } finally {
      setSyncing(false)
    }
  }

  async function save() {
    if (!account) return
    setBusy(true)
    setNotice('')
    try {
      await api.put(`/admin/accounts/${account.id}`, { display_name: name.trim(), group_ids: groupIds })
      await api.put(`/admin/accounts/${account.id}/proxies`, { proxy_ids: proxyIds })
      // 以勾选为准：未勾选的模型从目录里移除（路由同步只看勾选结果）
      await api.put(`/admin/accounts/${account.id}/models`, { models: models.filter((m) => selected.includes(m.id)) })
      await onSaved()
      onClose()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      title={`编辑账号${account ? ` · ${account.display_name || `#${account.id}`}` : ''}`}
      onClose={onClose}
      width={640}
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <Field label="账号名称" hint="留空 = 保持上游返回的昵称">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：主力号" />
      </Field>

      <Field label="分组" hint="同一插件的账号池；路由按分组选号">
        <div className="flex flex-wrap gap-1">
          {pluginGroups.length === 0 && <span className="text-[12.5px] text-muted-foreground">该插件还没有分组</span>}
          {pluginGroups.map((g) => {
            const on = groupIds.includes(g.id)
            return (
              <button
                key={g.id}
                type="button"
                className={
                  'rounded border px-2 py-1 text-[12px] transition-colors ' +
                  (on ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')
                }
                onClick={() => setGroupIds((v) => (on ? v.filter((x) => x !== g.id) : [...v, g.id]))}
              >
                {g.name}
              </button>
            )
          })}
        </div>
      </Field>

      <Field label="出站代理" hint="账号级代理优先级高于分组代理；不选则跟随分组 / 直连">
        <div className="flex flex-wrap gap-1">
          {proxies.length === 0 && <span className="text-[12.5px] text-muted-foreground">还没有代理，先去代理页创建</span>}
          {proxies.map((p) => {
            const on = proxyIds.includes(p.ID)
            return (
              <button
                key={p.ID}
                type="button"
                className={
                  'rounded border px-2 py-1 text-[12px] transition-colors ' +
                  (on ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')
                }
                onClick={() => setProxyIds((v) => (on ? v.filter((x) => x !== p.ID) : [...v, p.ID]))}
              >
                {p.Name || `${p.Scheme}://${p.Host}:${p.Port}`}
              </button>
            )
          })}
        </div>
      </Field>

      <Field label="模型目录" hint="未勾选的模型不会被路由同步采用">
        <div className="mb-2 flex items-center gap-3">
          <Button variant="outline" size="sm" onClick={() => void syncModels()} disabled={syncing}>
            <RefreshCw className={syncing ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} /> 同步上游
          </Button>
          <button
            type="button"
            className="text-[12px] text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => setSelected(selected.length === models.length ? [] : models.map((m) => m.id))}
          >
            {selected.length === models.length && models.length > 0 ? '全部取消' : '全选'}
          </button>
          <span className="text-[12px] text-muted-foreground">
            已选 {selected.length}/{models.length}
          </span>
        </div>
        {models.length === 0 ? (
          <span className="text-[12.5px] text-muted-foreground">还没有模型目录，点「同步上游」拉一次</span>
        ) : (
          <div className="flex max-h-[220px] flex-wrap gap-1 overflow-y-auto rounded-md border p-2">
            {models.map((m) => {
              const on = selected.includes(m.id)
              return (
                <button
                  key={m.id}
                  type="button"
                  className={
                    'rounded border px-2 py-1 text-[12px] transition-colors ' +
                    (on ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')
                  }
                  title={modelTitle(m)}
                  onClick={() => setSelected((v) => (on ? v.filter((x) => x !== m.id) : [...v, m.id]))}
                >
                  <span>{modelLabel(m)}</span>
                  {modelLabel(m) !== m.id && <span className="ml-1 text-[10.5px] opacity-60">{m.id}</span>}
                </button>
              )
            })}
          </div>
        )}
      </Field>

      {notice && <p className="text-[12.5px] text-muted-foreground">{notice}</p>}
      <p className="text-[11.5px] text-muted-foreground">
        <Badge>提示</Badge> 名称留空时不会覆盖原昵称；模型与分组保存后立即生效。
      </p>
    </Modal>
  )
}
