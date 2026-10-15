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

	write("math.go", "package main\n")
	write("other.go", "package main\n")

	write("test.go", "package main\n\nimport \"./math.go\"\n\nfunc init() { _ = 1 }\n")

	index1, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	graph1 := dependencygraph.Build(index1)

	deps := graph1.Dependencies("test.go")
	if len(deps) != 1 || deps[0] != "math.go" {
		t.Fatalf("initial dependency graph: got %v, want [math.go]", deps)
	}

	// Change the project: test.go now depends on other.go instead.
	write("test.go", "package main\n\nimport \"./other.go\"\n\nfunc init() { _ = 1 }\n")

	index2, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	graph2 := dependencygraph.Build(index2)

	deps = graph2.Dependencies("test.go")
	if len(deps) != 1 || deps[0] != "other.go" {
		t.Fatalf("rebuilt dependency graph: got %v, want [other.go]", deps)
	}

	if got := graph2.Dependencies("test.go"); len(got) > 0 && got[0] == "math.go" {
		t.Fatalf("stale dependency math.go remained after rebuild")
	}
}
