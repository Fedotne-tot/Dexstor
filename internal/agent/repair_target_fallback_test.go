package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRepairTargetFallsBackToLastWritePath(t *testing.T) {
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
	a.LastWritePath = "math.go"

	got := a.failedSourcePath("expected 10, got 5\n")

	if got != "math.go" {
		t.Fatalf(
			"expected fallback repair target math.go, got %q",
			got,
		)
	}
}

func TestRepairTargetReturnsEmptyWithoutSourceOrFallback(t *testing.T) {
	a := New(t.TempDir(), "test-model", 12)

	got := a.failedSourcePath("expected 10, got 5\n")

	if got != "" {
		t.Fatalf(
			"expected empty repair target, got %q",
			got,
		)
	}
}
