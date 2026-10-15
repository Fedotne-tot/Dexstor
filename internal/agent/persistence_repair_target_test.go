package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRestoredGraphCanResolveRepairTarget(t *testing.T) {
	root := t.TempDir()

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

	original := New(root, "test-model", 12)
	original.ProjectIndex = index
	original.DependencyGraph = dependencygraph.Build(index)
	original.State = StateRepairRead
	original.RepairPath = "math.go"
	original.LastTestError = "test.go: expected 10, got 5"

	task := "Fix math.go so add(2, 3) returns 10"

	if err := original.saveState(task, 7); err != nil {
		t.Fatalf("save state: %v", err)
	}

	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	if !ok {
		t.Fatal("expected persisted task state")
	}

	restored := New(root, "test-model", 12)
	restored.restoreState(saved)

	if restored.ProjectIndex == nil {
		t.Fatal("project index was not restored")
	}

	if restored.DependencyGraph == nil {
		t.Fatal("dependency graph was not rebuilt after restore")
	}

	got := restored.failedSourcePath(
		"test.go: expected 10, got 5\n",
	)

	if got != "math.go" {
		t.Fatalf(
			"expected restored repair target math.go, got %q",
			got,
		)
	}
}
