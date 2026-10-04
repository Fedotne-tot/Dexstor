package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskStateRejectsDifferentTask(t *testing.T) {
	root := t.TempDir()

	taskA := "Исправь math.pl: add(2, 3) должна возвращать 10"
	taskB := "Исправь math.pl: add(2, 3) должна возвращать 20"

	stateA := &TaskState{
		Version:   taskStateVersion,
		Task:      taskA,
		TaskHash:  taskHash(taskA),
		Project:   root,
		State:     StateDone,
		Iteration: 5,
		History: []string{
			"inspect_project",
			"read_file:math.pl",
			"write_file:math.pl",
			"git_diff",
			"test:test.pl",
		},
		ReadFiles:  make(map[string]string),
		FileHashes: make(map[string]string),
	}

	if err := saveTaskState(root, stateA); err != nil {
		t.Fatalf("save task A state: %v", err)
	}

	saved, ok, err := loadTaskState(root, taskA)
	if err != nil {
		t.Fatalf("load task A state: %v", err)
	}

	if !ok {
		t.Fatal("expected task A state to be restored")
	}

	if saved.TaskHash != taskHash(taskA) {
		t.Fatalf("task A hash mismatch: got %s", saved.TaskHash)
	}

	saved, ok, err = loadTaskState(root, taskB)
	if err != nil {
		t.Fatalf("load task B state: %v", err)
	}

	if ok {
		t.Fatalf(
			"task B incorrectly restored task A state: state=%+v",
			saved,
		)
	}

	// Verify that the persisted file still belongs to task A.
	data, err := os.ReadFile(
		filepath.Join(root, ".ai", "task_state.json"),
	)
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("persisted task state is empty")
	}
}
