import * as React from 'react'
import { cn } from '@/lib/utils'

type Tone = 'neutral' | 'success' | 'warning' | 'danger'

const tones: Record<Tone, string> = {
  neutral: 'bg-muted text-muted-foreground',
  success: 'bg-[color-mix(in_oklch,var(--success)_16%,transparent)] text-[var(--success)]',
  warning: 'bg-[color-mix(in_oklch,var(--warning)_18%,transparent)] text-[var(--warning)]',
  danger: 'bg-[color-mix(in_oklch,var(--destructive)_16%,transparent)] text-[var(--destructive)]',
}

export function Badge({ tone = 'neutral', className, ...props }: React.HTMLAttributes<HTMLSpanElement> & { tone?: Tone }) {
  return (
    <span
      className={cn('inline-flex items-center rounded px-1.5 py-0.5 text-[11.5px] font-medium', tones[tone], className)}
      {...props}
    />
  )
}

// HTTP 状态码 → 语义色（4xx 警告、5xx 失败）
export function statusTone(code: number): Tone {
  if (code < 400) return 'success'
  if (code < 500) return 'warning'
  return 'danger'
}
