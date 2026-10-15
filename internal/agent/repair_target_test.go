package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestFailedSourcePathUsesDependencyGraph(t *testing.T) {
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

	a := &Agent{
		DependencyGraph: dependencygraph.Build(index),
		LastWritePath:   "",
	}

	got := a.failedSourcePath("test.go: FAILED\nexpected 10, got 5")

	if got != "math.go" {
		t.Fatalf("expected repair target math.go, got %q", got)
	}
}
