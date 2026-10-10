//go:build windows

package deploy

import (
	"os/exec"
	"strconv"
)

// configureProcessGroup Windows 无进程组概念，改用 taskkill /T 终止进程树
func configureProcessGroup(*exec.Cmd) {}

// killProcessTree 用 taskkill /T /F 终止进程及其子进程（powershell 下的孙进程）
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
