package agent

import (
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/tools"
)

func TestCrashSafeResumeAfterWriteAction(t *testing.T) {
	root := t.TempDir()

	initial := `package main

func Add(a, b int) int {
	return a + b
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(initial),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 10)
	agent.Router = tools.New(root)
	agent.State = StateWriteReq

	task := "Change math.go so add(2, 3) returns 10"

	/*
		Simulate the checkpoint immediately before the dangerous
		write action. The persisted FSM must still say WRITE_REQUIRED.
	*/
	if err := agent.saveState(task, 3); err != nil {
		t.Fatalf("save pre-write checkpoint: %v", err)
	}

	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}

	if !ok {
		t.Fatal("expected persisted checkpoint")
	}

	if saved.State != StateWriteReq {
		t.Fatalf(
			"expected WRITE_REQUIRED checkpoint, got %s",
			saved.State,
		)
	}

	if saved.Iteration != 3 {
		t.Fatalf(
			"expected checkpoint iteration 3, got %d",
			saved.Iteration,
		)
	}

	/*
		New process / new Agent after crash.
	*/
	resumed := New(root, "test-model", 10)

	if err := resumed.Run(task); err == nil {
		/*
			The test intentionally stops because no Ollama response is
			available. What matters is that restoration reaches the
			persisted FSM state rather than silently starting over.
		*/
		t.Fatal("expected Run to stop without Ollama")
	}

	if resumed.State != StateWriteReq {
		t.Fatalf(
			"expected resumed state WRITE_REQUIRED, got %s",
			resumed.State,
		)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != initial {
		t.Fatalf(
			"file changed before the resumed write action:\n%s",
			data,
		)
	}
}
