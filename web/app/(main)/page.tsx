// 根路径就是概览：静态导出下 next/navigation 的 redirect() 不可用
// （会生成 __next_error__ 页面），这里直接复用概览页组件。
export { default } from './dashboard/page'
