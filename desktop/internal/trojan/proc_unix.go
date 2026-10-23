//go:build !windows

package trojan

import (
	"context"
	"os/exec"
	"syscall"
)

func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func newCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	applySysProcAttr(cmd)
	return cmd
}

func newCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	applySysProcAttr(cmd)
	return cmd
}

func terminate(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

func forceKill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// afterStart is a no-op on Unix; process-group termination handles cleanup.
func afterStart(cmd *exec.Cmd) error {
	return nil
}
