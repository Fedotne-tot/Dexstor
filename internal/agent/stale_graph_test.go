package agent

import (
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestDependencyGraphRebuildsAfterProjectChange(t *testing.T) {
	root := t.TempDir()

	write := func(path, content string) {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("math.pl", "1;\n")
	write("other.pl", "1;\n")

	write("test.pl", `require "./math.pl";
1;
`)

	index1, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	graph1 := dependencygraph.Build(index1)

	deps := graph1.Dependencies("test.pl")
	if len(deps) != 1 || deps[0] != "math.pl" {
		t.Fatalf("initial dependency graph: got %v, want [math.pl]", deps)
	}

	// Change the project: test.pl now depends on other.pl instead.
	write("test.pl", `require "./other.pl";
1;
`)

	index2, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	graph2 := dependencygraph.Build(index2)

	deps = graph2.Dependencies("test.pl")
	if len(deps) != 1 || deps[0] != "other.pl" {
		t.Fatalf("rebuilt dependency graph: got %v, want [other.pl]", deps)
	}

	if got := graph2.Dependencies("test.pl"); len(got) > 0 && got[0] == "math.pl" {
		t.Fatalf("stale dependency math.pl remained after rebuild")
	}
}
