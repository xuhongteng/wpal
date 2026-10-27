// Package i18n 存放后端侧的中文兜底文案。
//
// 设计原则：
//   - 后端事件同时给出 label_key / detail_key（供前端查字典）与
//     label / detail（中文兜底），因此旧前端不做任何改动也能正常显示；
//   - 英文等其它语言由前端字典负责，后端不感知语言。
package i18n

import (
	"fmt"
	"sort"
)

// labels 是告警标签的中文文案。
var labels = map[string]string{
	"alert.cpu":    "CPU 使用率",
	"alert.memory": "内存使用率",
	"alert.swap":   "Swap 使用率",
	"alert.disk":   "磁盘使用率",
	"alert.inode":  "inode 使用率",
	"alert.load":   "负载/核数",
}

// details 是告警详情的中文文案模板，占位符为 {name}。
var details = map[string]string{
	"alert.cpu.detail":    "负载 {load1} / {cores} 核",
	"alert.memory.detail": "可用 {available_gib} GiB",
	"alert.swap.detail":   "Swap 已用 {used_gib} GiB",
	"alert.disk.detail":   "挂载点 {mount}",
	"alert.inode.detail":  "挂载点 {mount}",
	"alert.load.detail":   "load1 {load1}",
}

// Label 返回 key 对应的中文标签；未知 key 原样返回。
func Label(key string) string {
	if s, ok := labels[key]; ok {
		return s
	}
	return key
}

// Detail 用参数渲染中文详情模板；未知模板返回空串。
func Detail(key string, params map[string]any) string {
	tpl, ok := details[key]
	if !ok {
		return ""
	}
	return render(tpl, params)
}

// render 做最朴素的 {key} 占位替换。
func render(tpl string, params map[string]any) string {
	if len(params) == 0 {
		return tpl
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 保证渲染顺序稳定
	out := tpl
	for _, k := range keys {
		out = replaceAll(out, "{"+k+"}", fmt.Sprintf("%v", params[k]))
	}
	return out
}

func replaceAll(s, old, repl string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + repl
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
