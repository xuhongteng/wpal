package server

import (
	"bytes"
	"io"
	"time"
)

// zeroTime 用于 ServeContent，表示内容无具体修改时间（统一用 no-cache 头控制缓存）。
var zeroTime = time.Time{}

// newBytesReader 把字节切片包装成 io.ReadSeeker。
func newBytesReader(b []byte) io.ReadSeeker { return bytes.NewReader(b) }
