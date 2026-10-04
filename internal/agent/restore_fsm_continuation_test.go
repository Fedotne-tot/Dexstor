package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestRestoreStateContinuesFSMFromSavedState(t *testing.T) {
	root := t.TempDir()

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

	original := New(root, "test-model", 12)
	original.ProjectIndex = index
	original.DependencyGraph = dependencygraph.Build(index)

	original.State = StateRepairRead
	original.RepairPath = "math.pl"
	original.LastWritePath = "math.pl"
	original.LastTestError = "test.pl: expected 10, got 5"
	original.History = []string{
		"inspect_project",
		"read_file:math.pl",
		"read_file:test.pl",
		"write_file:math.pl",
		"git_diff",
		"test:test.pl",
	}
	original.LastAction = "test:test.pl"
	original.LastResult = "WRONG: 5"

	task := "Fix math.pl so add(2, 3) returns 10"

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

	if restored.State != StateRepairRead {
		t.Fatalf(
			"expected restored state REPAIR_READ, got %s",
			restored.State,
		)
	}

	if restored.RepairPath != "math.pl" {
		t.Fatalf(
			"expected restored repair path math.pl, got %q",
			restored.RepairPath,
		)
	}

	if restored.LastTestError == "" {
		t.Fatal("expected restored test error")
	}

	if restored.LastAction != "test:test.pl" {
		t.Fatalf(
			"expected restored last action test:test.pl, got %q",
			restored.LastAction,
		)
	}

	if restored.LastResult != "WRONG: 5" {
		t.Fatalf(
			"expected restored last result WRONG: 5, got %q",
			restored.LastResult,
		)
	}

	if len(restored.History) != len(original.History) {
		t.Fatalf(
			"expected restored history length %d, got %d",
			len(original.History),
			len(restored.History),
		)
	}

	if restored.ProjectIndex == nil {
		t.Fatal("expected restored project index")
	}

	if restored.DependencyGraph == nil {
		t.Fatal("expected rebuilt dependency graph")
	}

	got := restored.failedSourcePath(
		"test.pl: expected 10, got 5\n",
	)

	if got != "math.pl" {
		t.Fatalf(
			"expected FSM repair target math.pl after restore, got %q",
			got,
		)
	}
}
