package toolchainexec

import (
	"os"
	"syscall"
	"unsafe"
)

func waitForExit(pid int) error {
	const waitByPID = 1
	// Linux siginfo_t is 128 bytes. waitid writes the status without reaping.
	var info [16]uint64
	for {
		_, _, err := syscall.Syscall6(syscall.SYS_WAITID, waitByPID, uintptr(pid),
			uintptr(unsafe.Pointer(&info)), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if err == syscall.EINTR {
			continue
		}
		if err != 0 {
			return os.NewSyscallError("waitid", err)
		}
		return nil
	}
}
