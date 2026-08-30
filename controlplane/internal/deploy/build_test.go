package deploy

import "testing"

func TestBuildStateValid(t *testing.T) {
	valid := []BuildState{
		BuildQueued, BuildGenerating, BuildTesting, BuildBuilding,
		BuildPublishing, BuildSucceeded, BuildFailed, BuildCancelled,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []BuildState{"", "done", "Queued", "running"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestBuildStateIsTerminal(t *testing.T) {
	terminal := map[BuildState]bool{BuildSucceeded: true, BuildFailed: true, BuildCancelled: true}
	for _, s := range []BuildState{
		BuildQueued, BuildGenerating, BuildTesting, BuildBuilding,
		BuildPublishing, BuildSucceeded, BuildFailed, BuildCancelled,
	} {
		if got := s.IsTerminal(); got != terminal[s] {
			t.Errorf("%q IsTerminal = %v, want %v", s, got, terminal[s])
		}
	}
	if BuildState("bogus").IsTerminal() {
		t.Error("an unknown state must not be terminal")
	}
}

// TestBuildHappyPath walks the linear pipeline and confirms each step is a legal
// transition and no step may skip ahead.
func TestBuildHappyPath(t *testing.T) {
	path := []BuildState{
		BuildQueued, BuildGenerating, BuildTesting, BuildBuilding,
		BuildPublishing, BuildSucceeded,
	}
	for i := 0; i+1 < len(path); i++ {
		if !path[i].CanTransitionTo(path[i+1]) {
			t.Errorf("%q → %q should be allowed", path[i], path[i+1])
		}
		// Skipping a stage is not allowed.
		if i+2 < len(path) && path[i].CanTransitionTo(path[i+2]) {
			t.Errorf("%q → %q should be refused (skips a stage)", path[i], path[i+2])
		}
	}
}

// TestBuildFailAndCancelFromEveryStage: failed and cancelled are reachable from
// every non-terminal state, and from no terminal state.
func TestBuildFailAndCancelFromEveryStage(t *testing.T) {
	nonTerminal := []BuildState{
		BuildQueued, BuildGenerating, BuildTesting, BuildBuilding, BuildPublishing,
	}
	for _, s := range nonTerminal {
		if !s.CanTransitionTo(BuildFailed) {
			t.Errorf("%q → failed should be allowed", s)
		}
		if !s.CanTransitionTo(BuildCancelled) {
			t.Errorf("%q → cancelled should be allowed", s)
		}
	}
	for _, s := range []BuildState{BuildSucceeded, BuildFailed, BuildCancelled} {
		if s.CanTransitionTo(BuildFailed) || s.CanTransitionTo(BuildCancelled) {
			t.Errorf("terminal %q must not transition", s)
		}
	}
}

// TestBuildTransitionsFailClosed: terminal states, self-loops, backward moves
// and unknown states are all refused.
func TestBuildTransitionsFailClosed(t *testing.T) {
	if BuildSucceeded.CanTransitionTo(BuildPublishing) {
		t.Error("succeeded must not walk back to publishing")
	}
	if BuildTesting.CanTransitionTo(BuildTesting) {
		t.Error("a state must not transition to itself")
	}
	if BuildBuilding.CanTransitionTo(BuildGenerating) {
		t.Error("the pipeline does not run backward")
	}
	if BuildState("bogus").CanTransitionTo(BuildQueued) {
		t.Error("an unknown state permits no transition")
	}
	if BuildQueued.CanTransitionTo("bogus") {
		t.Error("a transition to an unknown state is refused")
	}
}

func TestEnvironmentTypeValid(t *testing.T) {
	for _, e := range []EnvironmentType{EnvironmentPreview, EnvironmentProduction} {
		if !e.Valid() {
			t.Errorf("%q should be valid", e)
		}
	}
	for _, e := range []EnvironmentType{"", "staging", "Preview", "prod"} {
		if e.Valid() {
			t.Errorf("%q should be invalid", e)
		}
	}
}
