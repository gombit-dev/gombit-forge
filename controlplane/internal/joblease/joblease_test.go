package joblease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeepRenewsUntilStopped(t *testing.T) {
	var renewals atomic.Int32
	var errs atomic.Int32
	stop := Keep(context.Background(), 5*time.Millisecond, func(context.Context) error {
		if renewals.Add(1) == 2 {
			return errors.New("blip")
		}
		return nil
	}, func(error) { errs.Add(1) })
	deadline := time.Now().Add(5 * time.Second)
	for renewals.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	after := renewals.Load()
	if after < 3 || errs.Load() != 1 {
		t.Fatalf("renewals = %d, errors = %d; want at least 3 renewals and the one failure reported", after, errs.Load())
	}
	time.Sleep(20 * time.Millisecond)
	if renewals.Load() != after {
		t.Fatal("a renewal ran after stop returned")
	}
}
