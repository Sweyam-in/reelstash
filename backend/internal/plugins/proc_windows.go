//go:build windows

package plugins

import "os/exec"

func setProcAttr(cmd *exec.Cmd) {}

func terminate(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func kill(cmd *exec.Cmd) { terminate(cmd) }
