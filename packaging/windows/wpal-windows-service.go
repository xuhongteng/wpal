//go:build windows

// Command wpal-service 把 wpal 注册为 Windows 服务。
//
// 编译（需在 Windows 或带 mingw 工具链的环境）：
//
//	GOOS=windows GOARCH=amd64 go build -o wpal-service.exe ./packaging/windows
//
// 安装（管理员权限）：
//
//	wpal-service.exe install
//	wpal-service.exe start
//
// 卸载：
//
//	wpal-service.exe stop
//	wpal-service.exe remove
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "wpal"
const serviceDisplay = "Wpal - Server Resource Monitor"
const serviceDescription = "Lightweight server resource monitoring dashboard."

type wpalService struct{}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "install":
		install()
	case "remove":
		remove()
	case "start":
		start()
	case "stop":
		stop()
	case "run":
		run()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: wpal-service <install|remove|start|stop|run>")
}

func exePath() (string, error) {
	return filepath.Abs(os.Args[0])
}

func install() {
	exe, err := exePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "获取可执行文件路径失败:", err)
		os.Exit(1)
	}
	m, err := mgr.Connect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接服务管理器失败:", err)
		os.Exit(1)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		fmt.Println("服务已存在，先执行 remove 再 install")
		os.Exit(1)
	}

	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: serviceDisplay,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建服务失败:", err)
		os.Exit(1)
	}
	defer s.Close()

	// 出错时自动回滚
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
	}, 0); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 设置恢复策略失败:", err)
	}

	fmt.Println("服务已安装。执行 'wpal-service start' 启动。")
}

func remove() {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接服务管理器失败:", err)
		os.Exit(1)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Println("服务不存在")
		return
	}
	defer s.Close()
	s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		fmt.Fprintln(os.Stderr, "删除服务失败:", err)
		os.Exit(1)
	}
	fmt.Println("服务已移除")
}

func start() {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接服务管理器失败:", err)
		os.Exit(1)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开服务失败:", err)
		os.Exit(1)
	}
	defer s.Close()
	if err := s.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "启动服务失败:", err)
		os.Exit(1)
	}
	fmt.Println("服务已启动")
}

func stop() {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接服务管理器失败:", err)
		os.Exit(1)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开服务失败:", err)
		os.Exit(1)
	}
	defer s.Close()
	s.Control(svc.Stop)
	fmt.Println("服务已停止")
}

func run() {
	// 服务控制管理器回调
	if err := svc.Run(serviceName, &wpalService{}); err != nil {
		fmt.Fprintln(os.Stderr, "服务运行失败:", err)
		os.Exit(1)
	}
}

func (s *wpalService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running}
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			return false, uint32(accepted)
		}
	}
	return false, uint32(accepted)
}
