# Go 构建环境变量。
#
# 硬约束：/tmp 是 1.9 GB 的 tmpfs（内存盘），放不下 module cache 与构建临时文件，
# 必须把 GOPATH/GOMODCACHE/GOCACHE/GOTMPDIR 都放在 /home 下，
# 否则会触发 "disk quota exceeded"。
export GOROOT="${GOROOT:-$HOME/sdk/go}"
export GOPATH="${GOPATH:-$HOME/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export GOCACHE="${GOCACHE:-$GOPATH/.cache/go-build}"
export GOTMPDIR="${GOTMPDIR:-$GOPATH/tmp}"
export PATH="$GOROOT/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export CGO_ENABLED=0
