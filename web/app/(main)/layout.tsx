import { AppShell } from '@/components/app-shell'

// 受保护区域：所有后台页面共用同一套外壳（侧栏 + 顶栏 + 滚动内容区）
export default function MainLayout({ children }: { children: React.ReactNode }) {
  return <AppShell>{children}</AppShell>
}
