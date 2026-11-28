//go:build unix && !(linux || darwin || dragonfly || freebsd || netbsd || openbsd)

package toolchainexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func combinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = outputDrainDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd.CombinedOutput()
}
