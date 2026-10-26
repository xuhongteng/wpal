// Package web 承载前端静态资源。
//
// 通过 embed 把整个看板打进二进制，使得最终产物是"单文件、零依赖"，
// 丢到任何机器上直接跑，不需要 Python/Node 运行时，也不需要容器。
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html style.css app.js i18n.js locales vendor
var FS embed.FS

// Sub 返回以 web 目录为根的文件系统。
func Sub() fs.FS { return FS }
