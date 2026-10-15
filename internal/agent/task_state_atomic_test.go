package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskStateAtomicSave(t *testing.T) {
	root := t.TempDir()

	task := "atomic persistence test"

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateDone,
		Iteration: 5,
		History: []string{
			"inspect_project",
			"read_file:math.go",
			"write_file:math.go",
			"git_diff",
			"test:test.go",
		},
		ReadFiles:  make(map[string]string),
		FileHashes: make(map[string]string),
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("saveTaskState failed: %v", err)
	}

	path := taskStatePath(root)
	tmpPath := path + ".tmp"

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("task state was not created: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temporary task state file remains after save: %v", err)
	}

	loaded, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load after atomic save failed: %v", err)
	}

	if !ok {
		t.Fatal("expected saved state to be restored")
	}

	if loaded.State != StateDone {
		t.Fatalf("expected DONE, got %s", loaded.State)
	}

	if loaded.Iteration != 5 {
		t.Fatalf("expected iteration 5, got %d", loaded.Iteration)
	}
}

func TestTaskStateCorruptionRecovery(t *testing.T) {
	tmp := t.TempDir()

	statePath := taskStatePath(tmp)
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}

	corruptData := []byte(`{"version":3,"task":`)

	if err := os.WriteFile(
		statePath,
		corruptData,
		0644,
	); err != nil {
		t.Fatal(err)
	}

	state, ok, err := loadTaskState(tmp, "broken")
	if err != nil {
		t.Fatalf("expected corrupted task state to recover, got error: %v", err)
	}

	if ok {
		t.Fatal("expected corrupted task state to force a fresh run")
	}

	if state != nil {
		t.Fatal("expected no restored state after corruption recovery")
	}

	corruptPath := statePath + ".corrupt"

	got, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatalf("expected corrupted backup %s: %v", corruptPath, err)
	}

	if string(got) != string(corruptData) {
		t.Fatalf(
			"corrupted backup changed: got %q want %q",
			got,
			corruptData,
		)
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf(
			"expected original corrupted state to be removed after backup, stat err=%v",
			err,
		)
	}
}
