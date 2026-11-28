package toolchainexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCancellationDuringOutputDrain(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := CombinedOutput(ctx, dir, executable, "-test.run=^TestDrainHelper$", "--", "leader")
		result <- err
	}()

	var childPID int
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	deadline := time.After(5 * time.Second)
	for childPID == 0 {
		select {
		case <-poll.C:
			data, err := os.ReadFile(filepath.Join(dir, "leader-exited"))
			if err == nil {
				childPID, _ = strconv.Atoi(string(data))
			}
		case err := <-result:
			t.Fatalf("runner returned before cancellation: %v", err)
		case <-deadline:
			t.Fatal("descendant did not observe the leader exit")
		}
	}

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel during drain: got %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID)); err == nil {
		state := strings.Fields(string(data)[strings.LastIndexByte(string(data), ')')+1:])[0]
		if state != "Z" && state != "X" {
			t.Fatalf("descendant is still alive (state %q)", state)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestDrainHelper(t *testing.T) {
	switch os.Args[len(os.Args)-1] {
	case "leader":
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		_, err = os.StartProcess(executable, []string{
			executable, "-test.run=^TestDrainHelper$", "--", strconv.Itoa(os.Getpid()), "descendant",
		}, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
		if err != nil {
			t.Fatal(err)
		}
	case "descendant":
		leaderPID, err := strconv.Atoi(os.Args[len(os.Args)-2])
		if err != nil {
			t.Fatal(err)
		}
		for os.Getppid() == leaderPID {
			time.Sleep(5 * time.Millisecond)
		}
		if err := os.WriteFile("leader-exited", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(4 * time.Second)
	}
}
