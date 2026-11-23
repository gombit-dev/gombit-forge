package toolchaintest

import (
	"fmt"
	"strings"
	"testing"
)

// recorder stands in for a test: it records whether a gate skipped or failed
// instead of stopping the real test. Embedding testing.TB satisfies the
// interface; only the methods the package calls are overridden.
type recorder struct {
	testing.TB
	skipped, failed string
}

func (r *recorder) Helper() {}

func (r *recorder) Skipf(format string, args ...any) {
	if r.skipped == "" && r.failed == "" {
		r.skipped = fmt.Sprintf(format, args...)
	}
}

func (r *recorder) Fatalf(format string, args ...any) {
	if r.skipped == "" && r.failed == "" {
		r.failed = fmt.Sprintf(format, args...)
	}
}

const missing = "forge-toolchaintest-no-such-binary"

func TestMissingBinarySkipsByDefault(t *testing.T) {
	t.Setenv(RequireEnv, "")
	r := &recorder{}
	RequireBinaries(r, "go", missing)
	if r.failed != "" || !strings.Contains(r.skipped, missing+" not on PATH") {
		t.Fatalf("want a skip naming %s, got skipped=%q failed=%q", missing, r.skipped, r.failed)
	}
}

func TestMissingBinaryFailsWhenRequired(t *testing.T) {
	t.Setenv(RequireEnv, "1")
	r := &recorder{}
	RequireBinaries(r, missing)
	if r.skipped != "" || !strings.Contains(r.failed, RequireEnv) || !strings.Contains(r.failed, missing) {
		t.Fatalf("want a failure naming %s and %s, got skipped=%q failed=%q", RequireEnv, missing, r.skipped, r.failed)
	}
}

func TestFailureSaysAValueThatIsNotABooleanCountsAsSet(t *testing.T) {
	t.Setenv(RequireEnv, "off")
	r := &recorder{}
	RequireBinaries(r, missing)
	if !strings.Contains(r.failed, `FORGE_REQUIRE_TOOLCHAIN="off" is not a boolean`) {
		t.Fatalf("want the failure to explain the value, got failed=%q skipped=%q", r.failed, r.skipped)
	}
}

func TestPresentBinariesNeitherSkipNorFail(t *testing.T) {
	t.Setenv(RequireEnv, "1")
	r := &recorder{}
	RequireBinaries(r, "go")
	if r.skipped != "" || r.failed != "" {
		t.Fatalf("go is on PATH in every environment these tests run in; got skipped=%q failed=%q", r.skipped, r.failed)
	}
}

func TestRequiredReadsTheVariable(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "0": false, "false": false, "FALSE": false,
		"1": true, "true": true, "TRUE": true,
		"no": true, "off": true, "yes": true, // not booleans: on, so a typo can't turn it off
	} {
		t.Setenv(RequireEnv, value)
		if got := Required(); got != want {
			t.Errorf("%s=%q: Required() = %v, want %v", RequireEnv, value, got, want)
		}
	}
}
