import type { NextConfig } from 'next'

// 静态导出：产物由 Go 的 go:embed 打进单二进制（web/dist），
// 因此这里必须是纯静态 SPA —— 不能用 server actions / ISR / 动态路由参数之外的运行时能力。
const nextConfig: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: { unoptimized: true },
  // 开发时把 /admin 与 /v1 代理到本地 Go 进程，避免 CORS 与 Cookie 差异
  async rewrites() {
    return process.env.NODE_ENV === 'development'
      ? [
          { source: '/admin/:path*', destination: 'http://127.0.0.1:8080/admin/:path*' },
          { source: '/v1/:path*', destination: 'http://127.0.0.1:8080/v1/:path*' },
        ]
      : []
  },
}

export default nextConfig
