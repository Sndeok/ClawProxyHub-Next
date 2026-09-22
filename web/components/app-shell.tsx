'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useState } from 'react'
import {
  BarChart3, Boxes, ChevronLeft, FileText, GitBranch, KeyRound, ListTree,
  Menu, Moon, Network, Plug, Power, Settings, Sun, Users,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { clearToken, getToken } from '@/lib/api'
import { cn } from '@/lib/utils'

interface NavItem {
  href: string
  label: string
  icon: React.ComponentType<{ className?: string }>
}

const NAV: { group: string; items: NavItem[] }[] = [
  { group: '总览', items: [{ href: '/dashboard', label: '概览', icon: BarChart3, }] },
  {
    group: '资源',
    items: [
      { href: '/plugins', label: '插件', icon: Plug },
      { href: '/accounts', label: '账号', icon: Users, },
      { href: '/groups', label: '分组', icon: Boxes },
      { href: '/proxies', label: '代理', icon: Network },
    ],
  },
  {
    group: '流量',
    items: [
      { href: '/routes', label: '路由', icon: GitBranch, },
      { href: '/keys', label: '密钥', icon: KeyRound },
      { href: '/logs', label: '日志', icon: FileText, },
    ],
  },
  {
    group: '运维',
    items: [
      { href: '/tasks', label: '任务', icon: ListTree },
      { href: '/settings', label: '设置', icon: Settings },
    ],
  },
]

const TITLES: Record<string, { title: string; desc: string }> = {
  '/dashboard': { title: '概览', desc: '调用量 / 成功率 / Token 用量总览' },
  '/accounts': { title: '账号', desc: '上游账号登录 / 分组 / 调度' },
  '/routes': { title: '路由', desc: '对外模型别名 → 分组映射与降级' },
  '/logs': { title: '日志', desc: '调用日志与协议 / 用量明细' },
  '/plugins': { title: '插件', desc: '客户端插件安装 / 授权 / 任务能力' },
  '/groups': { title: '分组', desc: '同插件账号池与出站代理' },
  '/proxies': { title: '代理', desc: '出站代理配置与绑定' },
  '/keys': { title: '密钥', desc: '客户端调用凭据与路由授权' },
  '/tasks': { title: '任务', desc: '签到等维护任务调度与历史' },
  '/settings': { title: '设置', desc: '系统参数与管理员密码' },
}

export function AppShell({ children }: { children: React.ReactNode }) {
  // Next 静态导出带 trailingSlash，pathname 会是 /logs/ 这种形态；
// 统一去掉尾斜杠，标题与选中态才能与导航表里的键对上。
const rawPathname = usePathname()
const pathname = rawPathname === '/' ? '/' : rawPathname.replace(/\/+$/, '')
  const router = useRouter()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [dark, setDark] = useState(false)
  const [ready, setReady] = useState(false)

  // 未登录直接踢回登录页（静态导出没有服务端鉴权，只能前端守一道）
  useEffect(() => {
    if (!getToken()) {
      router.replace('/login')
      return
    }
    setReady(true)
    setCollapsed(window.localStorage.getItem('cph-sidebar') === 'collapsed')
    setDark(document.documentElement.classList.contains('dark'))
  }, [router])

  // 窄屏：抽屉形态，导航后自动收起
  useEffect(() => {
    setMobileOpen(false)
  }, [pathname])

  function toggleCollapsed() {
    const next = !collapsed
    setCollapsed(next)
    window.localStorage.setItem('cph-sidebar', next ? 'collapsed' : 'expanded')
  }

  function toggleDark() {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    window.localStorage.setItem('cph-theme', next ? 'dark' : 'light')
  }

  function logout() {
    clearToken()
    router.replace('/login')
  }

  // 根路径等价于概览页（静态导出下 / 与 /dashboard 是同一份内容）
  const metaKey = pathname === '/' ? '/dashboard' : pathname
  const meta = TITLES[metaKey] ?? { title: 'ClawProxyHub-Next', desc: '' }
  if (!ready) return null

  return (
    <div className="flex h-screen w-full overflow-hidden">
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/40 md:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 flex w-[232px] shrink-0 flex-col bg-[var(--sidebar)] transition-transform duration-200',
          'md:static md:z-auto md:translate-x-0 md:transition-[width]',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
          collapsed ? 'md:w-[64px]' : 'md:w-[232px]',
        )}
      >
        <div className="flex h-[60px] shrink-0 items-center gap-2 px-4">
          <span className="grid h-8 w-8 place-items-center rounded-md bg-primary text-[11px] font-bold text-primary-foreground">
            C
          </span>
          {(!collapsed || mobileOpen) && (
            <div className="min-w-0">
              <div className="truncate text-[14px] font-semibold">
                ClawProxyHub<span className="ml-1 rounded border px-1 text-[10px] font-medium text-muted-foreground">NEXT</span>
              </div>
              <div className="truncate text-[10.5px] text-muted-foreground">AI 反代网关</div>
            </div>
          )}
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-4">
          {NAV.map((group) => (
            <div key={group.group} className="mb-1">
              {(!collapsed || mobileOpen) && (
                <div className="px-2 pb-1 pt-3 text-[11px] font-medium text-muted-foreground">{group.group}</div>
              )}
              {group.items.map((item) => {
                const active = item.href === '/dashboard' ? pathname === '/' || pathname === '/dashboard' : pathname === item.href
                const Icon = item.icon
                const cls = cn(
  'mb-0.5 flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] transition-colors',
  active ? 'bg-accent font-semibold text-foreground' : 'text-muted-foreground hover:bg-accent/60',
)
                return (
                  <Link key={item.href} href={item.href} className={cls}>
                    <Icon className="h-4 w-4 shrink-0" />
                    {(!collapsed || mobileOpen) && <span className="truncate">{item.label}</span>}
                  </Link>
                )
              })}
            </div>
          ))}
        </nav>

        <button
          onClick={toggleCollapsed}
          className="hidden h-10 shrink-0 items-center justify-center gap-1 border-t text-[12px] text-muted-foreground hover:text-foreground md:flex"
        >
          <ChevronLeft className={cn('h-4 w-4 transition-transform', collapsed && 'rotate-180')} />
          {!collapsed && '收起'}
        </button>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-[60px] shrink-0 items-center justify-between gap-2 border-b bg-background/85 px-3 backdrop-blur md:gap-4 md:px-5">
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setMobileOpen(true)} aria-label="打开菜单">
            <Menu className="h-4 w-4" />
          </Button>
          <div className="min-w-0 flex-1">
            <div className="truncate text-[16px] font-semibold">{meta.title}</div>
            <div className="truncate text-[12px] text-muted-foreground">{meta.desc}</div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="icon" onClick={toggleDark} aria-label="切换主题">
              {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </Button>
            <Button variant="ghost" size="icon" onClick={logout} aria-label="退出登录">
              <Power className="h-4 w-4" />
            </Button>
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto p-3 md:p-5">{children}</main>
      </div>
    </div>
  )
}
