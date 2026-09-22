'use client'

import { useEffect } from 'react'
import { Button } from '@/components/ui/button'

// 轻量弹层：静态导出下不引 Radix Dialog 也能用（Esc 关闭 + 点遮罩关闭 + 焦点留在面板）
export function Modal({
  open,
  title,
  onClose,
  footer,
  children,
  width = 520,
}: {
  open: boolean
  title: string
  onClose: () => void
  footer?: React.ReactNode
  children: React.ReactNode
  width?: number
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 p-6 pt-[10vh]" onClick={onClose} role="dialog" aria-modal="true" aria-label={title}>
      <div
        className="w-full overflow-hidden rounded-lg border bg-card shadow-xl"
        style={{ maxWidth: width }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b px-5 py-3">
          <h2 className="text-[14px] font-semibold">{title}</h2>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="关闭">✕</Button>
        </div>
        <div className="max-h-[62vh] overflow-y-auto px-5 py-4">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t px-5 py-3">{footer}</div>}
      </div>
    </div>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="mb-3 flex flex-col gap-1.5">
      <span className="text-[12.5px] text-muted-foreground">{label}</span>
      {children}
      {hint && <span className="text-[11.5px] text-muted-foreground">{hint}</span>}
    </label>
  )
}
