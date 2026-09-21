// Package web — 仪表盘静态资源：Next.js 静态导出产物经 go:embed 嵌入单二进制。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler SPA 静态服务。
//
// 三条分支（踩过的坑都在这里）：
//   - 空路径 → 直接给 index.html
//   - 命中文件 → FileServer 直出
//   - 命中目录 → 补斜杠后交给 FileServer 走目录索引（Next 静态导出是 <route>/index.html）
//   - 都没命中 → 回落 index.html，把深链接交给客户端路由
//
// 注意：fs.Stat 不接受尾部斜杠（embed.FS 的路径校验会直接报错），所以比较前必须先 Trim。
func Handler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")

	serveIndex := func(w http.ResponseWriter) {
		if index == nil {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if p == "" {
			serveIndex(w)
			return
		}
		info, err := fs.Stat(sub, p)
		if err != nil {
			serveIndex(w)
			return
		}
		if info.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
			// 目录要带斜杠，否则相对资源会解析错
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
