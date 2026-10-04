package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRepairTargetIsDeterministicWithMultipleDependencies(t *testing.T) {
	index := &projectindex.Index{
		Files: []projectindex.File{
			{Path: "alpha.pl", Language: "Perl"},
			{Path: "beta.pl", Language: "Perl"},
			{Path: "test.pl", Language: "Perl"},
		},
		Relations: []projectindex.Relation{
			{
				File:   "test.pl",
				Target: "beta.pl",
				Kind:   "require",
			},
			{
				File:   "test.pl",
				Target: "alpha.pl",
				Kind:   "require",
			},
		},
	}

	a := New(t.TempDir(), "test-model", 12)
	a.ProjectIndex = index
	a.DependencyGraph = dependencygraph.Build(index)

	for i := 0; i < 10; i++ {
		got := a.failedSourcePath("test.pl: expected 10, got 5\n")

		if got != "alpha.pl" {
			t.Fatalf(
				"expected deterministic repair target alpha.pl, got %q",
				got,
			)
		}
	}
}
