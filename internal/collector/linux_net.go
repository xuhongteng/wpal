//go:build linux

package collector

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

type netCounter struct {
	rxBytes, txBytes     uint64
	rxPackets, txPackets uint64
	errIn, errOut        uint64
	dropIn, dropOut      uint64
}

// tcpStates 把 /proc/net/tcp 的十六进制状态码翻译成名字。
var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1",
	"05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT",
	"09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING", "0C": "NEW_SYN_RECV",
}

// collectNet 采集网卡流量速率与 TCP 连接状态分布。
func (c *linuxCollector) collectNet(dt float64) Net {
	ifaces := c.collectIfaces(dt)
	tcp := c.collectTCP()
	total := 0
	for _, v := range tcp {
		total += v
	}
	return Net{Interfaces: ifaces, TCP: tcp, TCPTotal: total}
}

func (c *linuxCollector) collectIfaces(dt float64) []Iface {
	f, err := os.Open(c.procPath("net/dev"))
	if err != nil {
		return []Iface{}
	}
	defer f.Close()

	cur := map[string]netCounter{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		atoi := func(i int) uint64 { v, _ := strconv.ParseUint(fields[i], 10, 64); return v }
		cur[name] = netCounter{
			rxBytes: atoi(0), rxPackets: atoi(1), errIn: atoi(2), dropIn: atoi(3),
			txBytes: atoi(8), txPackets: atoi(9), errOut: atoi(10), dropOut: atoi(11),
		}
	}

	out := []Iface{}
	for name, c2 := range cur {
		virtual := hasAnyPrefix(name, virtualNicPrefix)
		if virtual && !c.opts.IncludeVirtualNics {
			continue
		}
		p := c.prevNet[name]
		out = append(out, Iface{
			Name:    name,
			RxBPS:   max0(float64(int64(c2.rxBytes)-int64(p.rxBytes)) / dt),
			TxBPS:   max0(float64(int64(c2.txBytes)-int64(p.txBytes)) / dt),
			RxPPS:   max0(float64(int64(c2.rxPackets)-int64(p.rxPackets)) / dt),
			TxPPS:   max0(float64(int64(c2.txPackets)-int64(p.txPackets)) / dt),
			RxTotal: c2.rxBytes,
			TxTotal: c2.txBytes,
			ErrIn:   c2.errIn,
			ErrOut:  c2.errOut,
			DropIn:  c2.dropIn,
			DropOut: c2.dropOut,
			Virtual: virtual,
		})
	}
	c.prevNet = cur

	sort.Slice(out, func(i, j int) bool {
		return out[i].RxBPS+out[i].TxBPS > out[j].RxBPS+out[j].TxBPS
	})
	return out
}

// collectTCP 统计 TCP 连接状态分布（IPv4 + IPv6）。
func (c *linuxCollector) collectTCP() map[string]int {
	counts := map[string]int{}
	for _, f := range []string{"net/tcp", "net/tcp6"} {
		file, err := os.Open(c.procPath(f))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		sc.Scan() // 跳过表头
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 4 {
				continue
			}
			name, ok := tcpStates[strings.ToUpper(fields[3])]
			if !ok {
				name = strings.ToUpper(fields[3])
			}
			counts[name]++
		}
		file.Close()
	}
	return counts
}
