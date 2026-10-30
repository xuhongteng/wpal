//go:build darwin

package collector

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v4/cpu"
	"golang.org/x/sys/unix"
)

// 通过 Go 运行时内置的动态链接声明机制，无缝绑定 macOS 的 libSystem.B.dylib。
// 这样在 CGO_ENABLED=0 静态构建时也能直接调用系统底层的 Mach IPC 接口，
// 避开 gopsutil 在非 CGO 模式下返回 ErrNotImplementedError 的硬伤。
//
//go:cgo_import_dynamic libc_mach_host_self mach_host_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_host_statistics host_statistics "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_host_processor_info host_processor_info "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_mach_task_self mach_task_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_vm_deallocate vm_deallocate "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_host_statistics64 host_statistics64 "/usr/lib/libSystem.B.dylib"

//go:linkname syscall_syscall syscall.syscall
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err error)

//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err error)

//go:linkname libc_mach_host_self libc_mach_host_self
var libc_mach_host_self byte

//go:linkname libc_host_statistics libc_host_statistics
var libc_host_statistics byte

//go:linkname libc_host_processor_info libc_host_processor_info
var libc_host_processor_info byte

//go:linkname libc_mach_task_self libc_mach_task_self
var libc_mach_task_self byte

//go:linkname libc_vm_deallocate libc_vm_deallocate
var libc_vm_deallocate byte

//go:linkname libc_host_statistics64 libc_host_statistics64
var libc_host_statistics64 byte

const (
	// Mach host/processor 相关的常数定义（对应 mach/host_info.h 与 mach/processor_info.h）
	hostCPULoadInfoFlavor = 3
	hostCPULoadInfoCount  = 4

	processorCPULoadInfoFlavor = 2
	cpuStateUser               = 0
	cpuStateSystem             = 1
	cpuStateIdle               = 2
	cpuStateNice               = 3
	cpuStateMax                = 4
)

// hostCPULoadInfoData 承载全机聚合 ticks（单位：jiffies）
type hostCPULoadInfoData struct {
	Ticks [4]uint32
}

// getPlatformCPUTimes 获取全机与逐核的 CPU 时间结构体。
func getPlatformCPUTimes() (cpu.TimesStat, []cpu.TimesStat, error) {
	hostPort, _, _ := syscall_syscall(uintptr(unsafe.Pointer(&libc_mach_host_self)), 0, 0, 0)
	if hostPort == 0 {
		return cpu.TimesStat{}, nil, fmt.Errorf("mach_host_self 调用失败")
	}

	// 1. 获取全机聚合数据：HOST_CPU_LOAD_INFO
	var totalInfo hostCPULoadInfoData
	count := uint32(hostCPULoadInfoCount)
	r1, _, _ := syscall_syscall6(
		uintptr(unsafe.Pointer(&libc_host_statistics)),
		hostPort,
		uintptr(hostCPULoadInfoFlavor),
		uintptr(unsafe.Pointer(&totalInfo)),
		uintptr(unsafe.Pointer(&count)),
		0, 0,
	)
	if r1 != 0 {
		return cpu.TimesStat{}, nil, fmt.Errorf("host_statistics 失败，返回值: %d", r1)
	}

	total := cpu.TimesStat{
		CPU:    "cpu-total",
		User:   float64(totalInfo.Ticks[cpuStateUser]),
		System: float64(totalInfo.Ticks[cpuStateSystem]),
		Idle:   float64(totalInfo.Ticks[cpuStateIdle]),
		Nice:   float64(totalInfo.Ticks[cpuStateNice]),
	}

	// 2. 获取逐核数据：PROCESSOR_CPU_LOAD_INFO
	var ncpu uint32
	var infoArray unsafe.Pointer
	var infoCount uint32
	r2, _, _ := syscall_syscall6(
		uintptr(unsafe.Pointer(&libc_host_processor_info)),
		hostPort,
		uintptr(processorCPULoadInfoFlavor),
		uintptr(unsafe.Pointer(&ncpu)),
		uintptr(unsafe.Pointer(&infoArray)),
		uintptr(unsafe.Pointer(&infoCount)),
		0,
	)
	if r2 != 0 {
		return total, nil, fmt.Errorf("host_processor_info 失败，返回值: %d", r2)
	}

	var percore []cpu.TimesStat
	if ncpu > 0 && infoArray != nil {
		// 内核返回连续的 CPU_STATE_MAX 长度的 uint32 数组，每一个核心一段
		slice := unsafe.Slice((*uint32)(infoArray), ncpu*cpuStateMax)
		percore = make([]cpu.TimesStat, ncpu)
		for i := 0; i < int(ncpu); i++ {
			base := i * cpuStateMax
			percore[i] = cpu.TimesStat{
				CPU:    fmt.Sprintf("cpu%d", i),
				User:   float64(slice[base+cpuStateUser]),
				System: float64(slice[base+cpuStateSystem]),
				Idle:   float64(slice[base+cpuStateIdle]),
				Nice:   float64(slice[base+cpuStateNice]),
			}
		}

		// 必须释放内核动态分配的输出缓冲区，避免长期运行导致虚拟内存句柄泄漏
		taskPort, _, _ := syscall_syscall(uintptr(unsafe.Pointer(&libc_mach_task_self)), 0, 0, 0)
		if taskPort != 0 {
			_, _, _ = syscall_syscall(
				uintptr(unsafe.Pointer(&libc_vm_deallocate)),
				taskPort,
				uintptr(infoArray),
				uintptr(infoCount*4),
			)
		}
	}

	return total, percore, nil
}

