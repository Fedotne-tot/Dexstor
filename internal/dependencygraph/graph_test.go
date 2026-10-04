package dependencygraph

import (
	"testing"

	"bratan-go/internal/projectindex"
)

func TestBuildDependencyGraph(t *testing.T) {
	index := &projectindex.Index{
		Relations: []projectindex.Relation{
			{
				File:   "test.pl",
				Target: "math.pl",
				Kind:   "require",
			},
			{
				File:   "app.pl",
				Target: "test.pl",
				Kind:   "require",
			},
			{
				File:   "app.pl",
				Target: "Helper::Math",
				Kind:   "use",
			},
		},
	}

	graph := Build(index)

	deps := graph.Dependencies("test.pl")

	if len(deps) != 1 || deps[0] != "math.pl" {
		t.Fatalf("unexpected test.pl dependencies: %#v", deps)
	}

	dependents := graph.Dependents("math.pl")

	if len(dependents) != 1 || dependents[0] != "test.pl" {
		t.Fatalf("unexpected math.pl dependents: %#v", dependents)
	}

	appDeps := graph.Dependencies("app.pl")

	if len(appDeps) != 2 {
		t.Fatalf("unexpected app.pl dependency count: %#v", appDeps)
	}

	if appDeps[0] != "Helper::Math" || appDeps[1] != "test.pl" {
		t.Fatalf("unexpected app.pl dependencies: %#v", appDeps)
	}
}

func TestBuildDependencyGraphDeduplicatesRelations(t *testing.T) {
	index := &projectindex.Index{
		Relations: []projectindex.Relation{
			{
				File:   "test.pl",
				Target: "math.pl",
				Kind:   "require",
			},
			{
				File:   "test.pl",
				Target: "math.pl",
				Kind:   "require",
			},
		},
	}

	graph := Build(index)

	deps := graph.Dependencies("test.pl")

	if len(deps) != 1 {
		t.Fatalf("expected one unique dependency, got %#v", deps)
	}
}
