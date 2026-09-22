import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function fmtNum(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return '-'
  const n = Number(v)
  return Number.isFinite(n) ? String(Math.round(n * 100) / 100) : String(v)
}

export function fmtCompact(n: number | undefined | null): string {
  const v = n ?? 0
  if (v < 1000) return String(v)
  if (v < 1_000_000) return `${(v / 1000).toFixed(1).replace(/\.0$/, '')}K`
  return `${(v / 1_000_000).toFixed(2)}M`
}

export function fmtTime(t: string | null | undefined): string {
  return t ? t.replace('T', ' ').slice(0, 19) : '-'
}

export function fmtMs(ms: number | undefined | null): string {
  if (!ms) return '-'
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(2)}s`
}

// 缓存命中率：命中是输入的子集，分母用输入总量
export function hitRate(cached: number | undefined, input: number | undefined): string {
  if (!cached || !input || input <= 0) return '0%'
  return `${Math.min(100, (cached / input) * 100).toFixed(1)}%`
}
