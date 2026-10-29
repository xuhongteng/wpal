//go:build darwin

package collector

import "testing"

// TestParsePsTime 覆盖 ps 累计 CPU 时间的三种格式。
func TestParsePsTime(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"", 0},
		{"0:00.00", 0},
		{"0:20.47", 20.47},
		{"292:03.46", 292*60 + 3.46},
		{"1:00:00.00", 3600},
		{"2-01:00:00", 2*86400 + 3600},
	}
	for _, c := range cases {
		if got := parsePsTime(c.in); got != c.want {
			t.Errorf("parsePsTime(%q) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

// TestSplitBulkProcLine 验证「前 7 个定长字段 + 行尾进程名」的切分。
// 关键场景是进程名本身含空格（如 "Google Chrome Helper"）。
func TestSplitBulkProcLine(t *testing.T) {
	line := "  424   1   501 xuhongteng   30096 125:09.73 Ss   /System/Library/PrivateFrameworks/Foo"
	fields, comm := splitBulkProcLine(line)
	if fields == nil {
		t.Fatal("应能解析出字段")
	}
	if len(fields) != 7 {
		t.Fatalf("应解析出 7 个定长字段，实际 %d", len(fields))
	}
	if fields[0] != "424" || fields[1] != "1" || fields[2] != "501" {
		t.Errorf("PID/PPID/UID 解析错误: %v", fields[:3])
	}
	if fields[3] != "xuhongteng" {
		t.Errorf("用户名解析错误: %q", fields[3])
	}
	if fields[4] != "30096" {
		t.Errorf("RSS 解析错误: %q", fields[4])
	}
	if fields[5] != "125:09.73" {
		t.Errorf("时间解析错误: %q", fields[5])
	}
	if fields[6] != "Ss" {
		t.Errorf("状态解析错误: %q", fields[6])
	}
	if comm != "/System/Library/PrivateFrameworks/Foo" {
		t.Errorf("进程名解析错误: %q", comm)
	}

	// 进程名含空格
	f2, c2 := splitBulkProcLine("  900   1   501 me   1024   0:01.00 S    Google Chrome Helper")
	if f2 == nil || c2 != "Google Chrome Helper" {
		t.Errorf("含空格进程名解析错误: %q", c2)
	}

	// 字段不足应返回 nil，由调用方跳过
	if f3, _ := splitBulkProcLine("  1 2 3"); f3 != nil {
		t.Error("字段不足时应返回 nil")
	}
}

// TestMapPsStat 验证 ps 状态码映射。
func TestMapPsStat(t *testing.T) {
	cases := map[string]string{
		"R":  "running",
		"R+": "running",
		"Ss": "sleeping",
		"T":  "stopped",
		"Z":  "zombie",
		"I":  "idle",
		"U":  "waiting",
		"@":  "unknown",
		"":   "",
	}
	for in, want := range cases {
		if got := mapPsStat(in); got != want {
			t.Errorf("mapPsStat(%q) = %q，期望 %q", in, got, want)
		}
	}
}
