package agent

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTaskStateCrashBeforeRenameKeepsPreviousState(t *testing.T) {
	root := t.TempDir()
	task := "atomic crash before rename"

	previous := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateWriteReq,
		Iteration: 7,
		History: []string{
			"inspect_project",
			"read_file:math.go",
		},
		ReadFiles:    make(map[string]string),
		FileHashes:   make(map[string]string),
		ChangedFiles: []string{},
	}

	if err := saveTaskState(root, previous); err != nil {
		t.Fatalf("save previous state: %v", err)
	}

	path := taskStatePath(root)
	tmpPath := path + ".tmp"

	crashed := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateTest,
		Iteration: 8,
		History: []string{
			"inspect_project",
			"read_file:math.go",
			"write_file:math.go",
			"git_diff",
		},
		ReadFiles:    make(map[string]string),
		FileHashes:   make(map[string]string),
		ChangedFiles: []string{"math.go"},
	}

	data, err := json.MarshalIndent(crashed, "", "  ")
	if err != nil {
		t.Fatalf("marshal simulated new state: %v", err)
	}

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		t.Fatalf("write simulated crash temporary file: %v", err)
	}

	loaded, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load after simulated crash: %v", err)
	}

	if !ok {
		t.Fatal("expected previous state to remain authoritative")
	}

	if loaded.State != StateWriteReq {
		t.Fatalf("expected previous state WRITE_REQUIRED, got %s", loaded.State)
	}

	if loaded.Iteration != 7 {
		t.Fatalf("expected previous iteration 7, got %d", loaded.Iteration)
	}

	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf(
			"expected stale temporary state to remain after simulated crash: %v",
			err,
		)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("previous task state disappeared: %v", err)
	}
}
