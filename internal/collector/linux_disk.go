//go:build linux

package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// skipMountPrefix 这些前缀下的挂载点是伪文件系统或容器内部视图。
var skipMountPrefix = []string{"/proc", "/sys", "/dev", "/run", "/snap", "/host"}

type mountEntry struct {
	majMin string
	mount  string
	fstype string
	source string
}

type diskCounter struct {
	readBytes, writeBytes uint64
	readCount, writeCount uint64
	readTime, writeTime   uint64
}

// collectDisk 采集分区容量与设备 IO 速率。
func (c *linuxCollector) collectDisk(dt float64) Disk {
	parts := c.collectPartitions()
	io := c.collectDiskIO(dt)
	return Disk{Partitions: parts, IO: io}
}

// collectPartitions 解析宿主挂载表并统计容量。
//
// 关键点：
//  1. 必须读 /proc/1/mountinfo。/proc/mounts 等价于 /proc/self/mounts，
//     在容器里读到的是容器自己的挂载表，而不是宿主的。
//  2. 用 major:minor 校验 bind mount 出来的路径是否真属于该文件系统，
//     否则会把独立分区（如 /boot/efi）误算成根盘容量。
func (c *linuxCollector) collectPartitions() []Partition {
	entries := c.readMountinfo()
	best := map[string]mountEntry{}
	for _, e := range entries {
		path := c.hostPath(e.mount)
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		maj, min := majorMinor(uint64(sys.Dev))
		if majMin := itoa(maj) + ":" + itoa(min); majMin != e.majMin {
			// 该挂载点位于独立文件系统，bind 挂载的宿主根里量不到，跳过
			continue
		}
		if cur, ok := best[e.majMin]; !ok || len(e.mount) < len(cur.mount) {
			best[e.majMin] = e
		}
	}

	out := make([]Partition, 0, len(best))
	for _, e := range best {
		path := c.hostPath(e.mount)
		var fs syscall.Statfs_t
		if err := syscall.Statfs(path, &fs); err != nil {
			continue
		}
		bsize := uint64(fs.Bsize)
		total := fs.Blocks * bsize
		availToRoot := fs.Bfree * bsize
		availToUser := fs.Bavail * bsize
		used := total - availToRoot
		free := availToUser
		// 口径与 psutil 一致：百分比以"用户可用空间"为分母，
		// 因此比 df 的数值略低（ext4 默认给 root 预留 5%）。
		totalUser := used + availToUser
		pct := 0.0
		if totalUser > 0 {
			pct = float64(used) / float64(totalUser) * 100
		}
		inodeTotal := uint64(fs.Files)
		inodeFree := uint64(fs.Ffree)
		inodePct := 0.0
		if inodeTotal > 0 {
			inodePct = float64(inodeTotal-inodeFree) / float64(inodeTotal) * 100
		}
		out = append(out, Partition{
			Device:       e.source,
			Mount:        e.mount,
			Fstype:       e.fstype,
			Total:        total,
			Used:         used,
			Free:         free,
			Percent:      round1(pct),
			InodePercent: round2(inodePct),
			InodeTotal:   inodeTotal,
			InodeFree:    inodeFree,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}

// readMountinfo 解析 /proc/1/mountinfo。
func (c *linuxCollector) readMountinfo() []mountEntry {
	raw := c.readFirstAvailable("1/mountinfo", "self/mountinfo")
	out := []mountEntry{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 10 {
			continue
		}
		sep := -1
		for i := 6; i < len(parts); i++ {
			if parts[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || sep+2 >= len(parts) {
			continue
		}
		fstype := parts[sep+1]
		if !realFS[fstype] {
			continue
		}
		mount := unescapeMount(parts[4])
		if skipMount(mount) {
			continue
		}
		out = append(out, mountEntry{
			majMin: parts[2],
			mount:  mount,
			fstype: fstype,
			source: parts[sep+2],
		})
	}
	return out
}

func (c *linuxCollector) readFirstAvailable(paths ...string) string {
	for _, p := range paths {
		if b, err := os.ReadFile(c.procPath(p)); err == nil {
			return string(b)
		}
	}
	return ""
}

func skipMount(mount string) bool {
	for _, p := range skipMountPrefix {
		if mount == p || strings.HasPrefix(mount, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// unescapeMount 处理 mountinfo 中的八进制转义（\040 空格、\011 制表符等）。
func unescapeMount(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// hostPath 把宿主路径映射到可 stat 的本地路径。
func (c *linuxCollector) hostPath(p string) string {
	if c.opts.HostRoot == "" {
		return p
	}
	return filepath.Join(c.opts.HostRoot, strings.TrimPrefix(p, "/"))
}

// majorMinor 把 Linux 的 dev_t 拆成 major:minor。
func majorMinor(dev uint64) (uint32, uint32) {
	major := uint32((dev >> 8) & 0xfff)
	minor := uint32(dev&0xff) | uint32((dev>>12)&0xfff00)
	return major, minor
}

func itoa(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// collectDiskIO 解析 /proc/diskstats 计算各设备读写速率。
func (c *linuxCollector) collectDiskIO(dt float64) []DiskIO {
	f, err := os.Open(c.procPath("diskstats"))
	if err != nil {
		return []DiskIO{}
	}
	defer f.Close()

	cur := map[string]diskCounter{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) < 11 {
			continue
		}
		name := parts[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		atoi := func(i int) uint64 { v, _ := strconv.ParseUint(parts[i], 10, 64); return v }
		cur[name] = diskCounter{
			readBytes:  atoi(5) * 512,
			writeBytes: atoi(9) * 512,
			readCount:  atoi(3),
			writeCount: atoi(7),
			readTime:   atoi(6),
			writeTime:  atoi(10),
		}
	}

	out := []DiskIO{}
	for name, c2 := range cur {
		p, ok := c.prevDisk[name]
		if !ok {
			continue
		}
		readBPS := float64(int64(c2.readBytes)-int64(p.readBytes)) / dt
		writeBPS := float64(int64(c2.writeBytes)-int64(p.writeBytes)) / dt
		readIOPS := float64(int64(c2.readCount)-int64(p.readCount)) / dt
		writeIOPS := float64(int64(c2.writeCount)-int64(p.writeCount)) / dt
		busy := 0.0
		dTime := float64(int64(c2.readTime)-int64(p.readTime)) +
			float64(int64(c2.writeTime)-int64(p.writeTime))
		if dTime > 0 {
			busy = dTime / 1000.0 / dt * 100.0
			if busy > 100 {
				busy = 100
			}
		}
		out = append(out, DiskIO{
			Name:      name,
			ReadBPS:   max0(readBPS),
			WriteBPS:  max0(writeBPS),
			ReadIOPS:  max0(readIOPS),
			WriteIOPS: max0(writeIOPS),
			Busy:      round2(busy),
		})
	}
	c.prevDisk = cur

	sort.Slice(out, func(i, j int) bool {
		return out[i].ReadBPS+out[i].WriteBPS > out[j].ReadBPS+out[j].WriteBPS
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}
