package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const pkg = "example.com/p"

// stream renders test2json events, one per line, the way `go test -json`
// writes them.
func stream(t *testing.T, events ...event) string {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func run(name string) event { return event{Action: "run", Package: pkg, Test: name} }

func out(name, text string) event {
	return event{Action: "output", Package: pkg, Test: name, Output: text}
}

func end(action, name string) event { return event{Action: action, Package: pkg, Test: name} }

func pkgPass() event { return event{Action: "pass", Package: pkg, Elapsed: 1.5} }
func pkgFail() event { return event{Action: "fail", Package: pkg, Elapsed: 1.5} }

func skipped(name, reason string) []event {
	return []event{run(name), out(name, "    x_test.go:9: "+reason+"\n"), end("skip", name)}
}

func TestCheck(t *testing.T) {
	root := allowList{"TestRoot": "running as root"}
	cases := []struct {
		name    string
		events  []event
		allowed allowList
		wantErr string // "" means check passes
		wantOut string
	}{
		{
			name:    "every test passes",
			events:  []event{run("TestA"), end("pass", "TestA"), run("TestA/sub"), end("pass", "TestA/sub"), pkgPass()},
			wantOut: "--- PASS: p TestA (0.00s)\nok  \texample.com/p\t1.500s\n",
		},
		{
			name:    "a skip fails the lane",
			events:  append(skipped("TestB", "docker not on PATH"), pkgPass()),
			wantErr: "SKIP example.com/p TestB",
			wantOut: "docker not on PATH",
		},
		{
			name:    "a subtest skip fails the lane",
			events:  append(append([]event{run("TestA")}, skipped("TestA/sub", "no node")...), end("pass", "TestA"), pkgPass()),
			wantErr: "SKIP example.com/p TestA/sub",
		},
		{
			name:    "an allowed skip with its reason passes",
			events:  append(skipped("TestRoot", "running as root: unlink"), pkgPass()),
			allowed: root,
			wantOut: "--- SKIP: p TestRoot (allowed: running as root)",
		},
		{
			name:    "an allowed test skipping for another reason fails",
			events:  append(skipped("TestRoot", "atlas not on PATH"), pkgPass()),
			allowed: root,
			wantErr: "SKIP example.com/p TestRoot",
		},
		{
			name:    "a failure prints the test's output",
			events:  []event{run("TestC"), out("TestC", "    c_test.go:3: boom\n"), end("fail", "TestC"), pkgFail()},
			wantErr: "FAIL example.com/p TestC",
			wantOut: "c_test.go:3: boom",
		},
		{
			name:    "a test that never ends is reported",
			events:  []event{run("TestD"), out("TestD", "panic: test timed out after 1s\n"), pkgFail()},
			wantErr: "did not finish: example.com/p TestD",
			wantOut: "panic: test timed out",
		},
		{
			name: "a build failure fails the lane",
			events: []event{
				{Action: "build-output", ImportPath: pkg + " [" + pkg + ".test]", Output: "x.go:1: undefined: y\n"},
				{Action: "build-fail", ImportPath: pkg + " [" + pkg + ".test]"},
				pkgFail(),
			},
			wantErr: "build failed",
			wantOut: "undefined: y",
		},
		{
			name:    "no input means nothing ran",
			wantErr: "go test ran nothing",
		},
		{
			name:    "only packages without tests means nothing ran",
			events:  []event{{Action: "skip", Package: pkg}},
			wantErr: "go test ran nothing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed := tc.allowed
			if allowed == nil {
				allowed = allowList{}
			}
			var got strings.Builder
			err := check(strings.NewReader(stream(t, tc.events...)), &got, allowed, "example.com/")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("check: %v\noutput:\n%s", err, got.String())
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want an error containing %q, got %v\noutput:\n%s", tc.wantErr, err, got.String())
			}
			if !strings.Contains(got.String(), tc.wantOut) {
				t.Errorf("output lacks %q:\n%s", tc.wantOut, got.String())
			}
		})
	}
}

func TestCheckRejectsInputThatIsNotTest2JSON(t *testing.T) {
	in := "go: cannot find main module\n" + stream(t, run("TestA"), end("pass", "TestA"), pkgPass())
	var got strings.Builder
	err := check(strings.NewReader(in), &got, allowList{}, "")
	if err == nil || !strings.Contains(err.Error(), "not go test -json output") {
		t.Fatalf("want a rejection of the stray line, got %v", err)
	}
	if !strings.Contains(got.String(), "cannot find main module") {
		t.Errorf("the stray line should be shown:\n%s", got.String())
	}
}

// go test -json interleaves packages, so each line names its package: two
// packages with a test of the same name stay apart.
func TestCheckNamesEachTestsPackage(t *testing.T) {
	ev := func(action, pkg, test string) event { return event{Action: action, Package: pkg, Test: test} }
	in := stream(t,
		ev("run", "example.com/mod/a", "TestSame"),
		ev("run", "example.com/mod/b", "TestSame"),
		ev("pass", "example.com/mod/b", "TestSame"),
		ev("pass", "example.com/mod/a", "TestSame"),
		ev("pass", "example.com/mod/a", ""),
		ev("pass", "example.com/mod/b", ""),
	)
	var got strings.Builder
	if err := check(strings.NewReader(in), &got, allowList{}, "example.com/mod/"); err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, want := range []string{"--- PASS: a TestSame ", "--- PASS: b TestSame "} {
		if !strings.Contains(got.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, got.String())
		}
	}
}

func TestAllowListFlag(t *testing.T) {
	a := allowList{}
	if err := a.Set("TestRoot=running as root"); err != nil || a["TestRoot"] != "running as root" {
		t.Fatalf("Set: %v, %v", err, a)
	}
	for _, bad := range []string{"TestRoot", "=reason", "TestRoot="} {
		if err := a.Set(bad); err == nil {
			t.Errorf("Set(%q) should fail", bad)
		}
	}
}
