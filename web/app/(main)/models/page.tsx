'use client'

// 模型中心：账号目录并集（插件从上游拉取）+ 选型元数据。
// 展示：系列 / 积分倍率 / 上下文 / 最大输出 / 推理档位；支持搜索、系列与能力筛选、倍率排序。
import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input, Select } from '@/components/ui/input'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { ModelRow } from '@/lib/types'
import { fmtCompact } from '@/lib/utils'

const EFFORT_LABEL: Record<string, string> = {
  minimal: '极低',
  low: '低',
  medium: '中',
  high: '高',
  xhigh: '极高',
  max: '最大',
}

const BIG_CONTEXT = 128000

function displayName(m: ModelRow): string {
  return m.label?.zh || m.label?.en || ''
}

function multiplierText(m: ModelRow): string {
  const v = m.credits_multiplier || 0
  if (!v) return '-'
  return 'x' + (v < 1 ? v.toFixed(2) : String(v))
}

function effortsText(m: ModelRow): string {
  const list = m.reasoning_efforts ?? []
  if (list.length === 0) return '-'
  return list.map((e) => EFFORT_LABEL[e] ?? e).join(' · ')
}

function hasReasoning(m: ModelRow): boolean {
  return (m.reasoning_efforts ?? []).length > 0 || (m.tags ?? []).some((t) => t.includes('推理'))
}

function isMultiModal(m: ModelRow): boolean {
  return (m.tags ?? []).some((t) => t.includes('多模态') || t.includes('读图'))
}

