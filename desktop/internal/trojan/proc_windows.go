//go:build windows

package trojan

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
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
	return cmd.Process.Kill()
}

func forceKill(cmd *exec.Cmd) error {
	return terminate(cmd)
}

// afterStart places the child into a kill-on-close job object so it cannot
// outlive the desktop process if the UI crashes or is force-terminated.
func afterStart(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	jobOnce.Do(func() {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(
			handle,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			windows.CloseHandle(handle)
			return
		}
		jobHandle = handle
	})
	if jobHandle == 0 {
		return nil
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(jobHandle, process)
}
