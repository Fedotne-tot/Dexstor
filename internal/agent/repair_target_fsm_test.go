package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRepairTargetFlowsThroughFSM(t *testing.T) {
	index := &projectindex.Index{
		Files: []projectindex.File{
			{Path: "math.pl", Language: "Perl"},
			{Path: "test.pl", Language: "Perl"},
		},
		Relations: []projectindex.Relation{
			{
				File:   "test.pl",
				Target: "math.pl",
				Kind:   "require",
			},
		},
	}

	a := New(t.TempDir(), "test-model", 12)
	a.ProjectIndex = index
	a.DependencyGraph = dependencygraph.Build(index)

	// This is the same failure information that the TEST state
	// passes into failedSourcePath().
	failure := "test.pl: expected 10, got 5\n"

	a.RepairPath = a.failedSourcePath(failure)

	if a.RepairPath != "math.pl" {
		t.Fatalf(
			"expected FSM repair target math.pl, got %q",
			a.RepairPath,
		)
	}

	if a.State != StateDiscover {
		t.Fatalf(
			"unexpected initial FSM state: %s",
			a.State,
		)
	}

	deps := a.DependencyGraph.Dependencies("test.pl")

	if len(deps) != 1 || deps[0] != "math.pl" {
		t.Fatalf(
			"expected test.pl dependency math.pl, got %v",
			deps,
		)
	}
}
