// Command noskip reads `go test -json` on stdin, prints a record of every test
// that ran, and exits non-zero when any test failed or skipped.
//
// It is how the toolchain lane (`make e2e`) holds its rule "a missing tool is a
// failure" for every test, not only the ones that remember to call
// toolchaintest.Skipf: in that lane every tool is installed and -short is
// unset, so a skip means something didn't run. A skip that is legitimate there
// is named with -allow-skip, with the reason it must give.
//
// The record is one line per top-level test with its package, result and time,
// so the log shows that a given test ran, and where: `go test -json` interleaves
// the packages it runs in parallel. -trim-prefix shortens the package paths. A
// failing test's output is printed in full.
//
//	go test -json ./... | go run ./internal/toolchaintest/noskip -trim-prefix example.com/mod/ -allow-skip 'TestX=running as root'
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// event is one line of `go test -json` (cmd/test2json). Build events name the
// package in ImportPath; everything else uses Package.
type event struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Elapsed    float64
	Output     string
}

// allowList maps a test name to the text its skip message must contain.
type allowList map[string]string

func (a allowList) String() string { return fmt.Sprint(map[string]string(a)) }

func (a allowList) Set(v string) error {
	name, reason, ok := strings.Cut(v, "=")
	if !ok || name == "" || reason == "" {
		return fmt.Errorf("want TestName=reason text, got %q", v)
	}
	a[name] = reason
	return nil
}

func main() {
	allowed := allowList{}
	flag.Var(allowed, "allow-skip", "`TestName=reason`: a skip this test may take, if its message contains reason (repeatable)")
	trim := flag.String("trim-prefix", "", "`prefix` to cut from package paths in the record")
	flag.Parse()
	if err := check(os.Stdin, os.Stdout, allowed, *trim); err != nil {
		fmt.Fprintln(os.Stdout, "noskip:", err)
		os.Exit(1)
	}
}

type key struct{ pkg, test string }

// check streams the record of in to out and returns an error describing every
// failure, disallowed skip and unfinished test. trim is cut from the package
// paths in the per-test lines.
func check(in io.Reader, out io.Writer, allowed allowList, trim string) error {
	output := map[key]*strings.Builder{} // buffered until the test ends
	running := map[key]bool{}
	var problems []string
	var passed, skipped, packages int

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if err := json.Unmarshal(line, &e); err != nil || e.Action == "" {
			// Not test2json: go test failed before it ran anything, or
			// something wrote to stdout around it. Show it, and don't pass.
			fmt.Fprintf(out, "%s\n", line)
			problems = append(problems, fmt.Sprintf("not go test -json output: %.120s", line))
			continue
		}
		pkg := e.Package
		if pkg == "" {
			pkg = e.ImportPath
		}
		k := key{pkg, e.Test}
		short := strings.TrimPrefix(pkg, trim)
		top := e.Test != "" && !strings.Contains(e.Test, "/")

		switch e.Action {
		case "run":
			running[k] = true
		case "output", "build-output":
			b := output[k]
			if b == nil {
				b = &strings.Builder{}
				output[k] = b
			}
			b.WriteString(e.Output)
		case "build-fail":
			fmt.Fprintf(out, "%s", text(output[k]))
			problems = append(problems, "build failed: "+pkg)
		case "pass", "fail", "skip":
			delete(running, k)
			msg := text(output[k])
			delete(output, k)
			switch {
			case e.Test == "" && e.Action == "pass":
				packages++
				fmt.Fprintf(out, "ok  \t%s\t%.3fs\n", pkg, e.Elapsed)
			case e.Test == "" && e.Action == "skip": // no test files
				fmt.Fprintf(out, "?   \t%s\t[no test files]\n", pkg)
			case e.Test == "": // the package failed: a test in it, a panic, a timeout
				if msg == "" {
					msg = fmt.Sprintf("FAIL\t%s\n", pkg)
				}
				fmt.Fprintf(out, "%s", msg)
				problems = append(problems, "FAIL "+pkg)
			case e.Action == "fail":
				fmt.Fprintf(out, "%s", msg)
				problems = append(problems, fmt.Sprintf("FAIL %s %s", pkg, e.Test))
			case e.Action == "skip":
				if reason, ok := allowed[e.Test]; ok && strings.Contains(msg, reason) {
					skipped++
					fmt.Fprintf(out, "--- SKIP: %s %s (allowed: %s)\n", short, e.Test, reason)
					break
				}
				fmt.Fprintf(out, "%s", msg)
				problems = append(problems, fmt.Sprintf("SKIP %s %s: nothing may skip in this lane", pkg, e.Test))
			case top:
				passed++
				fmt.Fprintf(out, "--- PASS: %s %s (%.2fs)\n", short, e.Test, e.Elapsed)
			}
		}
	}
	if err := sc.Err(); err != nil {
		problems = append(problems, "reading go test output: "+err.Error())
	}

	// A test that started and never ended: the binary died under it.
	var unfinished []string
	for k := range running {
		if k.test != "" {
			unfinished = append(unfinished, k.pkg+" "+k.test)
			fmt.Fprintf(out, "%s", text(output[k]))
		}
	}
	sort.Strings(unfinished)
	for _, u := range unfinished {
		problems = append(problems, "did not finish: "+u)
	}
	if packages == 0 && len(problems) == 0 {
		problems = append(problems, "no package passed: go test ran nothing")
	}

	fmt.Fprintf(out, "noskip: %d packages, %d top-level tests passed, %d allowed skips\n", packages, passed, skipped)
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	return nil
}

func text(b *strings.Builder) string {
	if b == nil {
		return ""
	}
	return b.String()
}
