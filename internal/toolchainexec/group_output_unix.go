//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package toolchainexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func combinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = writer, writer
	err = cmd.Start()
	_ = writer.Close()
	if err != nil {
		return nil, err
	}

	// Keep the leader unreaped until we finish draining. Its PID reserves the
	// group ID, so cancellation cannot signal a newly reused group.
	exited := make(chan error, 1)
	go func() { exited <- waitForExit(cmd.Process.Pid) }()
	var output bytes.Buffer
	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, reader)
		drained <- err
	}()

	done := ctx.Done()
	var timer *time.Timer
	var deadline <-chan time.Time
	startDrainDeadline := func() {
		if timer == nil && drained != nil {
			timer = time.NewTimer(outputDrainDelay)
			deadline = timer.C
		}
	}
	var runErr error
	for exited != nil || drained != nil {
		select {
		case err := <-exited:
			exited = nil
			if err != nil {
				// ECHILD means we have lost ownership of the PID.
				if !errors.Is(err, syscall.ECHILD) {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				done = nil
				runErr = err
			}
			startDrainDeadline()
		case err := <-drained:
			drained = nil
			if runErr == nil {
				runErr = err
			}
		case <-done:
			done = nil
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				_ = cmd.Process.Kill()
				runErr = os.NewSyscallError("kill", err)
			} else {
				runErr = ctx.Err()
			}
			startDrainDeadline()
		case <-deadline:
			deadline = nil
			if drained != nil {
				if runErr == nil {
					runErr = exec.ErrWaitDelay
				}
				_ = reader.Close()
			}
		}
	}
	if timer != nil {
		timer.Stop()
	}
	if err := cmd.Wait(); err != nil {
		return output.Bytes(), err
	}
	return output.Bytes(), runErr
}
