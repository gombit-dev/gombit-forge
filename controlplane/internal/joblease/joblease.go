// Package joblease is the lease the control plane's job queues (GitHub export,
// Cloud build) claim jobs under, so a job a worker abandoned does not stay
// "running" forever.
//
// A claim stamps the job's lease_expires_at; the worker renews it while it
// works (Keep); a job whose lease lapsed means its worker died, or shut down
// past its grace period, and the queue fails it with a reason the user sees.
// It is not re-run: an export or a Cloud build that got part way has side
// effects outside the database (a created repository, a submitted build), so
// running it again is the user's call, not the queue's. A late worker that was
// only slow finds the job no longer running, and its outcome write is refused
// rather than overwriting the recorded failure.
//
// Gombit's jobs system is not used here: its only durable driver is Redis,
// which the control plane does not run; the queues live in its PostgreSQL.
package joblease

import (
	"context"
	"time"
)

const (
	// DefaultLease is how long a claim, or a renewal, holds a job.
	DefaultLease = 2 * time.Minute
	// DefaultRenewEvery is how often a worker renews the lease of the job it
	// is working: a quarter of DefaultLease, so a few renewals can fail (a
	// database blip) before the lease lapses.
	DefaultRenewEvery = 30 * time.Second
)

// Keep renews a claimed job's lease every interval until the returned stop is
// called (or ctx ends), reporting each failed renewal to onErr. stop waits for
// the renewer to exit, so no renewal lands after the job's outcome is written.
func Keep(ctx context.Context, every time.Duration, renew func(context.Context) error, onErr func(error)) (stop func()) {
	if every <= 0 {
		every = DefaultRenewEvery
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := renew(ctx); err != nil && ctx.Err() == nil && onErr != nil {
					onErr(err)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
