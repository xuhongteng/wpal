// Package version 提供构建版本信息，通过 ldflags 注入。
package version

import (
	"fmt"
	"runtime"
)

// 这些变量由构建脚本通过 -ldflags -X 注入。
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info 是完整的版本描述。
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get 返回当前构建的版本信息。
func Get() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// String 返回适合打印的一行版本信息。
func (i Info) String() string {
	return fmt.Sprintf("wpal %s (commit %s, built %s, %s %s/%s)",
		i.Version, i.Commit, i.Date, i.Go, i.OS, i.Arch)
}
