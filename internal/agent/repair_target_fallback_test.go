package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRepairTargetFallsBackToLastWritePath(t *testing.T) {
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
	a.LastWritePath = "math.pl"

	got := a.failedSourcePath("expected 10, got 5\n")

	if got != "math.pl" {
		t.Fatalf(
			"expected fallback repair target math.pl, got %q",
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
