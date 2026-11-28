//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package toolchainexec

import (
	"os"
	"syscall"
)

func waitForExit(pid int) error {
	queue, err := syscall.Kqueue()
	if err != nil {
		return os.NewSyscallError("kqueue", err)
	}
	defer syscall.Close(queue)
	syscall.CloseOnExec(queue)

	var change syscall.Kevent_t
	syscall.SetKevent(&change, pid, syscall.EVFILT_PROC, syscall.EV_ADD|syscall.EV_ONESHOT)
	change.Fflags = syscall.NOTE_EXIT
	changes := []syscall.Kevent_t{change}
	events := make([]syscall.Kevent_t, 1)
	for {
		n, err := syscall.Kevent(queue, changes, events, nil)
		if err == syscall.EINTR {
			continue
		}
		// With no concurrent waiter, a leader gone before registration has
		// exited and is still reserved for cmd.Wait.
		if err == syscall.ESRCH || (n == 1 && events[0].Flags&syscall.EV_ERROR != 0 &&
			syscall.Errno(events[0].Data) == syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return os.NewSyscallError("kevent", err)
		}
		if n == 1 {
			if events[0].Flags&syscall.EV_ERROR != 0 {
				return os.NewSyscallError("kevent", syscall.Errno(events[0].Data))
			}
			return nil
		}
		changes = nil
	}
}
