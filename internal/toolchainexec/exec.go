// Package toolchainexec runs build and generator commands with bounded cancellation.
package toolchainexec

import (
	"context"
	"time"
)

const outputDrainDelay = time.Second

func CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return combinedOutput(ctx, dir, name, args...)
}