// getPlatformUptime 通过 sysctl kern.boottime 获取自开机以来的秒数，
// 避免在没有 /proc/uptime 的 macOS 环境下返回 0。
func getPlatformUptime(now time.Time) float64 {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err == nil && tv.Sec > 0 {
		up := float64(now.Unix() - tv.Sec)
		if up > 0 {
			return up
		}
	}
	return 0.0
}

// vmStatistics64 对应 mach/vm_statistics.h 的 struct vm_statistics64。
// 字段顺序与大小必须与内核保持一致，HOST_VM_INFO64_COUNT = 152 字节 / 4 = 38。
type vmStatistics64 struct {
	FreeCount           uint32
	ActiveCount         uint32
	InactiveCount       uint32
	WireCount           uint32
	ZeroFillCount       uint64
	Reactivations       uint64
	Pageins             uint64
	Pageouts            uint64
	Faults              uint64
	CowFaults           uint64
	Lookups             uint64
	Hits                uint64
	Purges              uint64
	PurgeableCount      uint32
	SpeculativeCount    uint32
	Decompressions      uint64
	Compressions        uint64
	Swapins             uint64
	Swapouts            uint64
	CompressorPageCount uint32
	ThrottledCount      uint32
	ExternalPageCount   uint32
	InternalPageCount   uint32
	TotalUncompressed   uint64
}

const (
	hostVMInfo64Flavor = 4
	hostVMInfo64Count  = 38
)

// getPlatformMemCache 通过 Mach 的 HOST_VM_INFO64 取文件页统计，
// 用于填充「缓存缓冲」一栏。macOS 的内核不提供 free/buffers 这一套
// Linux 语义，可类比的是 external（file-backed）页数量。
func getPlatformMemCache() (cached, shared uint64) {
	hostPort, _, _ := syscall_syscall(uintptr(unsafe.Pointer(&libc_mach_host_self)), 0, 0, 0)
	if hostPort == 0 {
		return 0, 0
	}

	var vm vmStatistics64
	count := uint32(hostVMInfo64Count)
	r, _, _ := syscall_syscall6(
		uintptr(unsafe.Pointer(&libc_host_statistics64)),
		hostPort,
		uintptr(hostVMInfo64Flavor),
		uintptr(unsafe.Pointer(&vm)),
		uintptr(unsafe.Pointer(&count)),
		0, 0,
	)
	if r != 0 {
		return 0, 0
	}

	pageSize := uint64(unix.Getpagesize())
	// external 页即文件缓存，对应活动监视器里的「已缓存文件」。
	return uint64(vm.ExternalPageCount) * pageSize, 0
}
