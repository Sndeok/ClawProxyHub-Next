// 把 Next 的静态导出结果搬到 Go 的嵌入目录（web/dist）。
// 单独一个脚本而不是直接输出到那里：Next 需要先自己写完 out/ 再原子替换，
// 否则构建中途失败会留下残缺的 dist，go build 会嵌进半成品。
import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const out = resolve(here, '..', 'out')
const dest = resolve(here, '..', '..', 'web', 'dist')

if (!existsSync(out)) {
  console.error('[export] 未找到 out/，请先跑 next build')
  process.exit(1)
}
rmSync(dest, { recursive: true, force: true })
mkdirSync(dest, { recursive: true })
cpSync(out, dest, { recursive: true })
console.log('[export] 静态产物已同步到 web/dist（供 go:embed 打包）')
