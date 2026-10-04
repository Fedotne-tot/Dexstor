package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestFailedSourcePathUsesDependencyGraph(t *testing.T) {
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

	a := &Agent{
		DependencyGraph: dependencygraph.Build(index),
		LastWritePath:   "",
	}

	got := a.failedSourcePath("test.pl: FAILED\nexpected 10, got 5")

	if got != "math.pl" {
		t.Fatalf("expected repair target math.pl, got %q", got)
	}
}
