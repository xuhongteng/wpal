package collector

import (
	"testing"
)

// 本文件只放跨平台通用的单元测试。
// Linux 专属实现的测试见 collector_linux_test.go（带 //go:build linux）。

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
