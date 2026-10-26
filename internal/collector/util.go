package collector

// 本文件放置各平台共用的工具函数。
//
// 注意：不要把这里的东西放进带 build tag 的文件里——
// 非 Linux 平台的实现（other.go）同样要用它们。

// round1 四舍五入到 1 位小数。
func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// round2 四舍五入到 2 位小数。
func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

// round3 四舍五入到 3 位小数。
func round3(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }

// max0 把负数归零（计数器回绕时可能出现负差值）。
func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// hasAnyPrefix 判断字符串是否以任一前缀开头。
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

// realFS 是计入容量看板的真实文件系统，其余（tmpfs/overlay/squashfs…）忽略。
var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "jfs": true, "reiserfs": true, "ufs": true,
	"vfat": true, "ntfs": true, "ntfs3": true, "exfat": true, "apfs": true,
}

// virtualNicPrefix 是容器/虚拟化产生的网卡，默认不计入总流量。
var virtualNicPrefix = []string{"docker", "veth", "br-", "virbr", "cni", "flannel", "tun", "tap"}
