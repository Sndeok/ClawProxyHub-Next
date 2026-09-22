'use client'

import { useCallback, useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Copy, RefreshCw, Trash2 } from 'lucide-react'
import { Field, Modal } from '@/components/ui/modal'
import { Badge, statusTone } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input, Select } from '@/components/ui/input'
import { Table, TableShell, Td, Th, Tr } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { LogPage, RequestLog } from '@/lib/types'
import { fmtClock, fmtCompact, fmtMs, fmtTime, hitRate } from '@/lib/utils'

const PAGE_SIZE = 20

// LogBlock 详情里的大段文本块：标题 + 说明 + 复制 + 等宽滚动区；空文本不渲染。
function LogBlock({
  title,
  hint,
  text,
  onCopy,
}: {
  title: string
  hint: string
  text: string
  onCopy: (text: string, what: string) => void
}) {
  if (!text) return null
  return (
    <div className="mt-4">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-[13px] font-medium">{title}</span>
        <button
          type="button"
          className="inline-flex items-center gap-1 text-[12px] text-muted-foreground underline-offset-2 hover:underline"
          onClick={() => void onCopy(text, title)}
        >
          <Copy className="h-3 w-3" /> 复制
        </button>
      </div>
      <p className="mb-1 text-[11.5px] text-muted-foreground">{hint}</p>
      <pre className="max-h-[320px] overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-[12px]">
        {text}
      </pre>
    </div>
  )
}

