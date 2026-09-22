import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'ClawProxyHub-Next',
  description: '把官方 AI 桌面客户端变成你自己的 OpenAI 兼容网关',
}

// 首屏前把主题打到 <html> 上，避免暗色用户看到白屏闪烁
const themeBootstrap = `
try {
  var t = localStorage.getItem('cph-theme')
  if (t === 'dark') document.documentElement.classList.add('dark')
} catch (e) {}
`

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeBootstrap }} />
      </head>
      <body>{children}</body>
    </html>
  )
}