export default function ModelsPage() {
  const [models, setModels] = useState<ModelRow[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [keyword, setKeyword] = useState('')
  const [series, setSeries] = useState('全部')
  const [cap, setCap] = useState<'all' | 'reasoning' | 'big' | 'multi'>('all')
  const [sortBy, setSortBy] = useState<'multiplier' | 'context' | 'name'>('multiplier')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await api.get<{ models: ModelRow[] }>('/admin/models')
      setModels(r.models ?? [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function syncAccounts() {
    setBusy(true)
    setNotice('')
    try {
      const r = await api.post<{ total: number; refreshed: number; failed: number }>('/admin/models/sync-accounts')
      setNotice(
        r.failed > 0
          ? '已同步 ' + r.refreshed + '/' + r.total + ' 个账号，' + r.failed + ' 个失败'
          : '已同步 ' + r.refreshed + ' 个账号的模型目录',
      )
      await load()
    } catch (e) {
      setNotice((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const seriesList = useMemo(() => {
    const set = new Set<string>()
    for (const m of models) if (m.series) set.add(m.series)
    return ['全部', ...Array.from(set).sort()]
  }, [models])

  const stats = useMemo(() => {
    const maxContext = models.reduce((n, m) => Math.max(n, m.context_window || 0), 0)
    return {
      total: models.length,
      reasoning: models.filter(hasReasoning).length,
      big: models.filter((m) => (m.context_window || 0) >= BIG_CONTEXT).length,
      maxContext,
    }
  }, [models])

  const rows = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    const filtered = models.filter((m) => {
      if (series !== '全部' && (m.series || '其他') !== series) return false
      if (cap === 'reasoning' && !hasReasoning(m)) return false
      if (cap === 'big' && (m.context_window || 0) < BIG_CONTEXT) return false
      if (cap === 'multi' && !isMultiModal(m)) return false
      if (!kw) return true
      return (
        m.id.toLowerCase().includes(kw) ||
        displayName(m).toLowerCase().includes(kw) ||
        (m.series || '').toLowerCase().includes(kw) ||
        (m.description || '').toLowerCase().includes(kw)
      )
    })
    const sorted = [...filtered]
    sorted.sort((a, b) => {
      if (sortBy === 'context') return (b.context_window || 0) - (a.context_window || 0)
      if (sortBy === 'name') return a.id.localeCompare(b.id)
      const av = a.credits_multiplier || 0
      const bv = b.credits_multiplier || 0
      if (av === 0 && bv === 0) return a.id.localeCompare(b.id)
      if (av === 0) return 1
      if (bv === 0) return -1
      return av - bv
    })
    return sorted
  }, [models, keyword, series, cap, sortBy])

  const capTabs: [typeof cap, string][] = [
    ['all', '全部'],
    ['reasoning', '支持推理'],
    ['big', '大上下文'],
    ['multi', '多模态'],
]

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="w-[200px]"
            placeholder="搜索模型名 / id / 系列"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
          />
          <Select className="w-[150px]" value={series} onChange={(e) => setSeries(e.target.value)}>
            {seriesList.map((s) => (
              <option key={s} value={s}>
                {s === '全部' ? '全部系列' : s}
              </option>
            ))}
          </Select>
          <Select className="w-[170px]" value={sortBy} onChange={(e) => setSortBy(e.target.value as typeof sortBy)}>
            <option value="multiplier">倍率从低到高</option>
            <option value="context">上下文从大到小</option>
            <option value="name">按名称</option>
          </Select>
        </div>
        <div className="flex items-center gap-3">
          {notice && <span className="text-[12.5px] text-muted-foreground">{notice}</span>}
          <Button variant="outline" onClick={syncAccounts} disabled={busy}>
            <RefreshCw className={busy ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
            同步上游目录
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {(
          [
            ['可用模型', String(stats.total)],
            ['支持推理', String(stats.reasoning)],
            ['大上下文（≥128K）', String(stats.big)],
            ['最大上下文', stats.maxContext ? fmtCompact(stats.maxContext) : '-'],
          ] as [string, string][]
        ).map(([k, v]) => (
          <Card key={k}>
            <CardHeader className="pb-1">
              <CardTitle className="text-[12.5px] font-normal text-muted-foreground">{k}</CardTitle>
            </CardHeader>
            <CardContent className="text-[20px] font-semibold">{v}</CardContent>
          </Card>
        ))}
      </div>

      <div className="flex flex-wrap gap-1">
        {capTabs.map(([v, label]) => (
          <button
            key={v}
            type="button"
            className={
              'rounded border px-2.5 py-1 text-[12.5px] transition-colors ' +
              (cap === v ? 'border-primary bg-primary/10 font-medium' : 'text-muted-foreground hover:bg-accent')
            }
            onClick={() => setCap(v)}
          >
            {label}
          </button>
        ))}
      </div>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th>模型</Th>
              <Th className="text-right">上下文</Th>
              <Th className="hidden text-right md:table-cell">最大输出</Th>
              <Th className="hidden md:table-cell">推理档位</Th>
              <Th className="hidden md:table-cell">系列</Th>
              <Th className="text-right">倍率</Th>
              <Th className="hidden md:table-cell">能力</Th>
              <Th className="hidden text-right md:table-cell">账号</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((m) => (
              <Tr key={m.id}>
                <Td className="font-medium">
                  <div>{m.id}</div>
                  {displayName(m) && <div className="text-[11.5px] text-muted-foreground">{displayName(m)}</div>}
                  <div className="mt-0.5 flex flex-wrap gap-1 md:hidden">
                    {m.series && <Badge>{m.series}</Badge>}
                    <span className="text-[11px] text-muted-foreground">{effortsText(m)}</span>
                  </div>
                </Td>
                <Td className="tnum text-right">{m.context_window ? fmtCompact(m.context_window) : '-'}</Td>
                <Td className="tnum hidden text-right md:table-cell">
                  {m.max_output_tokens ? fmtCompact(m.max_output_tokens) : '-'}
                </Td>
                <Td className="hidden md:table-cell">
                  {effortsText(m)}
                  {m.default_reasoning_effort && (
                    <span className="ml-1 text-[11px] text-muted-foreground">
                      默认 {EFFORT_LABEL[m.default_reasoning_effort] ?? m.default_reasoning_effort}
                    </span>
                  )}
                </Td>
                <Td className="hidden md:table-cell">{m.series || '其他'}</Td>
                <Td className="tnum text-right">{multiplierText(m)}</Td>
                <Td className="hidden md:table-cell">
                  <div className="flex flex-wrap gap-1">
                    {(m.tags ?? []).map((t) => (
                      <Badge key={t}>{t}</Badge>
                    ))}
                  </div>
                </Td>
                <Td className="tnum hidden text-right md:table-cell">{m.accounts ?? '-'}</Td>
              </Tr>
            ))}
            {!loading && rows.length === 0 && (
              <Tr>
                <Td colSpan={8} className="py-10 text-center text-muted-foreground">
                  还没有模型：先去「账号」页同步一次上游目录，或点右上「同步上游目录」
                </Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>
    </div>
  )
}
