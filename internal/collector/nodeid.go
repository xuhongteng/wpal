package collector

import (
	"os"
	"strings"
)

// resolveNodeID 确定节点唯一标识，供将来多机汇聚时区分数据来源。
//
// 优先级：显式配置 > 系统 machine-id > hostname。
// machine-id 在重启后保持不变、且不随主机名变化，是比 hostname 更稳的标识；
// 但它只在 Linux 上普遍存在，其余平台回退到 hostname。
func resolveNodeID(configured, hostname string) string {
	if v := strings.TrimSpace(configured); v != "" {
		return v
	}
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v
			}
		}
	}
	return hostname
}
