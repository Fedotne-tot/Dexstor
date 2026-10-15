package agent

import (
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRestoreStateRebuildsDependencyGraph(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte("package main\n\nfunc Add(a, b int) int { return a + b }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte("package main\nimport \"./math.go\"\n\nfunc init() { _ = Add(1,2) }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	index, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	a := New(root, "test-model", 1)
	a.ProjectIndex = index
	a.DependencyGraph = dependencygraph.Build(index)

	state := &TaskState{
		Version:      taskStateVersion,
		ProjectIndex: a.ProjectIndex,
		Task:         "restore dependency graph",
		Project:      root,
	}

	restored := New(root, "test-model", 1)
	restored.restoreState(state)

	if restored.ProjectIndex == nil {
		t.Fatal("project index was not restored")
	}

	if restored.DependencyGraph == nil {
		t.Fatal("dependency graph was not rebuilt")
	}

	deps := restored.DependencyGraph.Dependencies("test.go")

	if len(deps) != 1 || deps[0] != "math.go" {
		t.Fatalf(
			"unexpected restored dependency graph: %#v",
			deps,
		)
	}
}
