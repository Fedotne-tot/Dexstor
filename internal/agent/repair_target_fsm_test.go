package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRepairTargetFlowsThroughFSM(t *testing.T) {
	index := &projectindex.Index{
		Files: []projectindex.File{
			{Path: "math.go", Language: "Perl"},
			{Path: "test.go", Language: "Perl"},
		},
		Relations: []projectindex.Relation{
			{
				File:   "test.go",
				Target: "math.go",
				Kind:   "require",
			},
		},
	}

	a := New(t.TempDir(), "test-model", 12)
	a.ProjectIndex = index
	a.DependencyGraph = dependencygraph.Build(index)

	// This is the same failure information that the TEST state
	// passes into failedSourcePath().
	failure := "test.go: expected 10, got 5\n"

	a.RepairPath = a.failedSourcePath(failure)

	if a.RepairPath != "math.go" {
		t.Fatalf(
			"expected FSM repair target math.go, got %q",
			a.RepairPath,
		)
	}

	if a.State != StateDiscover {
		t.Fatalf(
			"unexpected initial FSM state: %s",
			a.State,
		)
	}

	deps := a.DependencyGraph.Dependencies("test.go")

	if len(deps) != 1 || deps[0] != "math.go" {
		t.Fatalf(
			"expected test.go dependency math.go, got %v",
			deps,
		)
	}
}