export default function LogsPage() {
  const [page, setPage] = useState(1)
  const [data, setData] = useState<LogPage | null>(null)
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState('')
  const [protocol, setProtocol] = useState('')
  const [model, setModel] = useState('')
  const [keyword, setKeyword] = useState('')
  const [detail, setDetail] = useState<RequestLog | null>(null)
  const [detailRaw, setDetailRaw] = useState<{ request_body?: string; error_detail?: string }>({})
  const [copyHint, setCopyHint] = useState('')
  const [exporting, setExporting] = useState(false)
  const [exportHint, setExportHint] = useState('')
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [cleaning, setCleaning] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    const q = new URLSearchParams({ page: String(page), page_size: String(PAGE_SIZE) })
    if (status) q.set('status', status)
    if (protocol) q.set('protocol', protocol)
    if (model) q.set('model', model)
    if (keyword) q.set('q', keyword)
    try {
      setData(await api.get<LogPage>(`/admin/logs?${q.toString()}`))
    } finally {
      setLoading(false)
    }
  }, [page, status, protocol, model, keyword])

  useEffect(() => {
    void load()
  }, [load])

  // 请求原文与完整上游返回体积大，只在打开详情时按需拉取
  const openDetail = useCallback(async (r: RequestLog) => {
    setDetail(r)
    setDetailRaw({})
    try {
      const d = await api.get<{ request_body?: string; error_detail?: string }>('/admin/logs/' + r.ID + '/detail')
      setDetailRaw({ request_body: d.request_body ?? '', error_detail: d.error_detail ?? '' })
    } catch {
      // 详情拉取失败不影响基础字段展示
    }
  }, [])

  async function copyText(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text)
      setCopyHint(what + '已复制')
    } catch {
      setCopyHint('复制失败，请手动选择文本')
    }
    setTimeout(() => setCopyHint(''), 1800)
  }

  // 导出 CSV：带上当前筛选条件（服务端最多 5 万行，带 BOM 便于 Excel 打开）
  async function exportCSV() {
    setExporting(true)
    setExportHint('')
    try {
      const q = new URLSearchParams()
      if (status) q.set('status', status)
      if (protocol) q.set('protocol', protocol)
      if (model) q.set('model', model)
      if (keyword) q.set('q', keyword)
      const resp = await fetch('/admin/logs/export?' + q.toString(), {
        headers: { Authorization: 'Bearer ' + (window.localStorage.getItem('cph-admin-token') ?? '') },
      })
      if (!resp.ok) throw new Error(await resp.text())
      const blob = await resp.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'cph-logs.csv'
      a.click()
      URL.revokeObjectURL(url)
    } catch (e) {
      setExportHint((e as Error).message)
    } finally {
      setExporting(false)
    }
  }

  // 手动清理：days>0 清理 N 天前，all=true 清空（服务端只删 request_logs）
  async function cleanup(payload: { days?: number; all?: boolean }) {
    if (payload.all && !window.confirm('确定清空全部调用日志？该操作不可恢复。')) return
    setCleaning(true)
    setExportHint('')
    try {
      const r = await api.post<{ deleted: number }>('/admin/logs/cleanup', payload)
      setCleanupOpen(false)
      setPage(1)
      await load()
      setExportHint('已清理 ' + r.deleted + ' 条日志')
    } catch (e) {
      setExportHint((e as Error).message)
    } finally {
      setCleaning(false)
    }
  }

  const logs = data?.logs ?? []
  const total = data?.total ?? 0
  // 本页汇总：Σ Token（输入+输出）与缓存命中率（命中是输入的子集）
  const pageTokens = logs.reduce((n, r) => n + (r.InputTokens || 0) + (r.OutputTokens || 0), 0)
  const pageInput = logs.reduce((n, r) => n + (r.InputTokens || 0), 0)
  const pageCached = logs.reduce((n, r) => n + (r.CachedTokens || 0), 0)
  const pageCacheWrite = logs.reduce((n, r) => n + (r.CacheCreationTokens || 0), 0)

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardContent className="flex flex-wrap items-center gap-2 pt-5">
          <Select value={status} onChange={(e) => { setStatus(e.target.value); setPage(1) }}>
            <option value="">全部状态</option>
            <option value="ok">成功</option>
            <option value="error">失败</option>
            <option value="4xx">4xx</option>
            <option value="5xx">5xx</option>
          </Select>
          <Select value={protocol} onChange={(e) => { setProtocol(e.target.value); setPage(1) }}>
            <option value="">全部协议</option>
            <option value="chat_completions">OpenAI Chat</option>
            <option value="responses">OpenAI Responses</option>
            <option value="messages">Anthropic Messages</option>
          </Select>
          <Input
            className="w-[180px]"
            placeholder="模型名（模糊）"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && (setPage(1), void load())}
          />
          <Input
            className="w-[200px]"
            placeholder="错误 / IP / UA"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && (setPage(1), void load())}
          />
          <Button variant="outline" size="sm" onClick={() => { setPage(1); void load() }}>
            <RefreshCw className="h-3.5 w-3.5" /> 查询
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => { setStatus(''); setProtocol(''); setModel(''); setKeyword(''); setPage(1) }}
          >
            重置
          </Button>
          <Button variant="outline" size="sm" onClick={() => void exportCSV()} disabled={exporting}>
            {exporting ? '导出中…' : '导出 CSV'}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setCleanupOpen(true)}>
            <Trash2 className="h-3.5 w-3.5" /> 清理
          </Button>
          {exportHint && <span className="text-[12px] text-[var(--destructive)]">{exportHint}</span>}
          <div className="ml-auto flex items-center gap-3 text-[12px] text-muted-foreground">
            <span className="tnum">共 {total} 条</span>
            <span className="tnum">Σ {fmtCompact(pageTokens)}</span>
            <span className="tnum">缓存命中率 {hitRate(pageCached, pageInput)}</span>
            {pageCacheWrite > 0 && <span className="tnum">缓存写入 {fmtCompact(pageCacheWrite)}</span>}
          </div>
        </CardContent>
      </Card>

      <TableShell>
        <Table>
          <thead>
            <tr>
              <Th>时间</Th>
              <Th>状态</Th>
              <Th className="hidden md:table-cell">密钥</Th>
              <Th className="hidden md:table-cell">账号</Th>
              <Th>模型</Th>
              <Th className="hidden md:table-cell">协议</Th>
              <Th className="text-right">Token</Th>
              <Th className="hidden text-right md:table-cell">延迟</Th>
              <Th className="hidden md:table-cell">IP</Th>
            </tr>
          </thead>
          <tbody>
            {logs.map((r) => (
              <Tr key={r.ID} className="cursor-pointer" onClick={() => void openDetail(r)}>
                <Td className="tnum whitespace-nowrap">
                  <span className="hidden md:inline">{fmtTime(r.CreatedAt)}</span>
                  <span className="md:hidden">{fmtClock(r.CreatedAt)}</span>
                </Td>
                <Td>
                  <Badge tone={statusTone(r.Status)}>{r.Status}</Badge>
                  <div className="tnum mt-0.5 text-[11px] text-muted-foreground md:hidden">
                    {fmtMs(r.FirstTokenMs)} / {fmtMs(r.LatencyMs)}
                  </div>
                </Td>
                <Td className="hidden max-w-[120px] truncate md:table-cell">{r.key_name || '-'}</Td>
                <Td className="hidden max-w-[130px] truncate md:table-cell">
                  {r.account_name || (r.AccountID ? `#${r.AccountID}` : '-')}
                </Td>
                <Td className="max-w-[170px] truncate font-medium">
                  {r.RequestedModel || r.Model || '-'}
                  <div className="mt-0.5 flex flex-wrap gap-1 md:hidden">
                    <span className="text-[11px] text-muted-foreground">{r.Protocol}</span>
                    {!!r.key_name && <span className="text-[11px] text-muted-foreground">· {r.key_name}</span>}
                    <span className="text-[11px] text-muted-foreground">
                      · {r.account_name || (r.AccountID ? `#${r.AccountID}` : '无账号')}
                    </span>
                  </div>
                </Td>
                <Td className="hidden text-muted-foreground md:table-cell">{r.Protocol}</Td>
                <Td className="tnum whitespace-nowrap text-right">
                  <span className="inline-flex flex-wrap items-center justify-end gap-x-2 gap-y-0.5">
                    <span className="inline-flex items-center gap-0.5 text-[var(--success)]">
                      <ArrowDown className="h-3 w-3" />
                      {fmtCompact(r.InputTokens)}
                    </span>
                    <span className="inline-flex items-center gap-0.5 text-muted-foreground">
                      <ArrowUp className="h-3 w-3" />
                      {fmtCompact(r.OutputTokens)}
                    </span>
                    {!!r.CachedTokens && (
                      <span className="text-muted-foreground/80" title="缓存命中（读取）">
                        缓存 {fmtCompact(r.CachedTokens)}
                      </span>
                    )}
                    {!!r.CacheCreationTokens && (
                      <span className="text-muted-foreground/80" title="缓存写入（新建缓存，单价通常高于命中）">
                        写入 {fmtCompact(r.CacheCreationTokens)}
                      </span>
                    )}
                  </span>
                </Td>
                <Td className="tnum hidden whitespace-nowrap text-right md:table-cell">
                  {fmtMs(r.FirstTokenMs)} / {fmtMs(r.LatencyMs)}
                </Td>
                <Td className="tnum hidden text-muted-foreground md:table-cell">{r.ClientIP}</Td>
              </Tr>
            ))}
            {!loading && logs.length === 0 && (
              <Tr>
                <Td colSpan={9} className="py-10 text-center text-muted-foreground">没有符合条件的日志</Td>
              </Tr>
            )}
          </tbody>
        </Table>
      </TableShell>

      <div className="flex items-center justify-between">
        <span className="text-[12px] text-muted-foreground">
          第 {data?.page ?? 1} 页 · 每页 {PAGE_SIZE}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" disabled={(data?.page ?? 1) <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <Button variant="outline" size="sm" disabled={!data?.has_more} onClick={() => setPage((p) => p + 1)}>
            下一页
          </Button>
        </div>
      </div>

      <Modal
        open={cleanupOpen}
        title="清理调用日志"
        onClose={() => setCleanupOpen(false)}
        footer={<Button variant="outline" onClick={() => setCleanupOpen(false)}>取消</Button>}
      >
        <Field label="清理范围" hint="只删调用日志，不影响账号 / 密钥 / 路由配置">
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={cleaning} onClick={() => void cleanup({ days: 7 })}>
              清理 7 天前
            </Button>
            <Button variant="outline" disabled={cleaning} onClick={() => void cleanup({ days: 30 })}>
              清理 30 天前
            </Button>
            <Button variant="destructive" disabled={cleaning} onClick={() => void cleanup({ all: true })}>
              清空全部
            </Button>
          </div>
        </Field>
        <p className="text-[12px] text-muted-foreground">
          自动清理由「设置 → 日志保留（天）」控制，这里只做一次性手动清理。
        </p>
      </Modal>

      {detail && (
        <div
          className="fixed inset-0 z-50 flex justify-end bg-black/40"
          onClick={() => setDetail(null)}
          role="dialog"
          aria-modal="true"
        >
          <div
            className="h-full w-[520px] max-w-full overflow-y-auto bg-background p-5 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-[15px] font-semibold">日志详情</h2>
              <Button variant="ghost" size="sm" onClick={() => setDetail(null)}>
                <Trash2 className="h-3.5 w-3.5" /> 关闭
              </Button>
            </div>
            <dl className="text-[13px]">
              {([
                ['时间', fmtTime(detail.CreatedAt)],
                ['状态', String(detail.Status)],
                ['协议', detail.Protocol],
                ['流式', detail.Stream ? '流式' : '非流式'],
                ['请求模型', detail.RequestedModel || '-'],
                ['上游模型', detail.Model || '-'],
                ['密钥', detail.key_name || '-'],
                ['账号', detail.account_name || (detail.AccountID ? `#${detail.AccountID}` : '-')],
                ['输入 Token', String(detail.InputTokens || 0)],
                ['输出 Token', String(detail.OutputTokens || 0)],
                ['缓存命中', `${detail.CachedTokens || 0}（${hitRate(detail.CachedTokens, detail.InputTokens)}）`],
                ['总 Token', String((detail.InputTokens || 0) + (detail.OutputTokens || 0))],
                ['首字', fmtMs(detail.FirstTokenMs)],
                ['总耗时', fmtMs(detail.LatencyMs)],
                ['尝试', String(detail.Attempts || 1)],
                ['结束原因', detail.FinishReason || '-'],
                ['IP', detail.ClientIP || '-'],
                ['User-Agent', detail.UserAgent || '-'],
              ] as [string, string][]).map(([k, v]) => (
                <div key={k} className="flex justify-between gap-4 border-b py-2">
                  <dt className="shrink-0 text-muted-foreground">{k}</dt>
                  <dd className="break-all text-right">{v}</dd>
                </div>
              ))}
            </dl>
            {copyHint && <p className="mt-3 text-[12px] text-muted-foreground">{copyHint}</p>}

            <LogBlock
              title="错误摘要"
              hint="列表与响应里回给客户端的那句话"
              text={detail.ErrorBrief || ''}
              onCopy={copyText}
            />
            <LogBlock
              title="完整上游返回"
              hint="插件透传的上游状态行 + 响应体（超过 8KB 截断）；400/500 排查看这里"
              text={detailRaw.error_detail || ''}
              onCopy={copyText}
            />
            <LogBlock
              title="请求原文"
              hint="客户端发来的原始 JSON（超过 8KB 截断），可与上游返回对照看协议转换"
              text={detailRaw.request_body || ''}
              onCopy={copyText}
            />
          </div>
        </div>
      )}
    </div>
  )
}
