//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/svc"

	"github.com/iamartinlong/Argus/cmd/argus/cmd"
)

type argusService struct{}

func (m *argusService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// SCM 传入的 args[0] 为服务名，后续为 binPath 参数；同步到 os.Args 供 cobra 解析
	if len(args) > 1 {
		os.Args = append([]string{os.Args[0]}, args[1:]...)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				// 进程退出即停止服务；http.Server 无优雅关闭钩子，SCM 5s 内回收
				return false, 0
			}
		case err := <-done:
			// serve 正常退出或报错（非 Stop 触发）
			if err != nil {
				return true, 1
			}
			return false, 0
		}
	}
}

func maybeRunAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}
	cmd.ServiceMode = true
	_ = svc.Run("Argus", &argusService{})
	return true
}
