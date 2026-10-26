package collector

import (
	"math"
	"testing"
)

func TestCPUTimesPct(t *testing.T) {
	// 100 个 jiffy：60 user + 20 nice + 10 system + 10 idle
	d := cpuTimes{user: 60, nice: 20, system: 10, idle: 10}
	p := d.pct()
	if math.Abs(p.Busy-90) > 0.01 {
		t.Errorf("busy 应为 90，得到 %v", p.Busy)
	}
	if math.Abs(p.User-80) > 0.01 {
		t.Errorf("user 应为 80（user+nice），得到 %v", p.User)
	}
	if math.Abs(p.System-10) > 0.01 {
		t.Errorf("system 应为 10，得到 %v", p.System)
	}
	if math.Abs(p.Idle-10) > 0.01 {
		t.Errorf("idle 应为 10，得到 %v", p.Idle)
	}
	// 各部分之和应为 100
	sum := p.Busy + p.Idle + p.Iowait + p.Steal
	if math.Abs(sum-100) > 0.05 {
		t.Errorf("各部分之和应为 100，得到 %v", sum)
	}
}

func TestCPUTimesPctZeroDelta(t *testing.T) {
	p := cpuTimes{}.pct()
	if p.Busy != 0 || p.Idle != 100 {
		t.Errorf("零增量应返回 busy=0 idle=100，得到 %+v", p)
	}
}

func TestCPUTimesPctExcludesGuest(t *testing.T) {
	// guest / guest_nice 在内核里已计入 user / nice，psutil 单独列出。
	// 本实现刻意不设这两个字段，因此 total 天然不含 guest，不会重复计数。
	d := cpuTimes{user: 50, idle: 50}
	p := d.pct()
	if math.Abs(p.Busy-50) > 0.01 {
		t.Errorf("busy 应为 50，得到 %v", p.Busy)
	}
	if math.Abs(p.User-50) > 0.01 {
		t.Errorf("user 应为 50，得到 %v", p.User)
	}
}

func TestParseCPUFields(t *testing.T) {
	got := parseCPUFields([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"})
	want := cpuTimes{user: 1, nice: 2, system: 3, idle: 4, iowait: 5, irq: 6, softirq: 7, steal: 8}
	if got != want {
		t.Errorf("解析结果不对: %+v", got)
	}
	// 字段不足时应补零而不是 panic
	short := parseCPUFields([]string{"1"})
	if short.user != 1 || short.idle != 0 {
		t.Errorf("缺字段时应补零，得到 %+v", short)
	}
}

func TestParseProcStat(t *testing.T) {
	// 真实格式：pid (comm with spaces) state ppid ...
	raw := "1234 (my proc) S 1 1234 1234 0 -1 4194560 100 0 0 0 " +
		"500 250 0 0 20 0 5 0 1000 2000000 500 18446744073709551615"
	st, ok := parseProcStat(raw)
	if !ok {
		t.Fatal("应解析成功")
	}
	if st.name != "my proc" {
		t.Errorf("进程名应为 'my proc'，得到 %q", st.name)
	}
	if st.state != "S" {
		t.Errorf("state 应为 S，得到 %q", st.state)
	}
	if st.utime != 500 || st.stime != 250 {
		t.Errorf("utime/stime 应为 500/250，得到 %v/%v", st.utime, st.stime)
	}
	if st.threads != 5 {
		t.Errorf("线程数应为 5，得到 %v", st.threads)
	}
	if st.rss != 500 {
		t.Errorf("rss 应为 500，得到 %v", st.rss)
	}
}

func TestParseProcStatInvalid(t *testing.T) {
	if _, ok := parseProcStat("garbage"); ok {
		t.Error("非法输入应返回 false")
	}
	if _, ok := parseProcStat("1 (x) S 1"); ok {
		t.Error("字段不足应返回 false")
	}
}

func TestMajorMinor(t *testing.T) {
	// Linux dev_t 编码：major = (dev >> 8) & 0xfff, minor = (dev & 0xff) | ((dev >> 12) & 0xfff00)
	// major 254 minor 3 → dev = 3 | (254 << 8) = 65027
	maj, min := majorMinor(65027)
	if maj != 254 || min != 3 {
		t.Errorf("期望 254:3，得到 %d:%d", maj, min)
	}
	maj, min = majorMinor(1)
	if maj != 0 || min != 1 {
		t.Errorf("期望 0:1，得到 %d:%d", maj, min)
	}
}

func TestUnescapeMount(t *testing.T) {
	cases := map[string]string{
		"/mnt/my\\040disk": "/mnt/my disk",
		"/plain":           "/plain",
		"/a\\011b":         "/a\tb",
	}
	for in, want := range cases {
		if got := unescapeMount(in); got != want {
			t.Errorf("unescapeMount(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestRoundHelpers(t *testing.T) {
	// 只用无歧义的值：1.005 这类边界在浮点里本身就无法精确表示，
	// Go 与 Python 在那里会得到同样的结果（都是 1.0），行为一致。
	if round1(1.2) != 1.2 || round1(1.26) != 1.3 || round1(1.24) != 1.2 {
		t.Errorf("round1 不对: %v %v %v", round1(1.2), round1(1.26), round1(1.24))
	}
	if round2(1.2) != 1.2 || round2(1.26) != 1.26 || round2(1.234) != 1.23 {
		t.Errorf("round2 不对: %v %v %v", round2(1.2), round2(1.26), round2(1.234))
	}
	if round3(1.2345) != 1.235 || round3(1.2) != 1.2 {
		t.Errorf("round3 不对: %v %v", round3(1.2345), round3(1.2))
	}
}

func TestHasAnyPrefix(t *testing.T) {
	prefixes := []string{"docker", "veth", "br-"}
	if !hasAnyPrefix("veth123abc", prefixes) {
		t.Error("veth123abc 应匹配")
	}
	if hasAnyPrefix("eth0", prefixes) {
		t.Error("eth0 不应匹配")
	}
}

func TestRealFSFilter(t *testing.T) {
	for _, fs := range []string{"ext4", "xfs", "btrfs", "vfat", "ntfs"} {
		if !realFS[fs] {
			t.Errorf("%s 应被视为真实文件系统", fs)
		}
	}
	for _, fs := range []string{"tmpfs", "overlay", "squashfs", "proc", "sysfs"} {
		if realFS[fs] {
			t.Errorf("%s 不应被视为真实文件系统", fs)
		}
	}
}
