'use client'

// 路由编辑弹窗：对外模型名 / 策略 / 分组权重与真实模型 / 超时 / 降级。
// 分组条目里的 model 是「该分组要投递的上游真实模型名」，可用已同步的模型目录做下拉。
import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Field, Modal } from '@/components/ui/modal'
import { api } from '@/lib/api'
import type { GroupInfo, RouteInfo } from '@/lib/types'

export interface GroupEntry {
  group_id: number
  weight: number
  model: string
}

export const STRATEGY_OPTIONS: [string, string][] = [
  ['sticky_expiring', '会话粘性 + 过期积分优先（推荐：会话内固定账号，新会话先烧快过期积分）'],
  ['sticky', '会话粘性（同一会话固定账号，缓存命中高）'],
  ['round_robin', '轮询'],
  ['random', '随机'],
  ['least_used', '最少使用'],
  ['expiring', '过期优先（先消耗快过期的积分）'],
]

const emptyEntry = (groupID = 0): GroupEntry => ({ group_id: groupID, weight: 100, model: '' })

export function RouteEdit({
  open,
  route,
  groups,
  onClose,
  onSaved,
}: {
  open: boolean
  route: RouteInfo | null
  groups: GroupInfo[]
  onClose: () => void
  onSaved: () => void | Promise<void>
}) {
  const [name, setName] = useState('')
  const [strategy, setStrategy] = useState('sticky_expiring')
  const [entries, setEntries] = useState<GroupEntry[]>([emptyEntry()])
  const [timeoutSec, setTimeoutSec] = useState(0)
  const [foEnabled, setFoEnabled] = useState(false)
  const [fo4xx, setFo4xx] = useState(false)
  const [fo5xx, setFo5xx] = useState(false)
  const [foGroup, setFoGroup] = useState(0)
  const [foModel, setFoModel] = useState('')
  const [models, setModels] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')

  useEffect(() => {
    if (!open) return
    setNotice('')
    api
      .get<{ models: { id: string }[] }>('/admin/models')
      .then((r) => setModels((r.models ?? []).map((m) => m.id)))
      .catch(() => setModels([]))
    if (route) {
      setName(route.Name)
      setStrategy(route.Strategy || 'sticky_expiring')
      try {
        const parsed = JSON.parse(route.GroupsJSON) as GroupEntry[]
        setEntries(parsed.length ? parsed : [emptyEntry()])
      } catch {
        setEntries([emptyEntry()])
      }
      setTimeoutSec(route.TimeoutSeconds || 0)
      setFoEnabled(!!route.FailoverEnabled)
      setFo4xx(!!route.FailoverOn4xx)
      setFo5xx(!!route.FailoverOn5xx)
      setFoGroup(route.FailoverGroupID ?? 0)
      setFoModel(route.FailoverModel || '')
    } else {
      setName('')
      setStrategy('sticky_expiring')
      setEntries([emptyEntry(groups[0]?.id ?? 0)])
      setTimeoutSec(0)
      setFoEnabled(false)
      setFo4xx(false)
      setFo5xx(false)
      setFoGroup(0)
      setFoModel('')
    }
  }, [open, route, groups])

  function patchEntry(i: number, next: Partial<GroupEntry>) {
    setEntries((v) => v.map((e, idx) => (idx === i ? { ...e, ...next } : e)))
  }

  async function save() {
    const groupsOut = entries.filter((e) => e.group_id > 0)
    if (!name.trim() || groupsOut.length === 0) {
      setNotice('需要填对外模型名，并至少配置一个分组')
      return
    }
    if (foEnabled && !fo4xx && !fo5xx) {
      setNotice('开启降级需至少勾选 4xx 或 5xx')
      return
    }
    if (foEnabled && (!foGroup || !foModel.trim())) {
      setNotice('开启降级需配置降级分组与降级模型')
      return
    }
    setBusy(true)
    setNotice('')
    const body = {
      name: name.trim(),
      strategy,
      groups: groupsOut,
      timeout_seconds: Number(timeoutSec) || 0,
      failover_enabled: foEnabled,
      failover_on_4xx: fo4xx,
      failover_on_5xx: fo5xx,
      failover_group_id: foEnabled ? foGroup : null,
      failover_model: foEnabled ? foModel.trim() : '',
    }
    try {
      if (route) await api.put(`/admin/routes/${route.ID}`, body)
      else await api.post('/admin/routes', body)
      await onSaved()
      onClose()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const groupName = (id: number) => groups.find((g) => g.id === id)?.name || `#${id}`

  return (
    <Modal
      open={open}
      title={route ? `编辑路由 · ${route.Name}` : '新建路由'}
      onClose={onClose}
      width={720}
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={busy || groups.length === 0}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      {groups.length === 0 && (
        <p className="mb-3 rounded-md border border-dashed px-3 py-2 text-[12.5px] text-muted-foreground">
          还没有分组：路由必须挂到分组上（分组 = 同插件账号池）。请先到「分组」页创建。
        </p>
      )}

      <Field label="对外模型名" hint="客户端请求的 model 名（路由名唯一）">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：glm-5.3" />
      </Field>

      <Field label="负载策略">
        <Select className="w-full" value={strategy} onChange={(e) => setStrategy(e.target.value)}>
          {STRATEGY_OPTIONS.map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </Select>
      </Field>

      <Field label="分组 → 真实模型" hint="每行一个分组；权重仅在多分组时生效">
        <div className="flex flex-col gap-2">
          {entries.map((e, i) => (
            <div key={i} className="flex flex-wrap items-center gap-2">
              <Select
                className="min-w-[130px] flex-1"
                value={e.group_id}
                onChange={(ev) => patchEntry(i, { group_id: Number(ev.target.value) })}
              >
                <option value={0}>选择分组</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}（{g.plugin_label || g.plugin}）
                  </option>
                ))}
              </Select>
              <Input
                className="min-w-[140px] flex-1"
                list="cph-model-options"
                value={e.model}
                placeholder="上游真实模型名"
                onChange={(ev) => patchEntry(i, { model: ev.target.value })}
              />
              <Input
                className="w-[90px]"
                type="number"
                min={1}
                max={100}
                value={e.weight}
                onChange={(ev) => patchEntry(i, { weight: Number(ev.target.value) || 1 })}
              />
              <Button
                variant="ghost"
                size="icon"
                aria-label="删除该分组"
                onClick={() => setEntries((v) => (v.length > 1 ? v.filter((_, idx) => idx !== i) : v))}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
          <button
            type="button"
            className="inline-flex w-fit items-center gap-1 text-[12.5px] underline-offset-2 hover:underline"
            onClick={() => setEntries((v) => [...v, emptyEntry(groups[0]?.id ?? 0)])}
          >
            <Plus className="h-3.5 w-3.5" /> 添加分组
          </button>
        </div>
      </Field>
      <datalist id="cph-model-options">
        {models.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>

      <Field label="首字超时（秒）" hint="0 = 跟随全局设置">
        <Input
          className="w-[140px]"
          type="number"
          min={0}
          max={3600}
          value={timeoutSec}
          onChange={(e) => setTimeoutSec(Number(e.target.value) || 0)}
        />
      </Field>

      <Field label="失败降级" hint="主分组失败且状态码匹配时，切到降级分组的指定模型（每次请求至多降级一次）">
        <div className="flex flex-col gap-2">
          <label className="flex items-center gap-2 text-[13px]">
            <input type="checkbox" checked={foEnabled} onChange={(e) => setFoEnabled(e.target.checked)} />
            开启降级
          </label>
          {foEnabled && (
            <>
              <div className="flex items-center gap-4 text-[12.5px]">
                <label className="flex items-center gap-1.5">
                  <input type="checkbox" checked={fo4xx} onChange={(e) => setFo4xx(e.target.checked)} /> 4xx
                </label>
                <label className="flex items-center gap-1.5">
                  <input type="checkbox" checked={fo5xx} onChange={(e) => setFo5xx(e.target.checked)} /> 5xx
                </label>
              </div>
              <Select className="w-full" value={foGroup} onChange={(e) => setFoGroup(Number(e.target.value))}>
                <option value={0}>降级分组</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}（{g.plugin_label || g.plugin}）
                  </option>
                ))}
              </Select>
              <Input
                list="cph-model-options"
                value={foModel}
                placeholder="降级后使用的上游模型名"
                onChange={(e) => setFoModel(e.target.value)}
              />
            </>
          )}
        </div>
      </Field>

      {entries[0]?.group_id ? null : <span className="text-[11.5px] text-muted-foreground">当前分组：{groupName(entries[0]?.group_id ?? 0)}</span>}
      {notice && <p className="mt-2 text-[12.5px] text-[var(--destructive)]">{notice}</p>}
    </Modal>
  )
}
