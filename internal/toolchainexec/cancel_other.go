//go:build !unix

package toolchainexec

import (
	"context"
	"os/exec"
)

func combinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = outputDrainDelay
	return cmd.CombinedOutput()
}
