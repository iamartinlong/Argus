//go:build !windows

package deploy

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup 让 hook 命令运行在独立进程组，超时可整组终止（避免 nginx 等孙进程存活）
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree 终止命令所在进程组；组领导已退出时 Kill 返回 ESRCH，忽略即可
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
